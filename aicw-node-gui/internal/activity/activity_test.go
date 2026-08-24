package activity

import "testing"

func TestIngestLogsSigningEvent(t *testing.T) {
	tracker := NewTracker()
	tracker.IngestLogs([]string{
		"[my_node] Creating signing session with wallet abc",
		"[my_node] Creating signing session with wallet abc",
	})

	events := tracker.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "signing_selected" {
		t.Fatalf("unexpected type %q", events[0].Type)
	}
}

func TestIngestRewardsDetectsSolIncrease(t *testing.T) {
	tracker := NewTracker()
	tracker.IngestRewards([]NodeReward{
		{NodeID: "n1", NodeName: "node_a", RewardSol: 0.001},
	})
	tracker.IngestRewards([]NodeReward{
		{NodeID: "n1", NodeName: "node_a", RewardSol: 0.002},
	})

	events := tracker.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 reward event, got %d", len(events))
	}
	if events[0].Type != "reward_sol" {
		t.Fatalf("unexpected type %q", events[0].Type)
	}
}
