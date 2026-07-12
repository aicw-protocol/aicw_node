package orchestrator

// Trigger classifies whether (and how urgently) a wallet needs a reshare.
// See auto_reshare_design.md §3.2.
type Trigger int

const (
	// TriggerNone: committee is healthy with spare — monitor only.
	TriggerNone Trigger = iota
	// TriggerProactive: still signable (a_r >= t+1) but the availability spare
	// is exhausted — reshare to restore headroom.
	TriggerProactive
	// TriggerUrgent: ready participants are below the signing floor but some
	// old members are still ready — reshare immediately if possible.
	TriggerUrgent
	// TriggerUnrecoverable: the old committee is fully gone (a_r == 0) — outside
	// the scope of auto-reshare (social recovery / backup policy).
	TriggerUnrecoverable
	// TriggerMigration: the committee is healthy but larger than the current
	// policy tier target (legacy wallet, §13.6). Reshare down to the tier size.
	// Lowest priority; only acted on when orchestrator.migrate_oversized is on.
	TriggerMigration
)

func (t Trigger) String() string {
	switch t {
	case TriggerNone:
		return "none"
	case TriggerProactive:
		return "proactive"
	case TriggerUrgent:
		return "urgent"
	case TriggerUnrecoverable:
		return "unrecoverable"
	case TriggerMigration:
		return "migration"
	default:
		return "unknown"
	}
}

// DecideTrigger implements the trigger matrix of auto_reshare_design.md §3.2.
//
//	minSign = threshold + 1  (mpcium signing/reshare floor)
//	aReady  = |alive_ready ∩ oldCommittee|
//	aPing   = |alive_ping  ∩ oldCommittee|
//
//	aReady == 0                                  -> Unrecoverable
//	0 < aReady < minSign                         -> Urgent
//	aReady >= minSign && aPing < minSign+spare   -> Proactive
//	otherwise                                    -> None
func DecideTrigger(threshold, spareTarget, aPing, aReady int) Trigger {
	minSign := threshold + 1
	switch {
	case aReady == 0:
		return TriggerUnrecoverable
	case aReady < minSign:
		return TriggerUrgent
	case aPing < minSign+spareTarget:
		return TriggerProactive
	default:
		return TriggerNone
	}
}

// confirmTracker implements the hysteresis of §3.3 at wallet granularity: a
// non-None trigger must be observed for ConfirmDeadScans consecutive scans
// before it is acted upon, and a return to None (recovery) resets the streak.
// Urgent triggers act immediately (need == 1) because they are time-critical.
type confirmTracker struct {
	need   int
	streak map[string]int
}

func newConfirmTracker(confirmDeadScans int) *confirmTracker {
	if confirmDeadScans < 1 {
		confirmDeadScans = 1
	}
	return &confirmTracker{need: confirmDeadScans, streak: make(map[string]int)}
}

// observe records the latest trigger for a wallet and returns whether it should
// be acted upon now.
func (c *confirmTracker) observe(walletID string, t Trigger) bool {
	switch t {
	case TriggerNone, TriggerUnrecoverable:
		// Recovery (or out-of-scope): reset the streak.
		delete(c.streak, walletID)
		return false
	case TriggerUrgent:
		// Time-critical: act immediately and clear any streak.
		delete(c.streak, walletID)
		return true
	default: // Proactive, Migration — require the confirm streak (non-urgent).
		c.streak[walletID]++
		if c.streak[walletID] >= c.need {
			delete(c.streak, walletID)
			return true
		}
		return false
	}
}
