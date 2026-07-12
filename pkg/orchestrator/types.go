package orchestrator

import "sort"

// KeyKind is the MPC key family; every wallet has both an ed25519 and a
// secp256k1 key that must be resharded together (§4.4).
type KeyKind string

const (
	KeyKindEddsa KeyKind = "eddsa"
	KeyKindEcdsa KeyKind = "ecdsa"
)

// WalletKey mirrors keyinfo.KeyInfo for one key family.
type WalletKey struct {
	Kind      KeyKind
	Committee []string // ParticipantPeerIDs
	Threshold int
	Version   int
}

// Wallet aggregates both key families for a wallet UUID.
type Wallet struct {
	ID   string
	Keys map[KeyKind]WalletKey
}

// OldCommittee returns the canonical current committee for the wallet. The two
// key families share the same committee; the ed25519 entry is authoritative,
// falling back to ecdsa.
func (w Wallet) OldCommittee() []string {
	if k, ok := w.Keys[KeyKindEddsa]; ok && len(k.Committee) > 0 {
		return k.Committee
	}
	if k, ok := w.Keys[KeyKindEcdsa]; ok {
		return k.Committee
	}
	return nil
}

// Snapshot is a point-in-time view of network liveness (§3.1).
type Snapshot struct {
	Ready      map[string]bool // Consul ready/{nodeId}
	Whitelist  map[string]bool // Consul mpc_eligibility/membership_whitelist/{nodeId}
	PingActive map[string]bool // node_web last_ping_at <= threshold (or ready as proxy)
}

// CandidatePool returns C = ping ∧ whitelist ∧ ready (§4.1), sorted.
func (s Snapshot) CandidatePool() []string {
	out := make([]string, 0, len(s.Ready))
	for id := range s.Ready {
		if s.PingActive[id] && s.Whitelist[id] {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// countAlive returns |set ∩ committee|.
func countAlive(committee []string, set map[string]bool) int {
	n := 0
	for _, id := range committee {
		if set[id] {
			n++
		}
	}
	return n
}
