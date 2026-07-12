// Package mpc provides dynamic ECDH key exchange for AICW MPC network.
//
// AICW-FORK: This file extends the original ECDHSession with dynamic
// peer management capabilities. Peers can be added at runtime.
package mpc

import (
	"fmt"
	"sync"

	"github.com/aicw/aicw_node/pkg/identity"
	mpciumpc "github.com/fystack/mpcium/pkg/mpc"
)

// DynamicECDHSession extends ECDHSession with dynamic peer management.
//
// AICW-FORK: New interface that supports adding peers at runtime.
// The original ecdhSession has peerIDs fixed at creation time.
type DynamicECDHSession interface {
	// AddPeer adds a new peer to the ECDH session.
	// This triggers key exchange with the new peer by re-broadcasting
	// this node's public key so the new peer can receive it.
	AddPeer(peerID string) error

	// RemovePeer removes a peer from the ECDH session.
	// This removes the symmetric key for the peer.
	RemovePeer(peerID string)

	// GetExpectedPeerCount returns the number of expected peers for the active
	// scope (committee when scoped, otherwise full mesh).
	GetExpectedPeerCount() int

	// GetMeshExpectedPeerCount returns the full-mesh peer count (registry-wide).
	// Periodic broadcast and startup gates use this, not the ceremony scope.
	GetMeshExpectedPeerCount() int

	// GetMeshCompletedKeyCount returns how many mesh peers have symmetric keys.
	GetMeshCompletedKeyCount() int

	// ClearCeremonyScope clears committee-local scope after a ceremony gate passes.
	ClearCeremonyScope()

	// SetCeremonyPeers restricts the "expected peers" scope to a specific
	// committee (excluding self). Passing nil/empty clears the scope and
	// restores full-mesh (registry-wide) behavior.
	// AICW-FORK (P0-a, §13.4): committee-local ECDH.
	SetCeremonyPeers(peerIDs []string)

	// EnsureECDH ensures this node is exchanging keys with every committee
	// member, sets the ceremony scope to that committee, and re-broadcasts so
	// symmetric keys are (re)derived. Called just before a ceremony (wired P1).
	// AICW-FORK (P0-a, §13.4).
	EnsureECDH(committee []string) error

	// GetCompletedKeyCount returns the number of completed key exchanges.
	GetCompletedKeyCount() int

	// IsKeyExchangeComplete checks if all expected keys are established.
	IsKeyExchangeComplete() bool

	// SetInnerSession sets the wrapped original ecdhSession.
	// This must be called after the original session is created.
	SetInnerSession(inner mpciumpc.ECDHSession)

	// ListenKeyExchange starts listening for ECDH key exchange messages.
	ListenKeyExchange() error

	// BroadcastPublicKey broadcasts this node's public key to all peers.
	BroadcastPublicKey() error
}

// DynamicECDHSessionImpl implements DynamicECDHSession by wrapping
// the original mpcium ecdhSession with dynamic peer management.
//
// Key insight from ecdh_dynamic_join_verification.md:
// The original ecdhSession processes public keys from ANY peer (not just
// those in its initial peerIDs list) and stores symmetric keys for them.
// Only the completion check uses len(peerIDs). Therefore, we can add
// peers dynamically by:
// 1. Tracking our own dynamic peerIDs list
// 2. Re-broadcasting our public key when a new peer joins
// 3. Using our dynamic list for completion checks
type DynamicECDHSessionImpl struct {
	mu sync.RWMutex

	nodeID        string
	peerIDs       map[string]struct{}
	identityStore identity.Store

	// AICW-FORK (P0-a, §13.4): optional committee scope for ECDH. When non-nil
	// the "expected peers" count is the committee (minus self) rather than the
	// full registry, so a large network avoids an O(N) full-mesh exchange.
	// nil means no scope is active (full-mesh fallback).
	ceremonyPeers map[string]struct{}

	// Wrapped original ecdhSession (does actual X25519 crypto)
	inner mpciumpc.ECDHSession

	onKeyExchangeComplete func()
	onPeerAdded           func(peerID string)
}

// NewDynamicECDHSession creates a new dynamic ECDH session.
// The inner session must be set later via SetInnerSession() after
// the original ecdhSession is created.
func NewDynamicECDHSession(
	nodeID string,
	identityStore identity.Store,
) *DynamicECDHSessionImpl {
	return &DynamicECDHSessionImpl{
		nodeID:        nodeID,
		peerIDs:       make(map[string]struct{}),
		identityStore: identityStore,
	}
}

// SetInnerSession sets the wrapped original ecdhSession.
// This must be called after NewECDHSession() creates the original session.
func (e *DynamicECDHSessionImpl) SetInnerSession(inner mpciumpc.ECDHSession) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.inner = inner
}

// AddPeer adds a new peer to the ECDH session.
// This triggers re-broadcast of this node's public key so the new peer
// can establish a symmetric key with this node.
//
// AICW-FORK: This is the key method for dynamic peer joining.
// When a new peer joins:
// 1. All existing nodes detect the new peer via Consul watch
// 2. Each existing node calls AddPeer() for the new peer
// 3. Each existing node re-broadcasts its public key
// 4. The new peer receives all public keys and establishes symmetric keys
// 5. The new peer also broadcasts its key, completing bidirectional exchange
func (e *DynamicECDHSessionImpl) AddPeer(peerID string) error {
	if peerID == e.nodeID {
		return nil // Skip self
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if _, exists := e.peerIDs[peerID]; exists {
		// AICW-FORK (P0-b, §1.7 item B — rejoin path): the peer is already known
		// but may have restarted with a fresh ephemeral DH key, leaving our
		// stored symmetric key stale. Instead of the previous early return (which
		// left the rejoining node unable to re-establish a key), drop the stale
		// key and re-broadcast so both sides re-derive a fresh shared secret.
		// This is a no-op for a peer that never actually restarted: re-derivation
		// from unchanged keys yields the identical symmetric key.
		if e.identityStore != nil {
			e.identityStore.RemoveSymmetricKey(peerID)
		}
		if e.inner != nil {
			if err := e.inner.BroadcastPublicKey(); err != nil {
				return fmt.Errorf("failed to re-broadcast public key for rejoining peer %s: %w", peerID, err)
			}
		}
		return nil
	}

	// Add to our dynamic peer list
	e.peerIDs[peerID] = struct{}{}

	// Re-broadcast our public key so the new peer can receive it.
	// The original ecdhSession will process the new peer's public key
	// when it arrives (it doesn't filter by peerIDs on receive).
	if e.inner != nil {
		if err := e.inner.BroadcastPublicKey(); err != nil {
			return fmt.Errorf("failed to broadcast public key for new peer %s: %w", peerID, err)
		}
	}

	// Notify callback
	if e.onPeerAdded != nil {
		go e.onPeerAdded(peerID)
	}

	return nil
}

// RemovePeer removes a peer from the ECDH session.
// This removes the symmetric key and delegates to the inner session.
func (e *DynamicECDHSessionImpl) RemovePeer(peerID string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	delete(e.peerIDs, peerID)

	// Delegate to inner session (removes symmetric key)
	if e.inner != nil {
		e.inner.RemovePeer(peerID)
	} else {
		e.identityStore.RemoveSymmetricKey(peerID)
	}
}

// GetExpectedPeerCount returns the number of expected peers.
// AICW-FORK: Uses our dynamic peerIDs list, not the original's fixed list.
// AICW-FORK (P0-a, §13.4): when a ceremony scope is set, the expected count is
// the committee size (minus self); otherwise it falls back to the full dynamic
// peer set (full-mesh).
func (e *DynamicECDHSessionImpl) GetExpectedPeerCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if len(e.ceremonyPeers) > 0 {
		return len(e.ceremonyPeers)
	}
	return len(e.peerIDs)
}

// GetMeshExpectedPeerCount returns the registry-wide mesh size (excluding self).
func (e *DynamicECDHSessionImpl) GetMeshExpectedPeerCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.peerIDs)
}

// GetMeshCompletedKeyCount counts symmetric keys only for known mesh peers.
// Unlike GetCompletedKeyCount (total store size), this ignores stale/extra keys
// and reflects whether each mesh peer has a key.
func (e *DynamicECDHSessionImpl) GetMeshCompletedKeyCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.identityStore == nil {
		return 0
	}
	count := 0
	for peerID := range e.peerIDs {
		if _, err := e.identityStore.GetSymmetricKey(peerID); err == nil {
			count++
		}
	}
	return count
}

// ClearCeremonyScope restores full-mesh expected-peer counting after a ceremony.
func (e *DynamicECDHSessionImpl) ClearCeremonyScope() {
	e.SetCeremonyPeers(nil)
}

// SetCeremonyPeers restricts the ECDH expected-peer scope to a committee.
// Passing nil/empty clears the scope (full-mesh fallback).
// AICW-FORK (P0-a, §13.4).
func (e *DynamicECDHSessionImpl) SetCeremonyPeers(peerIDs []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(peerIDs) == 0 {
		e.ceremonyPeers = nil
		return
	}
	scope := make(map[string]struct{}, len(peerIDs))
	for _, id := range peerIDs {
		if id != e.nodeID {
			scope[id] = struct{}{}
		}
	}
	e.ceremonyPeers = scope
}

// EnsureECDH ensures symmetric keys with every committee member, sets the
// ceremony scope to the committee, and re-broadcasts this node's public key so
// keys are (re)derived. Called just before a keygen/reshare/sign ceremony.
// AICW-FORK (P0-a, §13.4 point 4).
func (e *DynamicECDHSessionImpl) EnsureECDH(committee []string) error {
	e.mu.Lock()
	scope := make(map[string]struct{}, len(committee))
	for _, id := range committee {
		if id == e.nodeID {
			continue
		}
		scope[id] = struct{}{}
		// Ensure the exchange set covers this member so completion counting and
		// the periodic broadcast include it.
		e.peerIDs[id] = struct{}{}
	}
	if len(scope) > 0 {
		e.ceremonyPeers = scope
	}
	inner := e.inner
	e.mu.Unlock()

	if inner != nil {
		if err := inner.BroadcastPublicKey(); err != nil {
			return fmt.Errorf("failed to broadcast public key for ceremony ECDH: %w", err)
		}
	}
	return nil
}

// GetCompletedKeyCount returns the number of completed key exchanges.
// Delegates to the inner session or identity store.
func (e *DynamicECDHSessionImpl) GetCompletedKeyCount() int {
	if e.inner != nil {
		return e.inner.GetReadyPeersCount()
	}
	return e.identityStore.GetSymmetricKeyCount()
}

// IsKeyExchangeComplete checks if all expected keys are established.
// AICW-FORK: Uses our dynamic peer count, not the original's fixed count.
func (e *DynamicECDHSessionImpl) IsKeyExchangeComplete() bool {
	return e.GetMeshCompletedKeyCount() >= e.GetMeshExpectedPeerCount() &&
		e.GetMeshExpectedPeerCount() > 0
}

// OnKeyExchangeComplete sets the callback for when key exchange completes.
func (e *DynamicECDHSessionImpl) OnKeyExchangeComplete(callback func()) {
	e.onKeyExchangeComplete = callback
	// Also set on inner session if available
	if e.inner != nil {
		e.inner.OnKeyExchangeComplete(callback)
	}
}

// OnPeerAdded sets the callback for when a peer is added.
func (e *DynamicECDHSessionImpl) OnPeerAdded(callback func(peerID string)) {
	e.onPeerAdded = callback
}

// GetAllPeerIDs returns all peer IDs.
func (e *DynamicECDHSessionImpl) GetAllPeerIDs() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	ids := make([]string, 0, len(e.peerIDs))
	for id := range e.peerIDs {
		ids = append(ids, id)
	}
	return ids
}

// InitializeWithPeers sets the initial peer list.
// Called during startup to populate from existing peers.
func (e *DynamicECDHSessionImpl) InitializeWithPeers(peerIDs []string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	for _, peerID := range peerIDs {
		if peerID != e.nodeID {
			e.peerIDs[peerID] = struct{}{}
		}
	}
}

// ErrChan returns the error channel from the inner session.
func (e *DynamicECDHSessionImpl) ErrChan() <-chan error {
	if e.inner != nil {
		return e.inner.ErrChan()
	}
	return nil
}

// Close closes the inner session.
func (e *DynamicECDHSessionImpl) Close() error {
	if e.inner != nil {
		return e.inner.Close()
	}
	return nil
}

// ListenKeyExchange starts the key exchange listener on the inner session.
func (e *DynamicECDHSessionImpl) ListenKeyExchange() error {
	if e.inner != nil {
		return e.inner.ListenKeyExchange()
	}
	return fmt.Errorf("inner ECDH session not set")
}

// BroadcastPublicKey broadcasts this node's public key via the inner session.
func (e *DynamicECDHSessionImpl) BroadcastPublicKey() error {
	if e.inner != nil {
		return e.inner.BroadcastPublicKey()
	}
	return fmt.Errorf("inner ECDH session not set")
}
