package orchestrator

import "testing"

func TestDecideTrigger(t *testing.T) {
	// threshold=2 -> minSign=3; spareTarget=1 (tier 5~10) unless noted.
	cases := []struct {
		name        string
		threshold   int
		spareTarget int
		aPing       int
		aReady      int
		want        Trigger
	}{
		{"healthy with spare", 2, 1, 5, 5, TriggerNone},
		{"healthy exactly spare", 2, 1, 4, 4, TriggerNone}, // aReady 4 >= minSign+spare 4
		{"ready spare exhausted -> proactive", 2, 1, 4, 3, TriggerProactive}, // stop+resign: ping lags, ready=3
		{"signable but no spare (spare0)", 2, 0, 3, 3, TriggerNone},          // minSign+spare=3, aReady 3 not <3
		{"below floor -> urgent", 2, 1, 2, 2, TriggerUrgent},                 // aReady 2<3, >0
		{"one ready -> urgent", 2, 1, 1, 1, TriggerUrgent},
		{"all gone -> unrecoverable", 2, 1, 0, 0, TriggerUnrecoverable},
		{"ping gone but ready ok -> none", 2, 1, 0, 5, TriggerNone},
	}
	for _, c := range cases {
		got := DecideTrigger(c.threshold, c.spareTarget, c.aPing, c.aReady)
		if got != c.want {
			t.Errorf("%s: DecideTrigger(t=%d,spare=%d,aPing=%d,aReady=%d) = %s, want %s",
				c.name, c.threshold, c.spareTarget, c.aPing, c.aReady, got, c.want)
		}
	}
}

func TestConfirmTrackerProactiveNeedsStreak(t *testing.T) {
	ct := newConfirmTracker(2)

	if ct.observe("w1", TriggerProactive) {
		t.Fatal("proactive should not fire on first scan when need=2")
	}
	if !ct.observe("w1", TriggerProactive) {
		t.Fatal("proactive should fire on second consecutive scan")
	}
	// After firing, streak resets.
	if ct.observe("w1", TriggerProactive) {
		t.Fatal("proactive should not fire again immediately after firing")
	}
}

func TestConfirmTrackerRecoveryResets(t *testing.T) {
	ct := newConfirmTracker(2)
	ct.observe("w1", TriggerProactive) // streak 1
	if ct.observe("w1", TriggerNone) {
		t.Fatal("none must not fire")
	}
	// Streak was reset by recovery; needs two more.
	if ct.observe("w1", TriggerProactive) {
		t.Fatal("streak should have reset after recovery")
	}
	if !ct.observe("w1", TriggerProactive) {
		t.Fatal("should fire after two fresh consecutive proactive scans")
	}
}

func TestConfirmTrackerUrgentImmediate(t *testing.T) {
	ct := newConfirmTracker(2)
	if !ct.observe("w1", TriggerUrgent) {
		t.Fatal("urgent must fire immediately")
	}
}

func TestConfirmTrackerMigrationNeedsStreak(t *testing.T) {
	ct := newConfirmTracker(2)
	// Migration is non-urgent: same hysteresis as proactive.
	if ct.observe("w1", TriggerMigration) {
		t.Fatal("migration should not fire on first scan when need=2")
	}
	if !ct.observe("w1", TriggerMigration) {
		t.Fatal("migration should fire on second consecutive scan")
	}
}

func TestTriggerString(t *testing.T) {
	if TriggerMigration.String() != "migration" {
		t.Fatalf("TriggerMigration.String() = %q, want migration", TriggerMigration.String())
	}
}
