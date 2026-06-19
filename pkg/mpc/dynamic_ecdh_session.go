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

	// GetExpectedPeerCount returns the number of expected peers.
	GetExpectedPeerCount() int

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
		return nil // Already added
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
func (e *DynamicECDHSessionImpl) GetExpectedPeerCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.peerIDs)
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
	return e.identityStore.CheckSymmetricKeyComplete(e.GetExpectedPeerCount())
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
