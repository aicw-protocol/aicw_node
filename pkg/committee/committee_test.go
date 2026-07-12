package committee

import (
	"fmt"
	"reflect"
	"sort"
	"testing"
)

func TestDefaultPolicyValidates(t *testing.T) {
	if err := DefaultPolicy().Validate(); err != nil {
		t.Fatalf("default policy should validate: %v", err)
	}
}

func TestTierFor(t *testing.T) {
	p := DefaultPolicy()
	cases := []struct {
		active   int
		wantSize int
		wantSpr  int
	}{
		{3, 3, 0},
		{4, 3, 0},
		{5, 4, 1},
		{10, 4, 1},
		{11, 5, 2},
		{30, 5, 2},
		{31, 6, 3},
		{100, 6, 3},
		{101, 7, 4},
		{1000, 7, 4},
		{100000, 7, 4},
	}
	for _, c := range cases {
		size, spare := p.TierFor(c.active)
		if size != c.wantSize || spare != c.wantSpr {
			t.Errorf("TierFor(%d) = (%d,%d), want (%d,%d)", c.active, size, spare, c.wantSize, c.wantSpr)
		}
	}
}

func TestTargetSize(t *testing.T) {
	p := DefaultPolicy() // cap 7
	cases := []struct {
		active int
		want   int
	}{
		{2, 2},   // active < tier size -> active
		{5, 4},   // tier size wins
		{50, 6},  // tier size wins
		{500, 7}, // capped
	}
	for _, c := range cases {
		if got := p.TargetSize(c.active); got != c.want {
			t.Errorf("TargetSize(%d) = %d, want %d", c.active, got, c.want)
		}
	}
}

// pool builds n synthetic node IDs.
func pool(n int) []string {
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = fmt.Sprintf("node-%02d", i)
	}
	return out
}

func TestCommitteeSizeMinRule(t *testing.T) {
	p := DefaultPolicy()
	cases := []struct {
		active int
		want   int
	}{
		{3, 3},    // tier 3, active 3 -> 3
		{5, 4},    // tier 4, active 5 -> 4
		{20, 5},   // tier 5
		{50, 6},   // tier 6
		{500, 7},  // tier 7, cap 7
		{5000, 7}, // cap dominates
	}
	for _, c := range cases {
		plan, err := p.SelectCommittee("wallet-x", nil, pool(c.active))
		if err != nil {
			t.Fatalf("active=%d: unexpected error: %v", c.active, err)
		}
		if plan.CommitteeSize != c.want || len(plan.NodeIDs) != c.want {
			t.Errorf("active=%d: size %d (len %d), want %d", c.active, plan.CommitteeSize, len(plan.NodeIDs), c.want)
		}
		// committee must be a subset of the pool
		poolSet := map[string]struct{}{}
		for _, id := range pool(c.active) {
			poolSet[id] = struct{}{}
		}
		for _, id := range plan.NodeIDs {
			if _, ok := poolSet[id]; !ok {
				t.Errorf("active=%d: committee member %q not in pool", c.active, id)
			}
		}
	}
}

func TestSelectCommitteeDeterministic(t *testing.T) {
	p := DefaultPolicy()
	base := pool(9)

	first, err := p.SelectCommittee("wallet-abc", nil, base)
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	// Shuffle the pool order and add duplicates; result must be identical.
	shuffled := []string{base[5], base[0], base[8], base[0], base[2], base[7], base[1], base[3], base[6], base[4], base[8]}
	second, err := p.SelectCommittee("wallet-abc", nil, shuffled)
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	if !reflect.DeepEqual(first.NodeIDs, second.NodeIDs) {
		t.Errorf("committee not deterministic under reordering:\n first=%v\nsecond=%v", first.NodeIDs, second.NodeIDs)
	}
}

func TestSelectCommitteePerWalletDiffers(t *testing.T) {
	p := DefaultPolicy()
	base := pool(20) // tier size 5, plenty of room to differ

	a, _ := p.SelectCommittee("wallet-A", nil, base)
	b, _ := p.SelectCommittee("wallet-B", nil, base)

	if reflect.DeepEqual(a.NodeIDs, b.NodeIDs) {
		t.Errorf("expected different committees for different wallets, both = %v", a.NodeIDs)
	}
}

func TestSelectCommitteeSortedOutput(t *testing.T) {
	p := DefaultPolicy()
	plan, err := p.SelectCommittee("wallet-x", nil, pool(12))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !sort.StringsAreSorted(plan.NodeIDs) {
		t.Errorf("committee output must be sorted for audit: %v", plan.NodeIDs)
	}
}

func TestRetainPrefersLiveOldMembers(t *testing.T) {
	p := DefaultPolicy()
	base := pool(10) // tier size 4

	// Old committee: 3 members still alive + 1 that has left the pool.
	old := []string{base[7], base[8], base[9], "node-dead"}

	plan, err := p.SelectCommittee("wallet-reshare", old, base)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if plan.CommitteeSize != 4 {
		t.Fatalf("size = %d, want 4", plan.CommitteeSize)
	}
	// The 3 live old members must be retained.
	for _, id := range []string{base[7], base[8], base[9]} {
		if !plan.Contains(id) {
			t.Errorf("live old member %q was not retained; committee=%v", id, plan.NodeIDs)
		}
	}
	// The dead member must not appear.
	if plan.Contains("node-dead") {
		t.Errorf("dead old member should not be retained; committee=%v", plan.NodeIDs)
	}
}

func TestRetainTrimIsDeterministic(t *testing.T) {
	p := DefaultPolicy()
	base := pool(10) // tier size 4
	// 6 live old members but committee size is only 4 -> must trim to 4 deterministically.
	old := []string{base[0], base[1], base[2], base[3], base[4], base[5]}

	a, err := p.SelectCommittee("wallet-trim", old, base)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// reorder old + pool; must be identical
	old2 := []string{base[5], base[4], base[3], base[2], base[1], base[0]}
	b, err := p.SelectCommittee("wallet-trim", old2, base)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !reflect.DeepEqual(a.NodeIDs, b.NodeIDs) {
		t.Errorf("retain trim not deterministic: a=%v b=%v", a.NodeIDs, b.NodeIDs)
	}
	if a.CommitteeSize != 4 {
		t.Errorf("size = %d, want 4", a.CommitteeSize)
	}
}

func TestQuorumFloorError(t *testing.T) {
	p := DefaultPolicy() // threshold 2 -> need >= 3
	// Only 2 active nodes: tier says size 3 but min(active,...) = 2 < threshold+1.
	_, err := p.SelectCommittee("wallet-x", nil, pool(2))
	if err == nil {
		t.Fatalf("expected quorum-floor error with 2 active nodes")
	}
}

func TestEmptyPoolError(t *testing.T) {
	p := DefaultPolicy()
	if _, err := p.SelectCommittee("wallet-x", nil, nil); err == nil {
		t.Fatalf("expected error for empty active pool")
	}
}

func TestPolicyHashStableAndSensitive(t *testing.T) {
	p := DefaultPolicy()
	h1 := p.Hash()
	// Reordering tiers must not change the hash (sorted internally).
	p2 := DefaultPolicy()
	p2.Tiers = []Tier{p.Tiers[4], p.Tiers[0], p.Tiers[3], p.Tiers[1], p.Tiers[2]}
	if p2.Hash() != h1 {
		t.Errorf("hash should be independent of tier ordering")
	}
	// Changing cap must change the hash.
	p3 := DefaultPolicy()
	p3.Cap = 5
	if p3.Hash() == h1 {
		t.Errorf("hash should change when cap changes")
	}
}

func TestPlanCarriesPolicyMetadata(t *testing.T) {
	p := DefaultPolicy()
	plan, err := p.SelectCommittee("wallet-x", nil, pool(5))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if plan.PolicyVersion != p.Version {
		t.Errorf("plan version = %q, want %q", plan.PolicyVersion, p.Version)
	}
	if plan.PolicyHash != p.Hash() {
		t.Errorf("plan hash mismatch")
	}
	if plan.Threshold != p.MPCThreshold {
		t.Errorf("plan threshold = %d, want %d", plan.Threshold, p.MPCThreshold)
	}
	if plan.Spare != 1 {
		t.Errorf("spare for 5 active = %d, want 1", plan.Spare)
	}
}
