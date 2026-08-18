package orchestrator

import (
	"testing"
	"time"
)

func TestDecideInflightSweep(t *testing.T) {
	maxAge := 25 * time.Minute
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)

	t.Run("recent record preserved", func(t *testing.T) {
		rec := &inflightRecord{StartedAt: now.Add(-5 * time.Minute)}
		deadline := rec.StartedAt.Add(maxAge)
		if decideInflightSweep(rec, false, false, now, deadline) {
			t.Fatal("expected preserve for 5m-old record")
		}
	})

	t.Run("old record deleted", func(t *testing.T) {
		rec := &inflightRecord{StartedAt: now.Add(-30 * time.Minute)}
		deadline := rec.StartedAt.Add(maxAge)
		if !decideInflightSweep(rec, false, false, now, deadline) {
			t.Fatal("expected delete for 30m-old record")
		}
	})

	t.Run("old record held preserved", func(t *testing.T) {
		rec := &inflightRecord{StartedAt: now.Add(-30 * time.Minute)}
		deadline := rec.StartedAt.Add(maxAge)
		if decideInflightSweep(rec, false, true, now, deadline) {
			t.Fatal("expected preserve when held")
		}
	})

	t.Run("corrupt record deleted", func(t *testing.T) {
		if !decideInflightSweep(nil, true, false, now, now) {
			t.Fatal("expected delete for corrupt record")
		}
	})
}
