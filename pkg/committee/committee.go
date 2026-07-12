// Package committee implements the deterministic committee-selection policy
// that is the single source of truth (SSOT) for both keygen (Bridge) and
// reshare (orchestrator).
//
// AICW-FORK (auto_reshare_design.md P1, §4.2/§4.3/§13.5/§13.8):
// Every node/coordinator must compute the SAME committee for a given wallet
// from the SAME inputs (walletID, active pool, policy version). Because the
// committee is NOT carried in the keygen/reshare message, SelectCommittee must
// be fully deterministic: given identical (walletID, activePool, policy) it
// always returns the identical committee, independent of input ordering.
package committee

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Tier maps an active-node-count ceiling to a committee size and spare count.
// See auto_reshare_design.md §4.3.
type Tier struct {
	MaxActive     int `mapstructure:"max_active"`
	CommitteeSize int `mapstructure:"committee_size"`
	Spare         int `mapstructure:"spare"`
}

// Policy is the committee-selection policy (SSOT for keygen and reshare).
// See auto_reshare_design.md §13.8.
type Policy struct {
	Version      string `mapstructure:"version"`
	Cap          int    `mapstructure:"cap"`
	MPCThreshold int    `mapstructure:"mpc_threshold"`
	Tiers        []Tier `mapstructure:"tiers"`
}

// Plan is the deterministic result of SelectCommittee.
type Plan struct {
	// NodeIDs is the selected committee, sorted lexicographically for a stable
	// audit representation.
	NodeIDs []string
	// Threshold is the mpc_threshold (t); signing requires t+1.
	Threshold int
	// Spare is the tier spare target used by the proactive reshare trigger.
	Spare int
	// CommitteeSize is len(NodeIDs) = min(active_count, cap, tier_size).
	CommitteeSize int
	// PolicyHash is a stable hash of the policy, recorded in the reshare audit
	// so committee decisions computed under different policies are detectable.
	PolicyHash string
	// PolicyVersion mirrors Policy.Version for convenience.
	PolicyVersion string
}

// Contains reports whether nodeID is a member of the plan's committee.
func (p Plan) Contains(nodeID string) bool {
	for _, id := range p.NodeIDs {
		if id == nodeID {
			return true
		}
	}
	return false
}

// DefaultPolicy returns the policy specified in auto_reshare_design.md §13.8.
// It is used when no committee_policy is configured, so the mechanism has sane
// production defaults out of the box.
func DefaultPolicy() Policy {
	return Policy{
		Version:      "2",
		Cap:          7,
		MPCThreshold: 2,
		Tiers: []Tier{
			{MaxActive: 4, CommitteeSize: 3, Spare: 0},
			{MaxActive: 10, CommitteeSize: 4, Spare: 1},
			{MaxActive: 30, CommitteeSize: 5, Spare: 2},
			{MaxActive: 100, CommitteeSize: 6, Spare: 3},
			{MaxActive: 999999, CommitteeSize: 7, Spare: 4},
		},
	}
}

// Validate checks the policy for internal consistency.
func (p Policy) Validate() error {
	if p.MPCThreshold < 1 {
		return fmt.Errorf("committee policy: mpc_threshold must be >= 1, got %d", p.MPCThreshold)
	}
	if p.Cap < p.MPCThreshold+1 {
		return fmt.Errorf("committee policy: cap %d must be >= threshold+1 (%d)", p.Cap, p.MPCThreshold+1)
	}
	if len(p.Tiers) == 0 {
		return fmt.Errorf("committee policy: at least one tier is required")
	}
	for i, t := range p.Tiers {
		if t.MaxActive < 1 {
			return fmt.Errorf("committee policy: tier[%d].max_active must be >= 1, got %d", i, t.MaxActive)
		}
		if t.CommitteeSize < p.MPCThreshold+1 {
			return fmt.Errorf("committee policy: tier[%d].committee_size %d must be >= threshold+1 (%d)", i, t.CommitteeSize, p.MPCThreshold+1)
		}
		if t.Spare < 0 {
			return fmt.Errorf("committee policy: tier[%d].spare must be >= 0", i)
		}
	}
	return nil
}

// sortedTiers returns the tiers sorted ascending by MaxActive (a copy).
func (p Policy) sortedTiers() []Tier {
	tiers := append([]Tier(nil), p.Tiers...)
	sort.Slice(tiers, func(i, j int) bool { return tiers[i].MaxActive < tiers[j].MaxActive })
	return tiers
}

// TierFor returns the (committee_size, spare) for a given active node count.
// The first tier whose MaxActive >= activeCount wins; if activeCount exceeds
// every tier, the largest tier is used.
func (p Policy) TierFor(activeCount int) (committeeSize, spare int) {
	tiers := p.sortedTiers()
	for _, t := range tiers {
		if activeCount <= t.MaxActive {
			return t.CommitteeSize, t.Spare
		}
	}
	last := tiers[len(tiers)-1]
	return last.CommitteeSize, last.Spare
}

// TargetSize returns the committee size the policy would select for the given
// active-node count: min(active_count, cap, tier_size) — the same "absolute
// rule" applied inside SelectCommittee (§4.3). Used by the orchestrator to
// detect oversized (legacy) committees for migration (§13.6).
func (p Policy) TargetSize(activeCount int) int {
	tierSize, _ := p.TierFor(activeCount)
	return min3(activeCount, p.Cap, tierSize)
}

// Hash returns a stable hash of the policy for the reshare audit log.
func (p Policy) Hash() string {
	var b strings.Builder
	fmt.Fprintf(&b, "v=%s;cap=%d;t=%d;", p.Version, p.Cap, p.MPCThreshold)
	for _, t := range p.sortedTiers() {
		fmt.Fprintf(&b, "[%d,%d,%d]", t.MaxActive, t.CommitteeSize, t.Spare)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// SelectCommittee deterministically selects the committee for a wallet.
//
// Inputs (auto_reshare_design.md §4.2):
//   - walletID:     the wallet the committee is for (mixed into the hash so
//     different wallets get different committees for load spread).
//   - oldCommittee: for reshare, the previous committee (retain preference).
//     Pass nil for keygen.
//   - activePool:   candidate pool C = ping ∧ whitelist ∧ consul_ready. The
//     caller is responsible for producing a consistent snapshot.
//
// Steps:
//  1. Tier: committee_size, spare ← active_count.
//  2. Absolute rule: committee_size = min(active_count, cap, tier_size).
//  3. Retain (reshare only): R = oldCommittee ∩ activePool.
//  4. Fill: F ⊂ activePool\R, chosen by deterministic hash sort until
//     |R ∪ F| == committee_size.
//  5. Validate committee_size >= threshold+1 and <= active_count.
func (p Policy) SelectCommittee(walletID string, oldCommittee, activePool []string) (Plan, error) {
	if err := p.Validate(); err != nil {
		return Plan{}, err
	}
	if walletID == "" {
		return Plan{}, fmt.Errorf("committee: walletID is required")
	}

	pool := uniqueSorted(activePool)
	activeCount := len(pool)
	if activeCount == 0 {
		return Plan{}, fmt.Errorf("committee: empty active pool")
	}

	tierSize, spare := p.TierFor(activeCount)
	size := min3(activeCount, p.Cap, tierSize)

	if size < p.MPCThreshold+1 {
		return Plan{}, fmt.Errorf(
			"committee: cannot form quorum — size %d < threshold+1 %d (active=%d)",
			size, p.MPCThreshold+1, activeCount,
		)
	}

	poolSet := make(map[string]struct{}, len(pool))
	for _, id := range pool {
		poolSet[id] = struct{}{}
	}

	// Step 3 — Retain: keep live old members (dedup, only those still in pool).
	retained := make([]string, 0, len(oldCommittee))
	retainedSet := make(map[string]struct{}, len(oldCommittee))
	for _, id := range oldCommittee {
		if _, inPool := poolSet[id]; !inPool {
			continue
		}
		if _, dup := retainedSet[id]; dup {
			continue
		}
		retained = append(retained, id)
		retainedSet[id] = struct{}{}
	}

	// If more old members survive than the target size, deterministically trim
	// by hash so the choice is reproducible across nodes.
	if len(retained) > size {
		sortByHash(retained, walletID, p.Version)
		retained = retained[:size]
		retainedSet = make(map[string]struct{}, len(retained))
		for _, id := range retained {
			retainedSet[id] = struct{}{}
		}
	}

	// Step 4 — Fill from the remaining pool by deterministic hash order.
	fillCandidates := make([]string, 0, len(pool))
	for _, id := range pool {
		if _, isRetained := retainedSet[id]; !isRetained {
			fillCandidates = append(fillCandidates, id)
		}
	}
	sortByHash(fillCandidates, walletID, p.Version)

	committee := make([]string, 0, size)
	committee = append(committee, retained...)
	for _, id := range fillCandidates {
		if len(committee) >= size {
			break
		}
		committee = append(committee, id)
	}

	if len(committee) != size {
		return Plan{}, fmt.Errorf(
			"committee: insufficient candidates — got %d, want %d (active=%d)",
			len(committee), size, activeCount,
		)
	}

	// Sorted for a stable audit representation (party ordering is derived
	// separately by each node from this canonical set).
	sort.Strings(committee)

	return Plan{
		NodeIDs:       committee,
		Threshold:     p.MPCThreshold,
		Spare:         spare,
		CommitteeSize: size,
		PolicyHash:    p.Hash(),
		PolicyVersion: p.Version,
	}, nil
}

// hashKey is the deterministic sort key: sha256(walletID | nodeID | version).
func hashKey(walletID, nodeID, version string) string {
	sum := sha256.Sum256([]byte(walletID + "|" + nodeID + "|" + version))
	return hex.EncodeToString(sum[:])
}

// sortByHash sorts ids ascending by hashKey; ties broken by nodeID for total
// determinism.
func sortByHash(ids []string, walletID, version string) {
	sort.SliceStable(ids, func(i, j int) bool {
		hi := hashKey(walletID, ids[i], version)
		hj := hashKey(walletID, ids[j], version)
		if hi == hj {
			return ids[i] < ids[j]
		}
		return hi < hj
	})
}

// uniqueSorted returns the unique, lexicographically sorted set of ids. Empty
// strings are dropped. This makes SelectCommittee independent of input order
// and of duplicate entries.
func uniqueSorted(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
