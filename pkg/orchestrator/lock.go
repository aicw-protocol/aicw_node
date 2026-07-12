package orchestrator

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"
)

const (
	inflightPrefix = "orchestrator/reshare/inflight/"
	cooldownPrefix = "orchestrator/reshare/cooldown/"

	lockSessionName = "reshare-orchestrator"
)

// inflightRecord is stored under orchestrator/reshare/inflight/{walletId} (§6.1).
type inflightRecord struct {
	SessionID       string    `json:"session_id"`
	StartedAt       time.Time `json:"started_at"`
	KeyTypesPending []string  `json:"key_types_pending"`
}

// LockManager provides per-wallet in-flight locks and cooldown tracking (§6).
//
// The in-flight lock is a real Consul session lock (§6.1) with behavior=delete:
// each orchestrator instance owns one session that it renews periodically, and
// all its wallet locks are auto-released if the instance crashes or its session
// TTL lapses. This makes the lock safe for a multi-instance (HA) deployment.
// An in-process set additionally dedupes concurrent attempts for the same
// wallet within a single instance (Consul allows the same session to re-acquire
// a key it already holds).
type LockManager struct {
	kv              *api.KV
	sessions        *api.Session
	sessionID       string
	ttl             time.Duration
	cooldownSuccess time.Duration
	cooldownFailure time.Duration
	cooldownFailMax time.Duration

	renewDone chan struct{}

	mu   sync.Mutex
	held map[string]bool
}

// NewLockManager creates a Consul-session-backed lock/cooldown manager and
// starts periodic renewal of the session. Call Close to release it.
func NewLockManager(client *api.Client, ttl, cooldownSuccess, cooldownFailure, cooldownFailMax time.Duration) (*LockManager, error) {
	// Consul requires a session TTL between 10s and 24h; it internally doubles
	// the value and expects renewal at ~TTL/2.
	if ttl < 10*time.Second {
		ttl = 10 * time.Second
	}
	ttlStr := fmt.Sprintf("%ds", int(ttl.Seconds()))

	sessions := client.Session()
	sessionID, _, err := sessions.Create(&api.SessionEntry{
		Name:      lockSessionName,
		TTL:       ttlStr,
		Behavior:  api.SessionBehaviorDelete,
		LockDelay: 0, // allow immediate failover for urgent reshares
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("lock: create consul session: %w", err)
	}

	m := &LockManager{
		kv:              client.KV(),
		sessions:        sessions,
		sessionID:       sessionID,
		ttl:             ttl,
		cooldownSuccess: cooldownSuccess,
		cooldownFailure: cooldownFailure,
		cooldownFailMax: cooldownFailMax,
		renewDone:       make(chan struct{}),
		held:            make(map[string]bool),
	}

	go func() {
		// RenewPeriodic renews at ttl/2 until renewDone is closed.
		_ = sessions.RenewPeriodic(ttlStr, sessionID, nil, m.renewDone)
	}()

	return m, nil
}

// Close stops session renewal and destroys the Consul session, which (with
// behavior=delete) releases every in-flight lock held by this instance.
func (m *LockManager) Close() {
	select {
	case <-m.renewDone:
		// already closed
	default:
		close(m.renewDone)
	}
	if m.sessions != nil && m.sessionID != "" {
		_, _ = m.sessions.Destroy(m.sessionID, nil)
	}
}

// Acquire takes the per-wallet in-flight lock via a Consul session lock (§6.1).
// It returns (true, nil) only if the wallet is not already locked by this or
// any other orchestrator instance.
func (m *LockManager) Acquire(walletID, sessionID string, keyTypes []string) (bool, error) {
	// Fast per-instance dedupe: the same Consul session can re-acquire a key it
	// already holds, so guard concurrent attempts for the same wallet locally.
	m.mu.Lock()
	if m.held[walletID] {
		m.mu.Unlock()
		return false, nil
	}
	m.mu.Unlock()

	val, err := json.Marshal(inflightRecord{
		SessionID:       sessionID,
		StartedAt:       time.Now().UTC(),
		KeyTypesPending: keyTypes,
	})
	if err != nil {
		return false, err
	}

	key := inflightPrefix + walletID
	acquired, _, err := m.kv.Acquire(&api.KVPair{
		Key:     key,
		Value:   val,
		Session: m.sessionID,
	}, nil)
	if err != nil {
		return false, fmt.Errorf("lock: acquire %q: %w", key, err)
	}
	if !acquired {
		return false, nil // held by another instance's live session
	}

	m.mu.Lock()
	m.held[walletID] = true
	m.mu.Unlock()
	return true, nil
}

// Release frees the in-flight lock for a wallet (deletes the key, which also
// releases the session hold).
func (m *LockManager) Release(walletID string) error {
	m.mu.Lock()
	delete(m.held, walletID)
	m.mu.Unlock()

	key := inflightPrefix + walletID
	if _, err := m.kv.Delete(key, nil); err != nil {
		return fmt.Errorf("lock: release %q: %w", key, err)
	}
	return nil
}

// cooldownRecord is stored under orchestrator/reshare/cooldown/{walletId} (§6.2).
type cooldownRecord struct {
	NextEligible time.Time `json:"next_eligible"`
	FailCount    int       `json:"fail_count"`
}

// CooldownOK reports whether a wallet is past its cooldown window.
func (m *LockManager) CooldownOK(walletID string) (bool, error) {
	rec, err := m.readCooldown(walletID)
	if err != nil {
		return false, err
	}
	if rec == nil {
		return true, nil
	}
	return time.Now().UTC().After(rec.NextEligible), nil
}

// MarkSuccess sets the success cooldown (§6.2) and clears the failure backoff.
func (m *LockManager) MarkSuccess(walletID string) error {
	return m.writeCooldown(walletID, cooldownRecord{
		NextEligible: time.Now().UTC().Add(m.cooldownSuccess),
		FailCount:    0,
	})
}

// MarkFailure applies an exponential failure backoff capped at cooldownFailMax.
func (m *LockManager) MarkFailure(walletID string) error {
	rec, err := m.readCooldown(walletID)
	if err != nil {
		return err
	}
	failCount := 1
	if rec != nil {
		failCount = rec.FailCount + 1
	}
	backoff := m.cooldownFailure
	for i := 1; i < failCount; i++ {
		backoff *= 2
		if backoff >= m.cooldownFailMax {
			backoff = m.cooldownFailMax
			break
		}
	}
	return m.writeCooldown(walletID, cooldownRecord{
		NextEligible: time.Now().UTC().Add(backoff),
		FailCount:    failCount,
	})
}

func (m *LockManager) readCooldown(walletID string) (*cooldownRecord, error) {
	pair, _, err := m.kv.Get(cooldownPrefix+walletID, nil)
	if err != nil {
		return nil, fmt.Errorf("cooldown: get: %w", err)
	}
	if pair == nil {
		return nil, nil
	}
	var rec cooldownRecord
	if err := json.Unmarshal(pair.Value, &rec); err != nil {
		return nil, nil // treat unparseable as no cooldown
	}
	return &rec, nil
}

func (m *LockManager) writeCooldown(walletID string, rec cooldownRecord) error {
	val, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if _, err := m.kv.Put(&api.KVPair{Key: cooldownPrefix + walletID, Value: val}, nil); err != nil {
		return fmt.Errorf("cooldown: put: %w", err)
	}
	return nil
}
