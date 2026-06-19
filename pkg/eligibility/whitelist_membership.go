package eligibility

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/hashicorp/consul/api"
)

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
	v.mu.RLock()
	defer v.mu.RUnlock()

	peer, exists := v.allowedNodes[nodeID]
	if !exists {
		return fmt.Errorf("%w: nodeID %s not found in whitelist", ErrNotInWhitelist, nodeID)
	}

	// Verify pubkey matches if provided and stored
	if len(pubKey) > 0 && len(peer.PublicKey) > 0 {
		if string(pubKey) != string(peer.PublicKey) {
			return fmt.Errorf("%w: public key mismatch for nodeID %s", ErrVerificationFailed, nodeID)
		}
	}

	return nil
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

	newNodes := make(map[string]PeerInfo)
	for _, pair := range pairs {
		var peer PeerInfo
		if err := json.Unmarshal(pair.Value, &peer); err != nil {
			// Attempt simple format: key is nodeID, value is pubkey
			nodeID := pair.Key[len(v.consulPrefix):]
			peer = PeerInfo{
				NodeID:    nodeID,
				PublicKey: pair.Value,
			}
		}

		if peer.NodeID == "" {
			// Extract nodeID from key
			peer.NodeID = pair.Key[len(v.consulPrefix):]
		}

		newNodes[peer.NodeID] = peer
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
