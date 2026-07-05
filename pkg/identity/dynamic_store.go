package identity

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/bnb-chain/tss-lib/v2/tss"
	"github.com/hashicorp/consul/api"
	"github.com/spf13/viper"

	"github.com/aicw/aicw_node/pkg/eligibility"
	mpciumenc "github.com/fystack/mpcium/pkg/encryption"
	mpciumtypes "github.com/fystack/mpcium/pkg/types"
)

// ConsulPeerIdentityPrefix is the Consul key prefix for peer identities.
// Format: mpc_node_identity/{nodeID} -> NodeIdentityValue
const ConsulPeerIdentityPrefix = "mpc_node_identity/"

// NodeIdentityValue is the value stored in Consul for each node.
type NodeIdentityValue struct {
	NodeID       string `json:"node_id"`
	NodeName     string `json:"node_name"`
	PublicKey    string `json:"public_key"`    // hex-encoded
	RegisteredAt string `json:"registered_at"` // ISO8601
}

// InitiatorKey holds the public key for verifying initiator messages.
// Supports both Ed25519 and P-256 algorithms.
type InitiatorKey struct {
	Algorithm mpciumtypes.EventInitiatorKeyType
	Ed25519   []byte
	P256      *ecdsa.PublicKey
}

// SignatureAlgorithm represents supported signature algorithms
type SignatureAlgorithm string

const (
	AlgorithmEd25519 SignatureAlgorithm = "ed25519"
	AlgorithmP256    SignatureAlgorithm = "p256"
)

// AuthorizerID is a unique identifier for an authorizer
type AuthorizerID string

// AuthorizerPublicKey represents an authorizer's public key configuration
type AuthorizerPublicKey struct {
	PublicKey string             `json:"public_key" mapstructure:"public_key"`
	Algorithm SignatureAlgorithm `json:"algorithm" mapstructure:"algorithm"`
}

// AuthorizationConfig holds the authorization settings
type AuthorizationConfig struct {
	Enabled              bool                                 `mapstructure:"enabled"`
	RequiredAuthorizers  []AuthorizerID                       `mapstructure:"required_authorizers"`
	AuthorizerPublicKeys map[AuthorizerID]AuthorizerPublicKey `mapstructure:"authorizer_public_keys"`
}

// DynamicFileStore implements DynamicStore for AICW MPC network.
// Unlike the original fileStore, this only loads self-identity at startup.
// Peer public keys are loaded dynamically from Consul.
//
// AICW-FORK: This is a new implementation that replaces the original
// NewFileStore behavior for dynamic peer networks.
type DynamicFileStore struct {
	mu sync.RWMutex

	// Self identity
	selfNodeID   string
	selfNodeName string
	privateKey   []byte
	selfPubKey   []byte

	// Dynamic peer public keys (loaded from Consul)
	peerPublicKeys map[string][]byte

	// Symmetric keys for encrypted communication
	symmetricKeys map[string][]byte

	// Consul client for dynamic peer discovery
	consulClient *api.Client

	// Membership verifier for validating new peers
	membershipVerifier eligibility.MembershipVerifier

	// Watch stop channel
	watchStopCh chan struct{}

	// === Fields for original mpcium compatibility ===

	// initiatorKey holds the trusted initiator's public key for message verification
	initiatorKey *InitiatorKey

	// authConfig holds authorization configuration
	authConfig AuthorizationConfig

	// cachedAuthorizerKeys caches parsed authorizer public keys
	cachedAuthorizerKeys map[AuthorizerID]any // ed25519.PublicKey or *ecdsa.PublicKey
}

// NewDynamicFileStore creates a new dynamic identity store.
// Unlike upstream Mpcium, this only loads the self identity at startup.
// Peer identities are loaded dynamically from Consul.
//
// Parameters:
//   - identityDir: directory containing identity files
//   - nodeName: this node's name
//   - privateKey: the node's Ed25519 private key
//   - consulClient: Consul client for peer discovery (optional, can be nil for testing)
func NewDynamicFileStore(identityDir, nodeName string, privateKey []byte, consulClient *api.Client) (*DynamicFileStore, error) {
	// Load self identity file
	identityPath := fmt.Sprintf("%s/%s_identity.json", identityDir, nodeName)
	data, err := os.ReadFile(identityPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read self identity file: %w", err)
	}

	var selfIdentity NodeIdentity
	if err := json.Unmarshal(data, &selfIdentity); err != nil {
		return nil, fmt.Errorf("failed to parse self identity: %w", err)
	}

	selfPubKey, err := hex.DecodeString(selfIdentity.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decode self public key: %w", err)
	}

	// Load initiator key from config (for VerifyInitiatorMessage)
	initiatorKey, err := loadInitiatorKey()
	if err != nil {
		// Warning only - not all deployments need initiator verification
		fmt.Printf("warning: failed to load initiator key: %v\n", err)
	}

	store := &DynamicFileStore{
		selfNodeID:           selfIdentity.NodeID,
		selfNodeName:         selfIdentity.NodeName,
		privateKey:           privateKey,
		selfPubKey:           selfPubKey,
		peerPublicKeys:       make(map[string][]byte),
		symmetricKeys:        make(map[string][]byte),
		consulClient:         consulClient,
		watchStopCh:          make(chan struct{}),
		initiatorKey:         initiatorKey,
		cachedAuthorizerKeys: make(map[AuthorizerID]any),
	}

	// Load authorization config
	if err := store.loadAuthorizationConfig(); err != nil {
		fmt.Printf("warning: failed to load authorization config: %v\n", err)
	}

	// Optionally load peers from Consul if client is available
	if consulClient != nil {
		if err := store.LoadPeersFromConsul(); err != nil {
			// Log warning but don't fail - peers may not be registered yet
			fmt.Printf("warning: failed to load peers from Consul: %v\n", err)
		}
	}

	return store, nil
}

// loadInitiatorKey loads the trusted initiator public key from config.
func loadInitiatorKey() (*InitiatorKey, error) {
	algorithm := viper.GetString("event_initiator_algorithm")
	if algorithm == "" {
		algorithm = string(mpciumtypes.EventInitiatorKeyTypeEd25519)
	}

	pubKeyHex := viper.GetString("event_initiator_pubkey")
	if pubKeyHex == "" {
		return nil, fmt.Errorf("event_initiator_pubkey not configured")
	}

	switch algorithm {
	case string(mpciumtypes.EventInitiatorKeyTypeEd25519):
		key, err := mpciumenc.ParseEd25519PublicKeyFromHex(pubKeyHex)
		if err != nil {
			return nil, fmt.Errorf("failed to parse Ed25519 initiator key: %w", err)
		}
		return &InitiatorKey{
			Algorithm: mpciumtypes.EventInitiatorKeyTypeEd25519,
			Ed25519:   key,
		}, nil

	case string(mpciumtypes.EventInitiatorKeyTypeP256):
		key, err := mpciumenc.ParseP256PublicKeyFromHex(pubKeyHex)
		if err != nil {
			// Try base64 as fallback
			key, err = mpciumenc.ParseP256PublicKeyFromBase64(pubKeyHex)
			if err != nil {
				return nil, fmt.Errorf("failed to parse P256 initiator key: %w", err)
			}
		}
		return &InitiatorKey{
			Algorithm: mpciumtypes.EventInitiatorKeyTypeP256,
			P256:      key,
		}, nil

	default:
		return nil, fmt.Errorf("unsupported algorithm: %s", algorithm)
	}
}

// loadAuthorizationConfig loads the authorization settings from config.
func (s *DynamicFileStore) loadAuthorizationConfig() error {
	var authConfig AuthorizationConfig
	if err := viper.UnmarshalKey("authorization", &authConfig); err != nil {
		return fmt.Errorf("failed to unmarshal authorization config: %w", err)
	}
	s.authConfig = authConfig

	if !authConfig.Enabled {
		return nil
	}

	// Cache parsed authorizer public keys
	for id, keyConfig := range authConfig.AuthorizerPublicKeys {
		switch keyConfig.Algorithm {
		case AlgorithmEd25519:
			pubKey, err := mpciumenc.ParseEd25519PublicKeyFromHex(keyConfig.PublicKey)
			if err != nil {
				return fmt.Errorf("invalid ed25519 key for authorizer %s: %w", id, err)
			}
			s.cachedAuthorizerKeys[id] = ed25519.PublicKey(pubKey)

		case AlgorithmP256:
			pubKey, err := mpciumenc.ParseP256PublicKeyFromHex(keyConfig.PublicKey)
			if err != nil {
				return fmt.Errorf("invalid P256 key for authorizer %s: %w", id, err)
			}
			s.cachedAuthorizerKeys[id] = pubKey

		default:
			return fmt.Errorf("unknown algorithm %s for authorizer %s", keyConfig.Algorithm, id)
		}
	}

	return nil
}

// GetPublicKey retrieves a node's public key by its ID.
// Checks self first, then peers.
func (s *DynamicFileStore) GetPublicKey(nodeID string) ([]byte, error) {
	// Check if it's self
	if nodeID == s.selfNodeID {
		return s.selfPubKey, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if key, exists := s.peerPublicKeys[nodeID]; exists {
		return key, nil
	}

	return nil, fmt.Errorf("public key not found for node ID: %s", nodeID)
}

// RegisterPeerPublicKey adds a new peer's public key to the store.
// If a membership verifier is set, the peer must pass verification first.
func (s *DynamicFileStore) RegisterPeerPublicKey(nodeID string, publicKey []byte) error {
	if nodeID == s.selfNodeID {
		return fmt.Errorf("cannot register self as peer")
	}

	// Verify membership if verifier is set
	if s.membershipVerifier != nil {
		if err := s.membershipVerifier.VerifyMembership(nodeID, publicKey, nil); err != nil {
			return fmt.Errorf("peer membership verification failed: %w", err)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.peerPublicKeys[nodeID] = publicKey
	return nil
}

// UnregisterPeerPublicKey removes a peer's public key from the store.
func (s *DynamicFileStore) UnregisterPeerPublicKey(nodeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.peerPublicKeys[nodeID]; !exists {
		return fmt.Errorf("peer not found: %s", nodeID)
	}

	delete(s.peerPublicKeys, nodeID)
	delete(s.symmetricKeys, nodeID) // Also remove symmetric key
	return nil
}

// GetAllPeerIDs returns all registered peer IDs (excluding self).
func (s *DynamicFileStore) GetAllPeerIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := make([]string, 0, len(s.peerPublicKeys))
	for id := range s.peerPublicKeys {
		ids = append(ids, id)
	}
	return ids
}

// GetSelfNodeID returns the current node's ID.
func (s *DynamicFileStore) GetSelfNodeID() string {
	return s.selfNodeID
}

// GetSelfPublicKey returns the current node's public key.
func (s *DynamicFileStore) GetSelfPublicKey() ([]byte, error) {
	return s.selfPubKey, nil
}

// SetSymmetricKey adds or updates a symmetric key for a peer.
func (s *DynamicFileStore) SetSymmetricKey(peerID string, key []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.symmetricKeys[peerID] = key
}

// GetSymmetricKey retrieves a peer's symmetric key.
func (s *DynamicFileStore) GetSymmetricKey(peerID string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if key, exists := s.symmetricKeys[peerID]; exists {
		return key, nil
	}

	return nil, fmt.Errorf("symmetric key not found for peer: %s", peerID)
}

// RemoveSymmetricKey removes a peer's symmetric key.
func (s *DynamicFileStore) RemoveSymmetricKey(peerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.symmetricKeys, peerID)
}

// GetSymmetricKeyCount returns the number of symmetric keys.
func (s *DynamicFileStore) GetSymmetricKeyCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.symmetricKeys)
}

// CheckSymmetricKeyComplete checks if we have all required symmetric keys.
func (s *DynamicFileStore) CheckSymmetricKeyComplete(desired int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.symmetricKeys) == desired
}

// SetMembershipVerifier sets the verifier used to validate new peers.
func (s *DynamicFileStore) SetMembershipVerifier(verifier eligibility.MembershipVerifier) {
	s.membershipVerifier = verifier
}

// LoadPeersFromConsul loads peer public keys from Consul.
func (s *DynamicFileStore) LoadPeersFromConsul() error {
	if s.consulClient == nil {
		return fmt.Errorf("consul client not configured")
	}

	kv := s.consulClient.KV()
	pairs, _, err := kv.List(ConsulPeerIdentityPrefix, nil)
	if err != nil {
		return fmt.Errorf("failed to list peer identities: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, pair := range pairs {
		var identity NodeIdentityValue
		if err := json.Unmarshal(pair.Value, &identity); err != nil {
			// Try simple format: key = nodeID, value = hex pubkey
			nodeID := pair.Key[len(ConsulPeerIdentityPrefix):]
			if nodeID == s.selfNodeID {
				continue // Skip self
			}
			pubKey, err := hex.DecodeString(string(pair.Value))
			if err != nil {
				fmt.Printf("warning: failed to parse peer identity %s: %v\n", pair.Key, err)
				continue
			}
			s.peerPublicKeys[nodeID] = pubKey
			continue
		}

		if identity.NodeID == s.selfNodeID {
			continue // Skip self
		}

		pubKey, err := hex.DecodeString(identity.PublicKey)
		if err != nil {
			fmt.Printf("warning: invalid public key for peer %s: %v\n", identity.NodeID, err)
			continue
		}

		s.peerPublicKeys[identity.NodeID] = pubKey
	}

	return nil
}

// WatchPeerDirectory watches Consul for peer additions/removals.
func (s *DynamicFileStore) WatchPeerDirectory(onPeerChange func(nodeID string, added bool)) error {
	if s.consulClient == nil {
		return fmt.Errorf("consul client not configured")
	}

	go func() {
		var lastIndex uint64 = 0

		for {
			select {
			case <-s.watchStopCh:
				return
			default:
			}

			kv := s.consulClient.KV()
			pairs, meta, err := kv.List(ConsulPeerIdentityPrefix, &api.QueryOptions{
				WaitIndex: lastIndex,
				WaitTime:  60 * 1000000000, // 60 seconds
			})
			if err != nil {
				fmt.Printf("warning: consul watch error: %v\n", err)
				continue
			}

			if meta.LastIndex == lastIndex {
				continue // No changes
			}
			lastIndex = meta.LastIndex

			// Build new peer set
			newPeers := make(map[string][]byte)
			for _, pair := range pairs {
				var identity NodeIdentityValue
				if err := json.Unmarshal(pair.Value, &identity); err != nil {
					continue
				}
				if identity.NodeID == s.selfNodeID {
					continue
				}
				pubKey, _ := hex.DecodeString(identity.PublicKey)
				newPeers[identity.NodeID] = pubKey
			}

			// Detect additions and removals while holding the store lock, but
			// invoke the callbacks AFTER releasing it.
			//
			// AICW-FORK FIX: the onPeerChange callback (registry.AddPeer) calls
			// back into this store (GetPublicKey / RegisterPeerPublicKey), which
			// take s.mu. sync.RWMutex is not reentrant, so calling the callback
			// while holding s.mu.Lock() deadlocked the watch goroutine the first
			// time a peer joined after startup — the node that started first
			// never discovered later peers. Mutating the map under the lock and
			// firing callbacks outside it fixes the dynamic-join path.
			var added, removed []string
			s.mu.Lock()
			for nodeID := range newPeers {
				if _, exists := s.peerPublicKeys[nodeID]; !exists {
					s.peerPublicKeys[nodeID] = newPeers[nodeID]
					added = append(added, nodeID)
				}
			}
			for nodeID := range s.peerPublicKeys {
				if _, exists := newPeers[nodeID]; !exists {
					delete(s.peerPublicKeys, nodeID)
					delete(s.symmetricKeys, nodeID)
					removed = append(removed, nodeID)
				}
			}
			s.mu.Unlock()

			if onPeerChange != nil {
				for _, nodeID := range added {
					onPeerChange(nodeID, true)
				}
				for _, nodeID := range removed {
					onPeerChange(nodeID, false)
				}
			}
		}
	}()

	return nil
}

// SyncSelfToConsul registers this node's identity in Consul.
func (s *DynamicFileStore) SyncSelfToConsul() error {
	if s.consulClient == nil {
		return fmt.Errorf("consul client not configured")
	}

	identity := NodeIdentityValue{
		NodeID:       s.selfNodeID,
		NodeName:     s.selfNodeName,
		PublicKey:    hex.EncodeToString(s.selfPubKey),
		RegisteredAt: "", // Will be set by the caller
	}

	data, err := json.Marshal(identity)
	if err != nil {
		return fmt.Errorf("failed to marshal self identity: %w", err)
	}

	key := ConsulPeerIdentityPrefix + s.selfNodeID
	_, err = s.consulClient.KV().Put(&api.KVPair{
		Key:   key,
		Value: data,
	}, nil)

	if err != nil {
		return fmt.Errorf("failed to sync self to Consul: %w", err)
	}

	return nil
}

// StopWatch stops the Consul watch goroutine.
func (s *DynamicFileStore) StopWatch() {
	close(s.watchStopCh)
}

// SignRawMessage signs raw bytes using the node's private key.
// This is a helper method for simple signing without TssMessage wrapper.
func (s *DynamicFileStore) SignRawMessage(data []byte) ([]byte, error) {
	if len(s.privateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid private key size")
	}
	return ed25519.Sign(s.privateKey, data), nil
}

// VerifyPeerSignature verifies a signature from a peer.
func (s *DynamicFileStore) VerifyPeerSignature(nodeID string, data, signature []byte) error {
	pubKey, err := s.GetPublicKey(nodeID)
	if err != nil {
		return err
	}

	if len(pubKey) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid public key size for node %s", nodeID)
	}

	if !ed25519.Verify(pubKey, data, signature) {
		return fmt.Errorf("invalid signature from node %s", nodeID)
	}

	return nil
}

// === Original mpcium identity.Store methods (ported from fileStore) ===

// SignMessage signs a TSS message using the node's private key.
// Ported from mpcium/pkg/identity/identity.go:463-472
func (s *DynamicFileStore) SignMessage(msg *mpciumtypes.TssMessage) ([]byte, error) {
	msgBytes, err := msg.MarshalForSigning()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal message for signing: %w", err)
	}
	signature := ed25519.Sign(s.privateKey, msgBytes)
	return signature, nil
}

// VerifyMessage verifies a TSS message's signature using the sender's public key.
// Ported from mpcium/pkg/identity/identity.go:474-501
func (s *DynamicFileStore) VerifyMessage(msg *mpciumtypes.TssMessage) error {
	if msg.Signature == nil {
		return fmt.Errorf("message has no signature")
	}

	senderNodeID := partyIDToNodeID(msg.From)
	publicKey, err := s.GetPublicKey(senderNodeID)
	if err != nil {
		return fmt.Errorf("failed to get sender's public key: %w", err)
	}

	msgBytes, err := msg.MarshalForSigning()
	if err != nil {
		return fmt.Errorf("failed to marshal message for verification: %w", err)
	}

	if !ed25519.Verify(publicKey, msgBytes, msg.Signature) {
		return fmt.Errorf("invalid signature")
	}

	return nil
}

// SignEcdhMessage signs an ECDH key exchange message.
// Ported from mpcium/pkg/identity/identity.go:529-539
func (s *DynamicFileStore) SignEcdhMessage(msg *mpciumtypes.ECDHMessage) ([]byte, error) {
	msgBytes, err := msg.MarshalForSigning()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal message for signing: %w", err)
	}
	signature := ed25519.Sign(s.privateKey, msgBytes)
	return signature, nil
}

// VerifySignature verifies an ECDH message's signature.
// Ported from mpcium/pkg/identity/identity.go:541-565
func (s *DynamicFileStore) VerifySignature(msg *mpciumtypes.ECDHMessage) error {
	if msg.Signature == nil {
		return fmt.Errorf("ECDH message has no signature")
	}

	senderPk, err := s.GetPublicKey(msg.From)
	if err != nil {
		return fmt.Errorf("failed to get sender's public key: %w", err)
	}

	msgBytes, err := msg.MarshalForSigning()
	if err != nil {
		return fmt.Errorf("failed to marshal message for verification: %w", err)
	}

	if !ed25519.Verify(senderPk, msgBytes, msg.Signature) {
		return fmt.Errorf("invalid signature from %s with public key %s", msg.From, hex.EncodeToString(senderPk))
	}

	return nil
}

// EncryptMessage encrypts plaintext using peer's symmetric key.
// Ported from mpcium/pkg/identity/identity.go:503-514
func (s *DynamicFileStore) EncryptMessage(plaintext []byte, peerID string) ([]byte, error) {
	key, err := s.GetSymmetricKey(peerID)
	if err != nil {
		return nil, err
	}
	if key == nil {
		return nil, fmt.Errorf("no symmetric key for peer %s", peerID)
	}
	return mpciumenc.EncryptAESGCMWithNonceEmbed(plaintext, key)
}

// DecryptMessage decrypts ciphertext using peer's symmetric key.
// Ported from mpcium/pkg/identity/identity.go:516-527
func (s *DynamicFileStore) DecryptMessage(cipher []byte, peerID string) ([]byte, error) {
	key, err := s.GetSymmetricKey(peerID)
	if err != nil {
		return nil, err
	}
	if key == nil {
		return nil, fmt.Errorf("no symmetric key for peer %s", peerID)
	}
	return mpciumenc.DecryptAESGCMWithNonceEmbed(cipher, key)
}

// VerifyInitiatorMessage verifies the signature of an initiator message.
// Ported from mpcium/pkg/identity/identity.go:567-577
func (s *DynamicFileStore) VerifyInitiatorMessage(msg mpciumtypes.InitiatorMessage) error {
	if s.initiatorKey == nil {
		return fmt.Errorf("initiator key not configured")
	}

	switch s.initiatorKey.Algorithm {
	case mpciumtypes.EventInitiatorKeyTypeEd25519:
		return s.verifyInitiatorEd25519(msg)
	case mpciumtypes.EventInitiatorKeyTypeP256:
		return s.verifyInitiatorP256(msg)
	}
	return fmt.Errorf("unsupported algorithm: %s", s.initiatorKey.Algorithm)
}

func (s *DynamicFileStore) verifyInitiatorEd25519(msg mpciumtypes.InitiatorMessage) error {
	msgBytes, err := msg.Raw()
	if err != nil {
		return fmt.Errorf("failed to get raw message data: %w", err)
	}
	signature := msg.Sig()
	if len(signature) == 0 {
		return errors.New("signature is empty")
	}

	if !ed25519.Verify(s.initiatorKey.Ed25519, msgBytes, signature) {
		return fmt.Errorf("invalid signature from initiator")
	}
	return nil
}

func (s *DynamicFileStore) verifyInitiatorP256(msg mpciumtypes.InitiatorMessage) error {
	msgBytes, err := msg.Raw()
	if err != nil {
		return fmt.Errorf("failed to get raw message data: %w", err)
	}
	signature := msg.Sig()

	if s.initiatorKey.P256 == nil {
		return fmt.Errorf("initiator public key for P256 is not set")
	}

	return mpciumenc.VerifyP256Signature(s.initiatorKey.P256, msgBytes, signature)
}

// AuthorizeInitiatorMessage checks authorization for an initiator message.
// Ported from mpcium/pkg/identity/identity.go:579-618
func (s *DynamicFileStore) AuthorizeInitiatorMessage(msg mpciumtypes.InitiatorMessage) error {
	if !s.authConfig.Enabled {
		return nil
	}

	sigs := msg.GetAuthorizerSignatures()
	if len(s.authConfig.RequiredAuthorizers) > 0 {
		providedSigs := make(map[AuthorizerID]mpciumtypes.AuthorizerSignature)
		for _, sig := range sigs {
			providedSigs[AuthorizerID(sig.AuthorizerID)] = sig
		}

		for _, requiredID := range s.authConfig.RequiredAuthorizers {
			if _, ok := providedSigs[requiredID]; !ok {
				return fmt.Errorf("missing required authorizer signature: %s", requiredID)
			}
		}
	}

	if len(sigs) == 0 {
		return nil
	}

	authorizerRaw, err := mpciumtypes.ComposeAuthorizerRaw(msg)
	if err != nil {
		return fmt.Errorf("failed to compose authorizer raw: %w", err)
	}

	for _, sig := range sigs {
		if err := s.verifyAuthorizerSignature(authorizerRaw, sig); err != nil {
			return fmt.Errorf("authorizer %s verification failed: %w", sig.AuthorizerID, err)
		}
	}

	return nil
}

func (s *DynamicFileStore) verifyAuthorizerSignature(raw []byte, sig mpciumtypes.AuthorizerSignature) error {
	authPub, ok := s.cachedAuthorizerKeys[AuthorizerID(sig.AuthorizerID)]
	if !ok {
		return fmt.Errorf("authorizer %s not found in cache", sig.AuthorizerID)
	}

	keyMeta := s.authConfig.AuthorizerPublicKeys[AuthorizerID(sig.AuthorizerID)]
	switch keyMeta.Algorithm {
	case AlgorithmEd25519:
		pub := authPub.(ed25519.PublicKey)
		if !ed25519.Verify(pub, raw, sig.Signature) {
			return fmt.Errorf("ed25519 verification failed for %s", sig.AuthorizerID)
		}

	case AlgorithmP256:
		pub := authPub.(*ecdsa.PublicKey)
		if err := mpciumenc.VerifyP256Signature(pub, raw, sig.Signature); err != nil {
			return fmt.Errorf("p256 verification failed for %s: %w", sig.AuthorizerID, err)
		}

	default:
		return fmt.Errorf("unsupported algorithm %q for authorizer %s", keyMeta.Algorithm, sig.AuthorizerID)
	}

	return nil
}

// GetSymetricKeyCount returns the number of symmetric keys.
// Note: This method preserves the original mpcium typo for interface compatibility.
func (s *DynamicFileStore) GetSymetricKeyCount() int {
	return s.GetSymmetricKeyCount()
}

// partyIDToNodeID extracts node ID from a tss.PartyID.
// Ported from mpcium/pkg/identity/identity.go:685-687
func partyIDToNodeID(partyID *tss.PartyID) string {
	return strings.Split(string(partyID.KeyInt().Bytes()), ":")[0]
}
