package eligibility

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/hashicorp/consul/api"
)

// ed25519RawPublicKeySize is the byte length of a raw Ed25519 public key.
const ed25519RawPublicKeySize = 32

// normalizePublicKey returns the canonical raw byte form of a public key.
//
// A 32-byte input is already raw and returned as-is. A 64-character ASCII
// hex string is decoded to its 32 raw bytes. Anything else is returned
// unchanged.
//
// AICW-FORK FIX (P0): This ONLY canonicalizes the representation (hex vs raw)
// so that equal keys stored in different encodings compare equal. It never
// skips the comparison and never causes a genuinely different key to match —
// a wrong key decodes to different bytes and is still rejected.
func normalizePublicKey(b []byte) []byte {
	if len(b) == ed25519RawPublicKeySize {
		return b
	}
	if decoded, err := hex.DecodeString(string(b)); err == nil && len(decoded) == ed25519RawPublicKeySize {
		return decoded
	}
	return b
}

// DefaultMembershipWhitelistPrefix is the Consul key prefix for membership whitelist.
const DefaultMembershipWhitelistPrefix = "mpc_eligibility/membership_whitelist/"

// WhitelistMembershipVerifier verifies node membership against a whitelist.
// This is the Phase A implementation for dynamic peer participation.
//
// SECURITY WARNING - SINGLE POINT OF FAILURE:
// The whitelist write permission is currently operator-only.
// Whoever controls the whitelist controls which nodes can join the MPC network.
// This is acceptable for testnet (Phase A) but MUST be addressed before mainnet:
//
// Recommended mitigations for Phase C:
// 1. Multi-sig requirement for whitelist modifications
// 2. Stake-based eligibility (nodes prove eligibility via on-chain staking)
// 3. Decentralized governance for operator key management
//
// Attack vectors in Phase A:
// - If operator credentials are compromised, attacker can add malicious nodes
// - Single operator can censor nodes from participating
// - No economic penalty for misbehavior (unlike staking in Phase C)
type WhitelistMembershipVerifier struct {
	mu sync.RWMutex

	// allowedNodes maps nodeID -> PeerInfo
	allowedNodes map[string]PeerInfo

	// source is "consul" or "file"
	source string

	// consulClient for Consul-based whitelist
	consulClient *api.Client

	// consulPrefix is the key prefix for Consul
	consulPrefix string

	// filePath for file-based whitelist
	filePath string
}

// NewWhitelistMembershipVerifier creates a whitelist-based membership verifier.
// source: "consul" or "file"
// path: Consul prefix or file path
func NewWhitelistMembershipVerifier(source, path string, consulClient *api.Client) (*WhitelistMembershipVerifier, error) {
	if path == "" {
		path = DefaultMembershipWhitelistPrefix
	}

	v := &WhitelistMembershipVerifier{
		allowedNodes: make(map[string]PeerInfo),
		source:       source,
		consulClient: consulClient,
		consulPrefix: path,
		filePath:     path,
	}

	if err := v.Refresh(); err != nil {
		return nil, fmt.Errorf("eligibility: failed to load membership whitelist: %w", err)
	}

	return v, nil
}

// VerifyMembership checks if a node is allowed to join the network.
func (v *WhitelistMembershipVerifier) VerifyMembership(nodeID string, pubKey []byte, metadata map[string]string) error {
	peer, exists := v.lookup(nodeID)
	if !exists {
		// AICW-FORK FIX: the whitelist is loaded once at construction, but peers
		// can be added to Consul at runtime (a node joins after this node
		// started). On a miss, refresh from the source once and retry so a
		// newly-whitelisted node is recognized. This does NOT weaken the check:
		// a node that is genuinely absent from the whitelist stays rejected, and
		// the public-key comparison below is still enforced for nodes that are
		// present.
		if err := v.Refresh(); err != nil {
			return fmt.Errorf("%w: nodeID %s not found in whitelist (refresh failed: %v)", ErrNotInWhitelist, nodeID, err)
		}
		peer, exists = v.lookup(nodeID)
		if !exists {
			return fmt.Errorf("%w: nodeID %s not found in whitelist", ErrNotInWhitelist, nodeID)
		}
	}

	// Verify pubkey matches if provided and stored.
	//
	// AICW-FORK FIX (P0): compare canonical 32-byte raw keys with bytes.Equal.
	// Both sides are normalized to raw form so a representation difference
	// (hex string vs raw bytes) can never cause a false mismatch — while a
	// genuinely different key is still rejected. Verification is NOT skipped:
	// if the whitelist entry carries a public key, the joining node must
	// present the exact same key.
	if len(pubKey) > 0 && len(peer.PublicKey) > 0 {
		if !bytes.Equal(normalizePublicKey(pubKey), normalizePublicKey(peer.PublicKey)) {
			return fmt.Errorf("%w: public key mismatch for nodeID %s", ErrVerificationFailed, nodeID)
		}
	}

	return nil
}

// lookup returns the whitelist entry for nodeID under a read lock.
func (v *WhitelistMembershipVerifier) lookup(nodeID string) (PeerInfo, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	peer, exists := v.allowedNodes[nodeID]
	return peer, exists
}

// Refresh reloads the membership whitelist from the configured source.
func (v *WhitelistMembershipVerifier) Refresh() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	switch v.source {
	case "consul":
		return v.loadFromConsul()
	case "file":
		return v.loadFromFile()
	default:
		return fmt.Errorf("eligibility: unknown whitelist source: %s", v.source)
	}
}

func (v *WhitelistMembershipVerifier) loadFromConsul() error {
	if v.consulClient == nil {
		return fmt.Errorf("eligibility: consul client not configured")
	}

	kv := v.consulClient.KV()
	pairs, _, err := kv.List(v.consulPrefix, nil)
	if err != nil {
		return fmt.Errorf("eligibility: failed to list consul keys: %w", err)
	}

	// membershipEntry mirrors the operator CLI's on-disk JSON format
	// (cmd/operator add-member / MembershipEntry), where public_key is a
	// hex-encoded string. We decode that hex into raw bytes so it matches the
	// node's raw Ed25519 identity key.
	type membershipEntry struct {
		NodeID    string            `json:"node_id"`
		PublicKey string            `json:"public_key"`
		Metadata  map[string]string `json:"metadata,omitempty"`
	}

	newNodes := make(map[string]PeerInfo)
	for _, pair := range pairs {
		nodeID := pair.Key[len(v.consulPrefix):]

		var entry membershipEntry
		if err := json.Unmarshal(pair.Value, &entry); err != nil {
			// AICW-FORK FIX (P0): do NOT fall back to storing the raw JSON
			// bytes as the public key. That old fallback poisoned PublicKey
			// with the full ~133-byte JSON blob, so every VerifyMembership
			// comparison against a node's 32-byte raw key failed.
			// A malformed entry is skipped (fail-closed) so an unverifiable
			// node simply never enters the whitelist and is rejected.
			fmt.Printf("warning: eligibility: skipping malformed whitelist entry for %q: %v\n", nodeID, err)
			continue
		}

		if entry.NodeID != "" {
			nodeID = entry.NodeID
		}

		info := PeerInfo{
			NodeID:   nodeID,
			Metadata: entry.Metadata,
		}

		// AICW-FORK FIX (P0): hex-decode the public key into 32-byte raw form.
		// Invalid hex is fail-closed (entry skipped) rather than stored as a
		// poisoned value.
		if entry.PublicKey != "" {
			raw, decErr := hex.DecodeString(entry.PublicKey)
			if decErr != nil {
				fmt.Printf("warning: eligibility: skipping whitelist entry %q with invalid hex public key: %v\n", nodeID, decErr)
				continue
			}
			info.PublicKey = raw
		}

		newNodes[nodeID] = info
	}

	v.allowedNodes = newNodes
	return nil
}

func (v *WhitelistMembershipVerifier) loadFromFile() error {
	data, err := os.ReadFile(v.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			// Empty whitelist is valid
			v.allowedNodes = make(map[string]PeerInfo)
			return nil
		}
		return fmt.Errorf("eligibility: failed to read whitelist file: %w", err)
	}

	var peers []PeerInfo
	if err := json.Unmarshal(data, &peers); err != nil {
		return fmt.Errorf("eligibility: failed to parse whitelist file: %w", err)
	}

	newNodes := make(map[string]PeerInfo)
	for _, peer := range peers {
		newNodes[peer.NodeID] = peer
	}

	v.allowedNodes = newNodes
	return nil
}

// Name returns the verifier name.
func (v *WhitelistMembershipVerifier) Name() string {
	return "whitelist"
}

// GetAllowedNodes returns a copy of all allowed nodes.
func (v *WhitelistMembershipVerifier) GetAllowedNodes() []PeerInfo {
	v.mu.RLock()
	defer v.mu.RUnlock()

	nodes := make([]PeerInfo, 0, len(v.allowedNodes))
	for _, peer := range v.allowedNodes {
		nodes = append(nodes, peer)
	}
	return nodes
}

// AllowedNodeCount returns the number of allowed nodes.
func (v *WhitelistMembershipVerifier) AllowedNodeCount() int {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return len(v.allowedNodes)
}

// IsNodeAllowed checks if a specific nodeID is in the whitelist.
func (v *WhitelistMembershipVerifier) IsNodeAllowed(nodeID string) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	_, exists := v.allowedNodes[nodeID]
	return exists
}
