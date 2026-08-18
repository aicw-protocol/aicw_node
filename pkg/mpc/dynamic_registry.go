// Package mpc provides dynamic peer registry for AICW MPC network.
//
// AICW-FORK: This file extends the original Mpcium registry with dynamic
// peer management capabilities. Peers can join and leave at runtime.
package mpc

import (
	"bytes"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/consul/api"

	"github.com/aicw/aicw_node/pkg/committee"
	"github.com/aicw/aicw_node/pkg/eligibility"
	"github.com/aicw/aicw_node/pkg/identity"
	"github.com/fystack/mpcium/pkg/logger"
)

// ReadinessCheckPeriod is how often to check peer readiness.
const ReadinessCheckPeriod = 1 * time.Second

// ECDHBroadcastPeriod is how often to re-broadcast ECDH public key
// until key exchange is complete with all peers.
const ECDHBroadcastPeriod = 3 * time.Second

// ReadySessionTTL bounds how long a ready/ entry outlives the node that wrote
// it. Consul may take up to twice the TTL to reap an expired session.
const ReadySessionTTL = 30 * time.Second

// readyReregisterBackoffMin/Max bound delays between ready/ re-registration
// attempts after the Consul session renewal loop stops.
const (
	readyReregisterBackoffMin = 5 * time.Second
	readyReregisterBackoffMax = 60 * time.Second
)

// DynamicPeerRegistry extends PeerRegistry with dynamic peer management.
//
// AICW-FORK: New interface that supports runtime peer additions/removals.
type DynamicPeerRegistry interface {
	// Ready marks this node as ready in Consul.
	Ready() error

	// Resign removes this node from Consul.
	Resign() error

	// ArePeersReady checks if enough peers are ready.
	ArePeersReady() bool

	// AreCeremonyReady reports whether every member of the given committee is
	// Consul-ready AND has an established symmetric key with this node
	// (committee-local ECDH gate). Used before a keygen/reshare/sign ceremony.
	// AICW-FORK (P0-a, §13.3).
	AreCeremonyReady(committee []string) bool

	// EnsureCeremonyECDH scopes ECDH to the committee and (re)starts the
	// broadcast so symmetric keys with all committee members are established.
	// AICW-FORK (P0-a, §13.4).
	EnsureCeremonyECDH(committee []string) error

	// EnsureCeremonyReady is the committee-local ceremony gate (§13.3): it
	// scopes/triggers ECDH for the committee and blocks until every member is
	// ceremony-ready or the ECDH-gate timeout elapses. When the committee filter
	// is disabled it falls back to the legacy full-cluster ArePeersReady() gate,
	// so default behavior is unchanged. AICW-FORK (P0-a/P1, §13.3/§13.4).
	EnsureCeremonyReady(committee []string) error

	// CeremonyFilterEnabled reports whether committee-local ceremony mode is on
	// (committee policy set AND keygen filter flag enabled). AICW-FORK (§13.5).
	CeremonyFilterEnabled() bool

	// AreMajorityReady checks if majority (> threshold) are ready.
	AreMajorityReady() bool

	// GetReadyPeersCount returns count of ready peers including self.
	GetReadyPeersCount() int64

	// GetReadyPeersIncludeSelf returns list of ready peer IDs including self.
	GetReadyPeersIncludeSelf() []string

	// GetKeygenParty returns the keygen party (peer IDs including self) for a
	// wallet: the deterministic committee when the filter is enabled, otherwise
	// the full ready set. AICW-FORK (P1, §13.5).
	GetKeygenParty(walletID string) []string

	// GetAllPeerIDs returns all known peer IDs (ready or not).
	GetAllPeerIDs() []string

	// AICW-FORK: New methods for dynamic peer management

	// AddPeer adds a new peer to the registry after membership verification.
	// Returns error if the peer fails verification or is already registered.
	AddPeer(nodeID string, publicKey []byte) error

	// RemovePeer removes a peer from the registry.
	// Also removes symmetric keys and ECDH state for the peer.
	RemovePeer(nodeID string) error

	// WatchPeerDirectory watches Consul for peer changes and updates registry.
	WatchPeerDirectory() error

	// SetMembershipVerifier sets the verifier for new peer membership.
	SetMembershipVerifier(verifier eligibility.MembershipVerifier)

	// Callbacks
	OnPeerConnected(callback func(peerID string))
	OnPeerDisconnected(callback func(peerID string))
	OnPeerReConnected(callback func(peerID string))
	OnNewPeerJoined(callback func(peerID string))
}

// DynamicRegistry implements DynamicPeerRegistry.
type DynamicRegistry struct {
	mu sync.RWMutex

	// Self
	nodeID string

	// Dynamic peer list (can grow/shrink at runtime)
	peerNodeIDs map[string]struct{}

	// Readiness tracking
	readyMap   map[string]bool
	readyCount int64
	ready      bool

	// Configuration
	mpcThreshold int

	// Dependencies
	consulClient       *api.Client
	identityStore      identity.DynamicStore
	membershipVerifier eligibility.MembershipVerifier

	// AICW-FORK: ECDH session for dynamic key exchange
	ecdhSession DynamicECDHSession

	// Callbacks
	onPeerConnected    func(peerID string)
	onPeerDisconnected func(peerID string)
	onPeerReConnected  func(peerID string)
	onNewPeerJoined    func(peerID string)

	// Watch control
	watchStopCh chan struct{}
	resignOnce  sync.Once

	// AICW-FORK: Consul session backing the ready/ key. The key is acquired
	// through this session with delete-on-expiry behavior, so a crash, kill -9
	// or power loss retires the node from the ready set without any operator
	// action. Resign() still removes it immediately on a clean shutdown.
	readySessionID     string
	readySessionStopCh chan struct{}

	// AICW-FORK: ECDH periodic broadcast control
	ecdhBroadcastStopCh chan struct{}

	// AICW-FORK (P0-b, §1.7): tracks whether periodicECDHBroadcast is currently
	// running (1) or has stopped after the grace period (0). Used to (a) restart
	// the broadcast loop when a peer rejoins in steady state, and (b) distinguish
	// a genuine post-grace rejoin from the initial startup exchange.
	ecdhBroadcastRunning int32

	// AICW-FORK (P1, §13.5): committee-selection policy for the keygen party
	// filter. Set once at startup (before ready/watch goroutines run). nil or a
	// disabled filter flag means keygen uses the full ready set (legacy behavior).
	committeePolicy *committee.Policy

	// AICW-FORK (§13.3): how long EnsureCeremonyReady blocks waiting for
	// committee-local ECDH to complete before returning an ecdh_not_ready error.
	ecdhGateTimeout time.Duration

	// AICW-FORK (P0-b): last seen ready/ KV value per peer. Detects restart
	// without Resign() (kill -9) where the ready key persists but the value
	// changes on the next Ready() call.
	readyEpoch map[string]string
}

// DefaultECDHGateTimeout is the committee ECDH-gate wait budget (§13.3, §13.8).
const DefaultECDHGateTimeout = 120 * time.Second

// NewDynamicRegistry creates a new dynamic peer registry.
//
// Unlike the original registry, this does not require a pre-populated peer list.
// Peers are discovered dynamically from Consul.
func NewDynamicRegistry(
	nodeID string,
	mpcThreshold int,
	consulClient *api.Client,
	identityStore identity.DynamicStore,
) *DynamicRegistry {
	if mpcThreshold < 1 {
		panic("mpc_threshold must be greater than 0")
	}

	reg := &DynamicRegistry{
		nodeID:              nodeID,
		peerNodeIDs:         make(map[string]struct{}),
		readyMap:            make(map[string]bool),
		readyCount:          1, // self
		mpcThreshold:        mpcThreshold,
		consulClient:        consulClient,
		identityStore:       identityStore,
		watchStopCh:         make(chan struct{}),
		ecdhBroadcastStopCh: make(chan struct{}),
		ecdhGateTimeout:     DefaultECDHGateTimeout,
		readyEpoch:          make(map[string]string),
	}

	return reg
}

// AddPeer adds a new peer to the registry after membership verification.
func (r *DynamicRegistry) AddPeer(nodeID string, publicKey []byte) error {
	if nodeID == r.nodeID {
		return fmt.Errorf("cannot add self as peer")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if already registered
	if _, exists := r.peerNodeIDs[nodeID]; exists {
		// AICW-FORK (P0-b): peer is already in the registry (e.g. rejoin after
		// Consul directory watch did not fire). Still trigger ECDH renegotiation
		// so stale symmetric keys are flushed and keys are re-derived.
		if r.ecdhSession != nil {
			if err := r.ecdhSession.AddPeer(nodeID); err != nil {
				fmt.Printf("warning: ECDH AddPeer on rejoin failed for %s: %v\n", nodeID, err)
			}
		}
		return nil
	}

	// Verify membership if verifier is set.
	// This is the authoritative security gate for joining the registry: an
	// unverified or mismatched key is rejected here regardless of what the
	// identity store already holds.
	if r.membershipVerifier != nil {
		if err := r.membershipVerifier.VerifyMembership(nodeID, publicKey, nil); err != nil {
			return fmt.Errorf("peer membership verification failed: %w", err)
		}
	}

	// Register public key in identity store (idempotent).
	//
	// AICW-FORK FIX (P0): peers preloaded via LoadPeersFromConsul() are already
	// present in the store with the same raw key. Re-registering would run the
	// store's membership verification a second time and overwrite an identical
	// value for no reason. If the store already holds the exact same key, we
	// add the peer to the registry only. Membership has already been verified
	// above, so this does not weaken the security check.
	if existing, err := r.identityStore.GetPublicKey(nodeID); err != nil || !bytes.Equal(existing, publicKey) {
		if err := r.identityStore.RegisterPeerPublicKey(nodeID, publicKey); err != nil {
			return fmt.Errorf("failed to register peer public key: %w", err)
		}
	}

	// Add to peer list
	r.peerNodeIDs[nodeID] = struct{}{}

	// AICW-FORK: Trigger ECDH key exchange with new peer
	// This causes all existing nodes to re-broadcast their public keys,
	// allowing the new peer to establish symmetric keys with everyone.
	if r.ecdhSession != nil {
		if err := r.ecdhSession.AddPeer(nodeID); err != nil {
			// Warning only - don't fail the entire AddPeer operation
			fmt.Printf("warning: ECDH AddPeer failed for %s: %v\n", nodeID, err)
		}
	}

	// Invoke callback
	if r.onNewPeerJoined != nil {
		go r.onNewPeerJoined(nodeID)
	}

	return nil
}

// SetECDHSession sets the ECDH session for dynamic key exchange.
// AICW-FORK: This connects the registry to the ECDH session so that
// new peer additions trigger key exchange.
func (r *DynamicRegistry) SetECDHSession(session DynamicECDHSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ecdhSession = session
}

// RemovePeer removes a peer from the registry.
func (r *DynamicRegistry) RemovePeer(nodeID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.peerNodeIDs[nodeID]; !exists {
		return fmt.Errorf("peer not found: %s", nodeID)
	}

	// Remove from peer list
	delete(r.peerNodeIDs, nodeID)

	// Update ready state
	if r.readyMap[nodeID] {
		atomic.AddInt64(&r.readyCount, -1)
	}
	delete(r.readyMap, nodeID)

	// Remove from identity store
	_ = r.identityStore.UnregisterPeerPublicKey(nodeID)

	// Invoke callback
	if r.onPeerDisconnected != nil {
		go r.onPeerDisconnected(nodeID)
	}

	// Update overall ready state
	r.updateReadyState()

	return nil
}

// WatchPeerDirectory watches Consul for peer changes.
func (r *DynamicRegistry) WatchPeerDirectory() error {
	if r.consulClient == nil {
		return fmt.Errorf("consul client not configured")
	}

	// Start watching identity store's peer directory
	return r.identityStore.WatchPeerDirectory(func(nodeID string, added bool) {
		if added {
			// New peer detected - get their public key and add
			pubKey, err := r.identityStore.GetPublicKey(nodeID)
			if err != nil {
				fmt.Printf("warning: could not get public key for new peer %s: %v\n", nodeID, err)
				return
			}
			if err := r.AddPeer(nodeID, pubKey); err != nil {
				fmt.Printf("warning: could not add peer %s: %v\n", nodeID, err)
			}
		} else {
			// Peer removed
			if err := r.RemovePeer(nodeID); err != nil {
				fmt.Printf("warning: could not remove peer %s: %v\n", nodeID, err)
			}
		}
	})
}

// createReadySession opens a TTL session whose expiry deletes the ready/ key.
// Renewal is driven by maintainReadySession(), not here.
func (r *DynamicRegistry) createReadySession() (string, error) {
	sessionID, _, err := r.consulClient.Session().Create(&api.SessionEntry{
		Name:      fmt.Sprintf("node-ready-%s", r.nodeID),
		TTL:       ReadySessionTTL.String(),
		Behavior:  api.SessionBehaviorDelete,
		LockDelay: 1 * time.Millisecond,
	}, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create ready session: %w", err)
	}

	stopCh := make(chan struct{})
	r.mu.Lock()
	r.readySessionID = sessionID
	r.readySessionStopCh = stopCh
	r.mu.Unlock()

	return sessionID, nil
}

// registerReady creates a Consul session and acquires the ready/ key for this node.
func (r *DynamicRegistry) registerReady() error {
	if r.consulClient == nil {
		return fmt.Errorf("consul client not configured")
	}

	sessionID, err := r.createReadySession()
	if err != nil {
		return err
	}

	key := fmt.Sprintf("ready/%s", r.nodeID)
	epoch := strconv.FormatInt(time.Now().UnixNano(), 10)
	acquired, _, err := r.consulClient.KV().Acquire(&api.KVPair{
		Key:     key,
		Value:   []byte(epoch),
		Session: sessionID,
	}, nil)
	if err != nil {
		return fmt.Errorf("failed to set ready key: %w", err)
	}
	if !acquired {
		if _, derr := r.consulClient.KV().Delete(key, nil); derr != nil {
			return fmt.Errorf("failed to clear stale ready key: %w", derr)
		}
		if _, _, err := r.consulClient.KV().Acquire(&api.KVPair{
			Key:     key,
			Value:   []byte(epoch),
			Session: sessionID,
		}, nil); err != nil {
			return fmt.Errorf("failed to set ready key: %w", err)
		}
	}
	return nil
}

func nextReadyReregisterBackoff(current time.Duration) time.Duration {
	next := current * 2
	if next > readyReregisterBackoffMax {
		return readyReregisterBackoffMax
	}
	return next
}

// maintainReadySession keeps the ready/ key alive and re-registers after session loss.
func (r *DynamicRegistry) maintainReadySession() {
	for {
		// Resign() may land between a successful re-register and this iteration;
		// bail out before renewing so the leaked session dies by TTL instead of
		// being kept alive for a node that already resigned.
		select {
		case <-r.watchStopCh:
			return
		default:
		}

		r.mu.RLock()
		sessionID := r.readySessionID
		stopCh := r.readySessionStopCh
		r.mu.RUnlock()

		if sessionID == "" || stopCh == nil {
			return
		}

		err := r.consulClient.Session().RenewPeriodic(
			ReadySessionTTL.String(), sessionID, nil, stopCh,
		)

		select {
		case <-r.watchStopCh:
			return
		default:
		}

		if err != nil {
			logger.Warn("Ready session renewal stopped", "nodeID", r.nodeID, "error", err.Error())
		}
		logger.Warn("ready session lost; re-registering", "nodeID", r.nodeID)

		backoff := readyReregisterBackoffMin
		for {
			select {
			case <-r.watchStopCh:
				return
			default:
			}

			if err := r.registerReady(); err == nil {
				break
			}

			select {
			case <-r.watchStopCh:
				return
			case <-time.After(backoff):
			}
			backoff = nextReadyReregisterBackoff(backoff)
		}
	}
}

// Ready marks this node as ready in Consul.
func (r *DynamicRegistry) Ready() error {
	if r.consulClient == nil {
		return fmt.Errorf("consul client not configured")
	}

	// AICW-FORK: Start ECDH key exchange before marking ready
	// This mirrors the original registry behavior where ECDH exchange
	// must be started before the node can participate in MPC operations.
	if r.ecdhSession != nil {
		if err := r.ecdhSession.ListenKeyExchange(); err != nil {
			return fmt.Errorf("failed to start ECDH listener: %w", err)
		}
		if err := r.ecdhSession.BroadcastPublicKey(); err != nil {
			return fmt.Errorf("failed to broadcast ECDH public key: %w", err)
		}

		// AICW-FORK: Start periodic ECDH re-broadcast loop
		// This ensures late-joining nodes receive our public key even if they
		// missed our initial broadcast (NATS pub/sub message loss).
		// The loop stops automatically when key exchange is complete.
		r.ensurePeriodicECDHBroadcast()
	}

	if err := r.registerReady(); err != nil {
		return err
	}

	go r.maintainReadySession()

	// Start watching for peer readiness
	go r.watchPeersReady()

	return nil
}

// ECDHGracePeriod is how long to continue broadcasting after ECDH exchange
// appears complete, to ensure late-joining nodes receive our public key.
const ECDHGracePeriod = 30 * time.Second

// ensurePeriodicECDHBroadcast starts the periodic ECDH re-broadcast loop if it
// is not already running.
//
// AICW-FORK (P0-b, §1.7 item E): the loop terminates after ECDH completes plus
// the grace period. When a peer later rejoins (e.g. a rolling restart with a
// fresh ephemeral DH key), we must resume broadcasting so the rejoining node
// can receive our public key again. The atomic CAS guarantees at most one loop
// runs concurrently, so repeated calls (startup + every rejoin) are safe.
func (r *DynamicRegistry) ensurePeriodicECDHBroadcast() {
	if r.ecdhSession == nil {
		return
	}
	if atomic.CompareAndSwapInt32(&r.ecdhBroadcastRunning, 0, 1) {
		go r.periodicECDHBroadcast()
	}
}

// periodicECDHBroadcast periodically re-broadcasts ECDH public key until
// key exchange is complete with all peers, plus a grace period.
//
// AICW-FORK: This solves the timing problem where late-joining nodes miss
// the initial ECDH broadcast due to NATS pub/sub message loss.
// By continuously re-broadcasting, we ensure that eventually all nodes
// receive each other's public keys, regardless of start order.
//
// The grace period ensures that even after our own ECDH exchange is complete,
// we continue broadcasting for late-joining nodes that may have received
// our earlier broadcasts but not yet sent theirs.
func (r *DynamicRegistry) periodicECDHBroadcast() {
	// AICW-FORK (P0-b): mark the loop stopped on exit so ensurePeriodicECDHBroadcast
	// can restart it when a peer rejoins after the grace period.
	defer atomic.StoreInt32(&r.ecdhBroadcastRunning, 0)

	ticker := time.NewTicker(ECDHBroadcastPeriod)
	defer ticker.Stop()

	var graceDeadline time.Time
	graceActive := false

	for {
		select {
		case <-r.ecdhBroadcastStopCh:
			return
		case <-r.watchStopCh:
			return
		case <-ticker.C:
			// Check if ECDH exchange is complete
			if r.ecdhSession == nil {
				return
			}

			expectedPeers := r.ecdhSession.GetMeshExpectedPeerCount()
			completedKeys := r.ecdhSession.GetMeshCompletedKeyCount()

			// Check if we've completed key exchange with every mesh peer.
			// Use mesh-scoped counts so a lingering ceremonyPeers scope (3) does
			// not falsely complete while a rejoining peer still needs a 4th key.
			if expectedPeers > 0 && completedKeys >= expectedPeers {
				if !graceActive {
					// Start grace period
					graceActive = true
					graceDeadline = time.Now().Add(ECDHGracePeriod)
					fmt.Printf("[ECDH] Mesh key exchange complete: %d/%d symmetric keys established. Continuing broadcast for %.0fs grace period.\n",
						completedKeys, expectedPeers, ECDHGracePeriod.Seconds())
				}

				// Check if grace period expired
				if time.Now().After(graceDeadline) {
					fmt.Printf("[ECDH] Grace period ended. Stopping periodic broadcast.\n")
					return
				}
			} else {
				// If we were in grace but now need more keys, reset grace
				if graceActive {
					graceActive = false
					fmt.Printf("[ECDH] New peer detected during grace period. Resuming active broadcast.\n")
				}
			}

			// Re-broadcast public key
			if err := r.ecdhSession.BroadcastPublicKey(); err != nil {
				fmt.Printf("warning: periodic ECDH broadcast failed: %v\n", err)
			} else if !graceActive {
				fmt.Printf("[ECDH] Periodic re-broadcast: %d/%d mesh keys (broadcasting until complete)\n",
					completedKeys, expectedPeers)
			}
		}
	}
}

// handleECDHRejoin recovers ECDH state when a peer transitions back into the
// ready state (or publishes a new ready epoch after restart without Resign).
//
// AICW-FORK (P0-b, §1.7): always flush the stale symmetric key, trigger
// AddPeer renegotiation, and resume periodic broadcast — even if the broadcast
// loop is still running. The loop may have falsely stopped (4/3 complete) or
// be in grace while the rejoining peer still lacks a key.
func (r *DynamicRegistry) handleECDHRejoin(peerID string) {
	if r.ecdhSession == nil {
		return
	}

	if r.identityStore != nil {
		r.identityStore.RemoveSymmetricKey(peerID)
	}

	if err := r.ecdhSession.AddPeer(peerID); err != nil {
		fmt.Printf("warning: ECDH AddPeer on peer %s rejoin failed: %v\n", peerID, err)
	}

	r.ensurePeriodicECDHBroadcast()
	if err := r.ecdhSession.BroadcastPublicKey(); err != nil {
		fmt.Printf("warning: ECDH re-broadcast on peer %s rejoin failed: %v\n", peerID, err)
		return
	}

	fmt.Printf("[ECDH] Peer %s re-ready detected; reset stale symmetric key and resumed broadcast for re-negotiation\n", peerID)
}

// Resign removes this node from Consul.
func (r *DynamicRegistry) Resign() error {
	var resignErr error
	r.resignOnce.Do(func() {
		if r.consulClient == nil {
			return
		}

		select {
		case <-r.ecdhBroadcastStopCh:
		default:
			close(r.ecdhBroadcastStopCh)
		}

		close(r.watchStopCh)

		r.mu.Lock()
		sessionID := r.readySessionID
		stopCh := r.readySessionStopCh
		r.readySessionID = ""
		r.readySessionStopCh = nil
		r.mu.Unlock()

		if stopCh != nil {
			close(stopCh)
		}

		key := fmt.Sprintf("ready/%s", r.nodeID)
		_, err := r.consulClient.KV().Delete(key, nil)
		if err != nil {
			resignErr = fmt.Errorf("failed to delete ready key: %w", err)
		}

		if sessionID != "" {
			if _, derr := r.consulClient.Session().Destroy(sessionID, nil); derr != nil && resignErr == nil {
				resignErr = fmt.Errorf("failed to destroy ready session: %w", derr)
			}
		}
	})
	return resignErr
}

// watchPeersReady monitors Consul for peer readiness changes.
func (r *DynamicRegistry) watchPeersReady() {
	ticker := time.NewTicker(ReadinessCheckPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-r.watchStopCh:
			return
		case <-ticker.C:
			r.checkPeerReadiness()
		}
	}
}

func (r *DynamicRegistry) checkPeerReadiness() {
	if r.consulClient == nil {
		return
	}

	pairs, _, err := r.consulClient.KV().List("ready/", nil)
	if err != nil {
		fmt.Printf("warning: failed to list ready keys: %v\n", err)
		return
	}

	// Build set of ready peers and their epoch values
	readyPeers := make(map[string]string)
	for _, pair := range pairs {
		var peerID string
		_, err := fmt.Sscanf(pair.Key, "ready/%s", &peerID)
		if err != nil || peerID == r.nodeID {
			continue
		}
		readyPeers[peerID] = string(pair.Value)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Process peer readiness changes
	for peerID := range r.peerNodeIDs {
		epoch, isReady := readyPeers[peerID]
		wasReady := r.readyMap[peerID]
		prevEpoch := r.readyEpoch[peerID]

		if isReady && !wasReady {
			// Peer became ready
			r.readyMap[peerID] = true
			atomic.AddInt64(&r.readyCount, 1)
			if r.onPeerConnected != nil {
				go r.onPeerConnected(peerID)
			}
			go r.handleECDHRejoin(peerID)
		} else if isReady && wasReady && epoch != prevEpoch {
			// Peer restarted without Resign(): ready key persisted but epoch changed.
			go r.handleECDHRejoin(peerID)
		} else if !isReady && wasReady {
			// Peer became not ready
			r.readyMap[peerID] = false
			atomic.AddInt64(&r.readyCount, -1)
			if r.identityStore != nil {
				r.identityStore.RemoveSymmetricKey(peerID)
			}
			if r.onPeerDisconnected != nil {
				go r.onPeerDisconnected(peerID)
			}
		}

		if isReady {
			r.readyEpoch[peerID] = epoch
		} else {
			delete(r.readyEpoch, peerID)
		}
	}

	r.updateReadyState()
}

func (r *DynamicRegistry) updateReadyState() {
	readyCount := 0
	for _, isReady := range r.readyMap {
		if isReady {
			readyCount++
		}
	}

	totalPeers := len(r.peerNodeIDs)

	// AICW-FORK FIX (P1): avoid the vacuous-truth bug. With no known peers,
	// readyCount == totalPeers is 0 == 0 == true, which previously made a
	// solo node believe the whole cluster was ready. A node with zero peers
	// is never "all peers ready".
	if totalPeers == 0 {
		r.ready = false
		return
	}

	r.ready = readyCount == totalPeers
}

// ArePeersReady reports whether the cluster is ready to start an MPC keygen.
//
// AICW-FORK FIX (P1): This is the gate the KeygenConsumer polls before it
// begins consuming keygen messages. It must return true ONLY when a real
// multi-party quorum exists, otherwise a lone node starts a solo (1-party)
// keygen while the Bridge reports a misleading HTTP 200 ("success-looking
// failure"). The gate therefore requires all of:
//
//  1. At least one known peer (no vacuous truth on an empty peer set).
//  2. Total participants including self >= mpcThreshold+1 (t+1 quorum floor).
//  3. Every known peer is currently ready (r.ready — full participation).
//  4. Ready participants including self >= mpcThreshold+1.
//
// These conditions only make the gate stricter; a correctly-formed cluster
// still passes and a solo node is correctly held back.
func (r *DynamicRegistry) ArePeersReady() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.peerNodeIDs) == 0 {
		return false
	}

	totalParticipants := int64(len(r.peerNodeIDs)) + 1 // include self
	if totalParticipants < int64(r.mpcThreshold+1) {
		return false
	}

	if !r.ready {
		return false
	}

	readyParticipants := atomic.LoadInt64(&r.readyCount) // self + ready peers
	if readyParticipants < int64(r.mpcThreshold+1) {
		return false
	}

	// AICW-FORK (P0-b, §1.7 item D — ECDH ceremony gate): Consul "ready" alone
	// is not sufficient to start a keygen. A node can be marked ready while its
	// symmetric-key exchange is still incomplete (the "2/4 keys" state). If a
	// ceremony starts in that window it fails mid-flight with "symmetric key not
	// found" / "ciphertext too short, tampered message". Hold the gate until the
	// local ECDH exchange is complete with every known peer. This only tightens
	// the gate: a fully-exchanged cluster still passes.
	if r.ecdhSession != nil {
		expected := r.ecdhSession.GetMeshExpectedPeerCount()
		if expected == 0 {
			return false
		}
		if r.ecdhSession.GetMeshCompletedKeyCount() < expected {
			return false
		}
	}

	return true
}

// AreCeremonyReady reports whether the given committee is ready to run a
// keygen/reshare/sign ceremony from this node's perspective.
//
// AICW-FORK (P0-a, §13.3 — committee-local ECDH gate): unlike ArePeersReady
// (which reasons about the whole registry / full-mesh), this checks only the
// committee members. A member is ceremony-ready when it is both Consul-ready
// AND this node holds an established symmetric key with it. This is the precise
// gate the keygen/reshare/sign paths use once SelectCommittee (P1) decides the
// committee, so a large network only needs ECDH within the committee.
func (r *DynamicRegistry) AreCeremonyReady(committee []string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Quorum floor: a committee must have at least t+1 members.
	if len(committee) < r.mpcThreshold+1 {
		return false
	}

	selfIncluded := false
	for _, id := range committee {
		if id == r.nodeID {
			selfIncluded = true
			continue
		}
		// Consul readiness for this committee member.
		if !r.readyMap[id] {
			return false
		}
		// Committee-local ECDH: a symmetric key with this member must exist.
		if r.identityStore != nil {
			if _, err := r.identityStore.GetSymmetricKey(id); err != nil {
				return false
			}
		}
	}

	// This node must itself be a member of the committee to be "ceremony ready";
	// a non-member skips the ceremony (handled by the keygen party filter in P1).
	return selfIncluded
}

// EnsureCeremonyECDH scopes ECDH to the committee and ensures the periodic
// broadcast is running so symmetric keys with every committee member converge.
//
// AICW-FORK (P0-a, §13.4 point 4): invoked just before a ceremony (wired in P1).
func (r *DynamicRegistry) EnsureCeremonyECDH(committee []string) error {
	if r.ecdhSession == nil {
		return fmt.Errorf("ECDH session not configured")
	}
	// Make sure the broadcast loop is active even if it stopped after grace.
	r.ensurePeriodicECDHBroadcast()
	return r.ecdhSession.EnsureECDH(committee)
}

// CeremonyFilterEnabled reports whether committee-local ceremony mode is active.
// AICW-FORK (§13.5).
func (r *DynamicRegistry) CeremonyFilterEnabled() bool {
	return r.committeePolicy != nil && committee.KeygenFilterEnabled()
}

// SetECDHGateTimeout overrides the committee ECDH-gate wait budget (§13.3).
// Call once at startup. AICW-FORK.
func (r *DynamicRegistry) SetECDHGateTimeout(d time.Duration) {
	if d > 0 {
		r.ecdhGateTimeout = d
	}
}

// EnsureCeremonyReady is the committee-local ceremony gate (§13.3/§13.4).
//
//   - Legacy mode (committee filter off): preserve the full-cluster gate
//     (ArePeersReady), so 5-node/test deployments behave exactly as before.
//   - Committee mode, no committee context (empty list — e.g. the consumer
//     startup gate): require only a signing quorum (t+1) to be ready, since the
//     wallet's committee is not known until a request arrives.
//   - Committee mode with a committee: scope+trigger ECDH for the committee and
//     block until every member is ceremony-ready (Consul-ready + symmetric key)
//     or the ECDH-gate timeout elapses.
func (r *DynamicRegistry) EnsureCeremonyReady(committee []string) error {
	if !r.CeremonyFilterEnabled() {
		if !r.ArePeersReady() {
			return fmt.Errorf("cluster not ready")
		}
		return nil
	}

	if len(committee) == 0 {
		if !r.AreMajorityReady() {
			return fmt.Errorf("quorum not ready")
		}
		return nil
	}

	if err := r.EnsureCeremonyECDH(committee); err != nil {
		return fmt.Errorf("ensure committee ECDH: %w", err)
	}

	timeout := r.ecdhGateTimeout
	if timeout <= 0 {
		timeout = DefaultECDHGateTimeout
	}
	deadline := time.Now().Add(timeout)
	for {
		if r.AreCeremonyReady(committee) {
			// Restore full-mesh counting so periodic broadcast does not falsely
			// complete at committee size after a keygen ceremony.
			if r.ecdhSession != nil {
				r.ecdhSession.ClearCeremonyScope()
			}
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("ecdh_not_ready: committee ECDH incomplete after %s", timeout)
		}
		time.Sleep(ReadinessCheckPeriod)
	}
}

// AreMajorityReady returns true if majority peers are ready.
func (r *DynamicRegistry) AreMajorityReady() bool {
	readyCount := atomic.LoadInt64(&r.readyCount)
	return int(readyCount) >= r.mpcThreshold+1
}

// GetReadyPeersCount returns count of ready peers including self.
func (r *DynamicRegistry) GetReadyPeersCount() int64 {
	return atomic.LoadInt64(&r.readyCount)
}

// GetReadyPeersIncludeSelf returns list of ready peer IDs including self.
func (r *DynamicRegistry) GetReadyPeersIncludeSelf() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	peerIDs := make([]string, 0, len(r.readyMap)+1)
	for peerID, isReady := range r.readyMap {
		if isReady {
			peerIDs = append(peerIDs, peerID)
		}
	}
	peerIDs = append(peerIDs, r.nodeID)
	return peerIDs
}

// SetCommitteePolicy sets the committee-selection policy used by the keygen
// party filter. Call once at startup before Ready(). AICW-FORK (P1, §13.5).
func (r *DynamicRegistry) SetCommitteePolicy(p committee.Policy) {
	r.committeePolicy = &p
}

// GetKeygenParty returns the party (peer IDs including self) for a wallet's
// keygen. When the committee filter is disabled (default) or no policy is set,
// it returns the full ready set — identical to the legacy behavior. When
// enabled, it returns the deterministic, tier-sized committee (§13.5).
//
// AICW-FORK: this is the mpcium PeerRegistry hook that replaces
// GetReadyPeersIncludeSelf() as the keygen party source (§13.5). Committee
// selection is deterministic (committee.SelectCommittee), so every node that
// shares the same ready view computes the same committee — a hard requirement
// since the committee is not carried in the keygen message. On selection error
// with filtering enabled it returns nil (never the full ready set).
func (r *DynamicRegistry) GetKeygenParty(walletID string) []string {
	ready := r.GetReadyPeersIncludeSelf()

	if r.committeePolicy == nil || !committee.KeygenFilterEnabled() {
		return ready
	}

	plan, err := r.committeePolicy.SelectCommittee(walletID, nil, ready)
	if err != nil {
		// Never fall back to the full ready set when filtering is enabled —
		// an oversized keygen party triggers immediate migrate_oversized churn
		// and breaks post-reshare signing (Consul v2 without usable Badger v2).
		logger.Error(
			"committee selection failed for keygen; refusing oversized party",
			err,
			"walletID", walletID,
			"readyCount", len(ready),
		)
		return nil
	}
	return plan.NodeIDs
}

// GetAllPeerIDs returns all known peer IDs.
func (r *DynamicRegistry) GetAllPeerIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := make([]string, 0, len(r.peerNodeIDs))
	for id := range r.peerNodeIDs {
		ids = append(ids, id)
	}
	return ids
}

// SetMembershipVerifier sets the verifier for new peer membership.
func (r *DynamicRegistry) SetMembershipVerifier(verifier eligibility.MembershipVerifier) {
	r.membershipVerifier = verifier
}

// Callback setters
func (r *DynamicRegistry) OnPeerConnected(callback func(peerID string)) {
	r.onPeerConnected = callback
}

func (r *DynamicRegistry) OnPeerDisconnected(callback func(peerID string)) {
	r.onPeerDisconnected = callback
}

func (r *DynamicRegistry) OnPeerReConnected(callback func(peerID string)) {
	r.onPeerReConnected = callback
}

func (r *DynamicRegistry) OnNewPeerJoined(callback func(peerID string)) {
	r.onNewPeerJoined = callback
}

// GetNodeID returns this node's ID.
func (r *DynamicRegistry) GetNodeID() string {
	return r.nodeID
}

// === Original mpcium PeerRegistry methods (for interface compatibility) ===

// WatchPeersReady starts watching Consul for peer readiness changes.
// This is the public wrapper for the private watchPeersReady method.
// Implements mpcium PeerRegistry interface.
func (r *DynamicRegistry) WatchPeersReady() {
	go r.watchPeersReady()
}

// GetReadyPeersCountExcludeSelf returns the count of ready peers excluding self.
// Implements mpcium PeerRegistry interface.
func (r *DynamicRegistry) GetReadyPeersCountExcludeSelf() int64 {
	return r.GetReadyPeersCount() - 1
}

// GetTotalPeersCount returns the total number of peers including self.
// Implements mpcium PeerRegistry interface.
func (r *DynamicRegistry) GetTotalPeersCount() int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var self int64 = 1
	return int64(len(r.peerNodeIDs)) + self
}
