package orchestrator

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/fystack/mpcium/pkg/logger"

	"github.com/aicw/aicw_node/pkg/committee"
)

// Orchestrator wires the reconcile loop (auto_reshare_design.md §2, §6.4).
type Orchestrator struct {
	cfg          Config
	inventory    *Inventory
	liveness     *LivenessProvider
	lock         *LockManager
	auditor      *Auditor
	publisher    *Publisher
	progress     *ProgressStore
	verifier     *Verifier
	confirm      *confirmTracker
	initiatorPub string

	active int64 // atomic gauge of in-flight reshares (global_max_inflight, §6.2)
}

// New builds an orchestrator from its dependencies.
func New(
	cfg Config,
	inv *Inventory,
	liveness *LivenessProvider,
	lock *LockManager,
	auditor *Auditor,
	publisher *Publisher,
	progress *ProgressStore,
	verifier *Verifier,
	initiatorPub string,
) *Orchestrator {
	return &Orchestrator{
		cfg:          cfg,
		inventory:    inv,
		liveness:     liveness,
		lock:         lock,
		auditor:      auditor,
		publisher:    publisher,
		progress:     progress,
		verifier:     verifier,
		confirm:      newConfirmTracker(cfg.ConfirmDeadScans),
		initiatorPub: initiatorPub,
	}
}

// Run executes the reconcile loop until ctx is cancelled (§6.4).
func (o *Orchestrator) Run(ctx context.Context) {
	logger.Info("Reshare orchestrator started",
		"scan_interval", o.cfg.ScanInterval.String(),
		"global_max_inflight", o.cfg.GlobalMaxInflight,
		"policy_version", o.cfg.Policy.Version,
	)

	ticker := time.NewTicker(o.cfg.ScanInterval)
	defer ticker.Stop()

	o.reconcileOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			logger.Info("Reshare orchestrator stopped")
			return
		case <-ticker.C:
			o.reconcileOnce(ctx)
		}
	}
}

// reconcileOnce performs a single monitor→trigger→pre-flight→publish pass.
func (o *Orchestrator) reconcileOnce(ctx context.Context) {
	wallets, err := o.inventory.List()
	if err != nil {
		logger.Error("Reconcile: inventory list failed", err)
		return
	}
	snap, err := o.liveness.Snapshot(ctx)
	if err != nil {
		logger.Error("Reconcile: liveness snapshot failed", err)
		return
	}

	pool := snap.CandidatePool()
	threshold := o.cfg.Policy.MPCThreshold
	_, spareTarget := o.cfg.Policy.TierFor(len(pool))

	for _, w := range wallets {
		if atomic.LoadInt64(&o.active) >= int64(o.cfg.GlobalMaxInflight) {
			return // saturated for this scan
		}

		old := w.OldCommittee()
		if len(old) == 0 {
			continue
		}

		// §5E: never auto-reshare a wallet flagged frozen (unexpected pubkey change).
		if frozen, ferr := o.verifier.IsFrozen(w.ID); ferr != nil {
			logger.Error("Reconcile: freeze check failed", ferr, "walletID", w.ID)
			continue
		} else if frozen {
			logger.Warn("Reconcile: wallet is frozen; skipping (operator intervention required)", "walletID", w.ID)
			continue
		}

		aReady := countAlive(old, snap.Ready)
		aPing := countAlive(old, snap.PingActive)
		trig := DecideTrigger(threshold, spareTarget, aPing, aReady)

		// §13.6 legacy migration: a healthy committee that is larger than the
		// current policy target is proactively shrunk to the tier size. Only when
		// enabled, and only if the whole old committee is ready (a plain resize,
		// not a recovery) to avoid churn. Still rate-limited by cooldown/inflight.
		if trig == TriggerNone && o.cfg.MigrateOversized {
			targetSize := o.cfg.Policy.TargetSize(len(pool))
			if len(old) > targetSize && aReady == len(old) {
				trig = TriggerMigration
			}
		}

		if trig == TriggerUnrecoverable {
			logger.Warn("Wallet committee unrecoverable (a_r==0); auto-reshare cannot help",
				"walletID", w.ID, "old_committee", old)
			continue
		}
		if !o.confirm.observe(w.ID, trig) {
			continue
		}

		if ok, err := o.lock.CooldownOK(w.ID); err != nil {
			logger.Error("Reconcile: cooldown check failed", err, "walletID", w.ID)
			continue
		} else if !ok {
			continue
		}

		// Committee plan (§4).
		plan, err := o.cfg.Policy.SelectCommittee(w.ID, old, pool)
		if err != nil {
			logger.Warn("Reconcile: committee selection failed", "walletID", w.ID, "error", err.Error())
			continue
		}

		// Pre-flight ready check (§3.4). Publishing when the old committee lacks a
		// signing quorum, or a new member is not ready, would only fail at the node.
		if !preflightReady(old, plan.NodeIDs, snap, threshold) {
			logger.Warn("Reconcile: pre-flight not ready; deferring reshare (MPC not ready)",
				"walletID", w.ID, "trigger", trig.String(),
				"old_ready", aReady, "new_committee", plan.NodeIDs)
			continue
		}

		sessionID := uuid.New().String()
		acquired, err := o.lock.Acquire(w.ID, sessionID, []string{string(KeyKindEddsa), string(KeyKindEcdsa)})
		if err != nil {
			logger.Error("Reconcile: lock acquire failed", err, "walletID", w.ID)
			continue
		}
		if !acquired {
			continue // another attempt already in-flight
		}

		atomic.AddInt64(&o.active, 1)
		go o.runReshare(ctx, w, old, plan, snap, sessionID, trig)
	}
}

// preflightReady enforces auto_reshare_design.md §3.4 / §3.1 step 3.
func preflightReady(old, newCommittee []string, snap Snapshot, threshold int) bool {
	if countAlive(old, snap.Ready) < threshold+1 {
		return false
	}
	for _, id := range newCommittee {
		if !snap.Ready[id] {
			return false
		}
	}
	return true
}

func (o *Orchestrator) runReshare(ctx context.Context, w Wallet, old []string, plan committee.Plan, snap Snapshot, sessionID string, trig Trigger) {
	defer atomic.AddInt64(&o.active, -1)
	defer func() {
		if err := o.lock.Release(w.ID); err != nil {
			logger.Error("Reshare: lock release failed", err, "walletID", w.ID)
		}
	}()

	// Resume from any partial progress: after "EdDSA OK / ECDSA FAIL" only the
	// remaining key families are resharded (§7.1).
	only, perr := o.progress.Pending(w.ID)
	if perr != nil {
		logger.Warn("Reshare: pending progress read failed; resharing all key types",
			"walletID", w.ID, "error", perr.Error())
		only = nil
	}
	attempted := AttemptedKeyTypes(only)

	rec := AuditRecord{
		SessionID:     sessionID,
		WalletID:      w.ID,
		Trigger:       trig.String(),
		OldCommittee:  old,
		NewCommittee:  plan.NodeIDs,
		AlivePing:     aliveList(old, snap.PingActive),
		AliveReady:    aliveList(old, snap.Ready),
		PolicyVersion: plan.PolicyVersion,
		PolicyHash:    plan.PolicyHash,
		InitiatorPub:  o.initiatorPub,
		NewThreshold:  plan.Threshold,
		Result:        "published",
		PublishedAt:   time.Now().UTC(),
	}
	if err := o.auditor.Write(rec); err != nil {
		logger.Error("Reshare: initial audit write failed", err, "walletID", w.ID)
	}

	logger.Info("Publishing reshare",
		"walletID", w.ID, "trigger", trig.String(), "key_types", keyTypeStrings(attempted),
		"new_committee", plan.NodeIDs, "new_threshold", plan.Threshold, "sessionID", sessionID)

	// Bound the total time to both key families' result timeouts plus slack.
	pctx, cancel := context.WithTimeout(ctx, 2*o.cfg.ReshareResultTimeout+time.Minute)
	defer cancel()

	res, err := o.publisher.ReshareWallet(pctx, w.ID, plan.NodeIDs, plan.Threshold, only)
	rec.CompletedAt = time.Now().UTC()

	// §7.4 + §5E: verify keyinfo committee/version and pubkey immutability for
	// every key family that completed (even on partial failure, since a
	// completed family already wrote its keyinfo).
	if len(res.Done) > 0 {
		oldVersions := map[KeyKind]int{}
		for kind, k := range w.Keys {
			oldVersions[kind] = k.Version
		}
		vr := o.verifier.Verify(w.ID, plan.NodeIDs, oldVersions, res)
		rec.Verified = vr.OK
		rec.VerifyProblems = vr.Problems
		rec.Frozen = vr.Frozen
		if !vr.OK {
			logger.Error("Reshare: post-reshare verification problems", errors.New("verification failed"),
				"walletID", w.ID, "problems", vr.Problems, "frozen", vr.Frozen)
		}
	}

	if err != nil {
		remaining := RemainingKeyTypes(attempted, res.Done)
		if perr := o.progress.SetPending(w.ID, remaining); perr != nil {
			logger.Error("Reshare: persist partial progress failed", perr, "walletID", w.ID)
		}
		rec.Result = "failure"
		rec.ErrorReason = err.Error()
		logger.Error("Reshare failed", err, "walletID", w.ID, "sessionID", sessionID,
			"remaining_key_types", keyTypeStrings(remaining))
		if merr := o.lock.MarkFailure(w.ID); merr != nil {
			logger.Error("Reshare: mark failure cooldown failed", merr, "walletID", w.ID)
		}
	} else {
		if perr := o.progress.Clear(w.ID); perr != nil {
			logger.Error("Reshare: clear progress failed", perr, "walletID", w.ID)
		}
		rec.Result = "success"
		logger.Info("Reshare succeeded", "walletID", w.ID, "sessionID", sessionID, "new_committee", plan.NodeIDs)
		if merr := o.lock.MarkSuccess(w.ID); merr != nil {
			logger.Error("Reshare: mark success cooldown failed", merr, "walletID", w.ID)
		}
	}

	if err := o.auditor.Write(rec); err != nil {
		logger.Error("Reshare: final audit write failed", err, "walletID", w.ID)
	}
}

func aliveList(committee []string, set map[string]bool) []string {
	out := make([]string, 0, len(committee))
	for _, id := range committee {
		if set[id] {
			out = append(out, id)
		}
	}
	return out
}
