// Package mpc provides dynamic peer registry for AICW MPC network.
//
// AICW-FORK: This file extends the original Mpcium registry with dynamic
// peer management capabilities. Peers can join and leave at runtime.
package mpc

import (
	"bytes"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/consul/api"

	"github.com/aicw/aicw_node/pkg/eligibility"
	"github.com/aicw/aicw_node/pkg/identity"
)

// ReadinessCheckPeriod is how often to check peer readiness.
const ReadinessCheckPeriod = 1 * time.Second

// ECDHBroadcastPeriod is how often to re-broadcast ECDH public key
// until key exchange is complete with all peers.
const ECDHBroadcastPeriod = 3 * time.Second

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

	// AreMajorityReady checks if majority (> threshold) are ready.
	AreMajorityReady() bool

	// GetReadyPeersCount returns count of ready peers including self.
	GetReadyPeersCount() int64

	// GetReadyPeersIncludeSelf returns list of ready peer IDs including self.
	GetReadyPeersIncludeSelf() []string

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

	// AICW-FORK: ECDH periodic broadcast control
	ecdhBroadcastStopCh chan struct{}
}

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
		return fmt.Errorf("peer already registered: %s", nodeID)
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
		go r.periodicECDHBroadcast()
	}

	key := fmt.Sprintf("ready/%s", r.nodeID)
	_, err := r.consulClient.KV().Put(&api.KVPair{
		Key:   key,
		Value: []byte("true"),
	}, nil)

	if err != nil {
		return fmt.Errorf("failed to set ready key: %w", err)
	}

	// Start watching for peer readiness
	go r.watchPeersReady()

	return nil
}

// ECDHGracePeriod is how long to continue broadcasting after ECDH exchange
// appears complete, to ensure late-joining nodes receive our public key.
const ECDHGracePeriod = 30 * time.Second

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

			expectedPeers := r.ecdhSession.GetExpectedPeerCount()
			completedKeys := r.ecdhSession.GetCompletedKeyCount()

			// Check if we've completed key exchange
			if expectedPeers > 0 && completedKeys >= expectedPeers {
				if !graceActive {
					// Start grace period
					graceActive = true
					graceDeadline = time.Now().Add(ECDHGracePeriod)
					fmt.Printf("[ECDH] Key exchange complete: %d/%d symmetric keys established. Continuing broadcast for %.0fs grace period.\n",
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
				fmt.Printf("[ECDH] Periodic re-broadcast: %d/%d keys (broadcasting until complete)\n",
					completedKeys, expectedPeers)
			}
		}
	}
}

// Resign removes this node from Consul.
func (r *DynamicRegistry) Resign() error {
	if r.consulClient == nil {
		return nil
	}

	// Stop periodic ECDH broadcast
	select {
	case <-r.ecdhBroadcastStopCh:
		// Already closed
	default:
		close(r.ecdhBroadcastStopCh)
	}

	close(r.watchStopCh)

	key := fmt.Sprintf("ready/%s", r.nodeID)
	_, err := r.consulClient.KV().Delete(key, nil)
	if err != nil {
		return fmt.Errorf("failed to delete ready key: %w", err)
	}

	return nil
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

	// Build set of ready peers
	readyPeers := make(map[string]struct{})
	for _, pair := range pairs {
		var peerID string
		_, err := fmt.Sscanf(pair.Key, "ready/%s", &peerID)
		if err != nil || peerID == r.nodeID {
			continue
		}
		readyPeers[peerID] = struct{}{}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Process peer readiness changes
	for peerID := range r.peerNodeIDs {
		_, isReady := readyPeers[peerID]
		wasReady := r.readyMap[peerID]

		if isReady && !wasReady {
			// Peer became ready
			r.readyMap[peerID] = true
			atomic.AddInt64(&r.readyCount, 1)
			if r.onPeerConnected != nil {
				go r.onPeerConnected(peerID)
			}
			// AICW-FORK: Removed 500ms hardcoded delay re-broadcast.
			// The periodic re-broadcast loop (periodicECDHBroadcast) now handles
			// ensuring late-joining nodes receive our public key.
		} else if !isReady && wasReady {
			// Peer became not ready
			r.readyMap[peerID] = false
			atomic.AddInt64(&r.readyCount, -1)
			if r.onPeerDisconnected != nil {
				go r.onPeerDisconnected(peerID)
			}
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
	return readyParticipants >= int64(r.mpcThreshold+1)
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
