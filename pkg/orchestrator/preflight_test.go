package orchestrator

import "testing"

func TestReachableCommittee(t *testing.T) {
	old := []string{"gone", "live-a", "live-b"}
	whitelist := map[string]bool{"live-a": true, "live-b": true}

	got := reachableCommittee(old, whitelist)
	if len(got) != 2 || got[0] != "live-a" || got[1] != "live-b" {
		t.Fatalf("reachableCommittee() = %v, want [live-a live-b]", got)
	}
}

func TestPreflightReadyIgnoresOffboardedOldMembers(t *testing.T) {
	old := []string{"departed", "a", "b", "c"}
	snap := Snapshot{
		Ready: map[string]bool{
			"a": true,
			"b": true,
			"c": true,
		},
		Whitelist: map[string]bool{
			"a": true,
			"b": true,
			"c": true,
		},
	}
	newCommittee := []string{"a", "b", "c", "d"}

	// departed not ready/whitelisted; 3 reachable old members ready >= threshold+1 (3).
	if !preflightReady(old, newCommittee, snap, 2) {
		t.Fatal("preflightReady should pass when offboarded peer is excluded from old quorum")
	}

	snap.Ready["d"] = true
	if !preflightReady(old, newCommittee, snap, 2) {
		t.Fatal("preflightReady should pass when new committee is ready")
	}

	snap.Ready["c"] = false
	if preflightReady(old, newCommittee, snap, 2) {
		t.Fatal("preflightReady should fail when a new committee member is not ready")
	}
}

func TestPreflightReadyFailsWhenReachableOldBelowFloor(t *testing.T) {
	old := []string{"departed", "a", "b"}
	snap := Snapshot{
		Ready:     map[string]bool{"a": true, "b": true},
		Whitelist: map[string]bool{"a": true, "b": true},
	}
	newCommittee := []string{"a", "b", "c"}
	snap.Ready["c"] = true

	if preflightReady(old, newCommittee, snap, 2) {
		t.Fatal("preflightReady should fail when only 2 reachable old members ready but floor is 3")
	}
}
