package eligibility

// MembershipVerifier verifies whether a node is authorized to join the MPC network.
// This enables dynamic peer participation without pre-distributed identity files.
//
// Phase A: WhitelistMembershipVerifier (operator-managed whitelist in Consul)
// Phase C: StakeMembershipVerifier (staking-based verification)
//
// SECURITY WARNING: The whitelist write permission is a single point of failure.
// In Phase A, only the operator can add nodes to the whitelist.
// This is acceptable for testnet but must be addressed before mainnet (Phase C).
type MembershipVerifier interface {
	// VerifyMembership checks if a node is authorized to join the network.
	// nodeID: unique identifier for the node
	// pubKey: Ed25519 public key of the node
	// metadata: optional additional data (e.g., staking proof in Phase C)
	// Returns nil if verification passes, error otherwise.
	VerifyMembership(nodeID string, pubKey []byte, metadata map[string]string) error

	// Refresh reloads the membership data from the source.
	// Phase A: Consul watch / Phase C: staking cache refresh
	Refresh() error

	// Name returns the verifier implementation name for logging/config.
	Name() string
}

// MembershipVerifierConfig holds configuration for membership verification.
type MembershipVerifierConfig struct {
	// Mode selects the verification mode: "whitelist", "stake"
	Mode string

	// WhitelistSource is the source for whitelist mode: "consul" or "file"
	WhitelistSource string

	// WhitelistPath is the Consul key prefix or file path for whitelist
	// Default: "mpc_eligibility/whitelist/"
	WhitelistPath string

	// RefreshInterval is how often to refresh the membership data
	RefreshIntervalSeconds int
}

// PeerInfo represents information about a peer node.
type PeerInfo struct {
	NodeID       string            `json:"node_id"`
	PublicKey    []byte            `json:"public_key"`
	RegisteredAt string            `json:"registered_at"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}
