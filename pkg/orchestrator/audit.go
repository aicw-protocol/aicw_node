package orchestrator

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/hashicorp/consul/api"
)

const auditPrefix = "reshare_audit/"

// AuditRecord is the verifiable reshare audit entry (auto_reshare_design.md §5.3C).
type AuditRecord struct {
	SessionID      string    `json:"session_id"`
	WalletID       string    `json:"wallet_id"`
	Trigger        string    `json:"trigger"` // proactive|urgent|manual
	OldCommittee   []string  `json:"old_committee"`
	NewCommittee   []string  `json:"new_committee"`
	AlivePing      []string  `json:"alive_ping"`
	AliveReady     []string  `json:"alive_ready"`
	PolicyVersion  string    `json:"policy_version"`
	PolicyHash     string    `json:"policy_hash"`
	InitiatorPub   string    `json:"initiator_pubkey"`
	NewThreshold   int       `json:"new_threshold"`
	Result         string    `json:"result"` // success|failure|published
	ErrorReason    string    `json:"error_reason,omitempty"`
	PublishedAt    time.Time `json:"published_at"`
	CompletedAt    time.Time `json:"completed_at,omitempty"`

	// Post-reshare verification (§7.4 + §5E).
	Verified       bool     `json:"verified"`
	VerifyProblems []string `json:"verify_problems,omitempty"`
	Frozen         bool     `json:"frozen,omitempty"`
}

// Auditor persists reshare audit records to Consul (§5.3C).
type Auditor struct {
	kv *api.KV
}

// NewAuditor creates a Consul-backed auditor.
func NewAuditor(kv *api.KV) *Auditor {
	return &Auditor{kv: kv}
}

// Write stores an audit record under reshare_audit/{sessionID}.
func (a *Auditor) Write(rec AuditRecord) error {
	val, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("audit: marshal: %w", err)
	}
	if _, err := a.kv.Put(&api.KVPair{Key: auditPrefix + rec.SessionID, Value: val}, nil); err != nil {
		return fmt.Errorf("audit: put: %w", err)
	}
	return nil
}
