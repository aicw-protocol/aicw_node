// Package identity provides node identity management for AICW MPC network.
//
// AICW-FORK: This file extends the original Mpcium identity.Store interface
// to support dynamic peer public key registration.
package identity

import (
	"github.com/aicw/aicw_node/pkg/eligibility"
	mpciumtypes "github.com/fystack/mpcium/pkg/types"
)

// NodeIdentity represents a node's identity information
type NodeIdentity struct {
	NodeName  string `json:"node_name"`
	NodeID    string `json:"node_id"`
	PublicKey string `json:"public_key"`
	CreatedAt string `json:"created_at"`
}

// Store manages node identities.
// AICW-FORK: This interface is compatible with the original mpcium identity.Store
// (14 methods) plus additional methods for dynamic peer management (5 methods).
type Store interface {
	// === Original mpcium identity.Store methods (14) ===

	// GetPublicKey retrieves a node's public key by its ID
	GetPublicKey(nodeID string) ([]byte, error)

	// VerifyInitiatorMessage verifies the signature of an initiator message
	VerifyInitiatorMessage(msg mpciumtypes.InitiatorMessage) error

	// AuthorizeInitiatorMessage checks authorization for an initiator message
	AuthorizeInitiatorMessage(msg mpciumtypes.InitiatorMessage) error

	// SignMessage signs a TSS message using the node's private key
	SignMessage(msg *mpciumtypes.TssMessage) ([]byte, error)

	// VerifyMessage verifies a TSS message's signature
	VerifyMessage(msg *mpciumtypes.TssMessage) error

	// SignEcdhMessage signs an ECDH key exchange message
	SignEcdhMessage(msg *mpciumtypes.ECDHMessage) ([]byte, error)

	// VerifySignature verifies an ECDH message's signature
	VerifySignature(msg *mpciumtypes.ECDHMessage) error

	// SetSymmetricKey adds or updates a symmetric key for a peer
	SetSymmetricKey(peerID string, key []byte)

	// GetSymmetricKey retrieves a peer's symmetric key
	GetSymmetricKey(peerID string) ([]byte, error)

	// RemoveSymmetricKey removes a peer's symmetric key
	RemoveSymmetricKey(peerID string)

	// GetSymetricKeyCount returns the number of symmetric keys
	// Note: Original mpcium uses this spelling (typo preserved for compatibility)
	GetSymetricKeyCount() int

	// GetSymmetricKeyCount returns the number of symmetric keys (correct spelling)
	// AICW-FORK: Added for readability while maintaining compatibility
	GetSymmetricKeyCount() int

	// CheckSymmetricKeyComplete checks if we have all required symmetric keys
	CheckSymmetricKeyComplete(desired int) bool

	// EncryptMessage encrypts plaintext using peer's symmetric key
	EncryptMessage(plaintext []byte, peerID string) ([]byte, error)

	// DecryptMessage decrypts ciphertext using peer's symmetric key
	DecryptMessage(cipher []byte, peerID string) ([]byte, error)

	// === AICW-FORK: New methods for dynamic peer management (5) ===

	// RegisterPeerPublicKey adds a new peer's public key to the store.
	RegisterPeerPublicKey(nodeID string, publicKey []byte) error

	// UnregisterPeerPublicKey removes a peer's public key from the store.
	UnregisterPeerPublicKey(nodeID string) error

	// GetAllPeerIDs returns all registered peer IDs (excluding self).
	GetAllPeerIDs() []string

	// GetSelfNodeID returns the current node's ID.
	GetSelfNodeID() string

	// GetSelfPublicKey returns the current node's public key.
	GetSelfPublicKey() ([]byte, error)
}

// DynamicStore extends Store with membership verification integration.
type DynamicStore interface {
	Store

	// SetMembershipVerifier sets the verifier used to validate new peers.
	SetMembershipVerifier(verifier eligibility.MembershipVerifier)

	// LoadPeersFromConsul loads peer public keys from Consul.
	LoadPeersFromConsul() error

	// WatchPeerDirectory watches Consul for peer additions/removals.
	WatchPeerDirectory(onPeerChange func(nodeID string, added bool)) error

	// SyncSelfToConsul registers this node's identity in Consul.
	SyncSelfToConsul() error
}
