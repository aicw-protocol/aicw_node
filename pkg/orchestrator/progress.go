package orchestrator

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/hashicorp/consul/api"

	"github.com/fystack/mpcium/pkg/types"
)

const progressPrefix = "orchestrator/reshare/pending/"

// progressRecord tracks which key families still need resharing for a wallet
// after a partial failure (auto_reshare_design.md §7.1: "EdDSA OK / ECDSA FAIL
// → reshare ECDSA only").
type progressRecord struct {
	KeyTypes  []types.KeyType `json:"key_types"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// ProgressStore persists per-wallet, per-key-family reshare progress in Consul
// so a partial success survives across reconcile passes and orchestrator
// restarts.
type ProgressStore struct {
	kv *api.KV
}

// NewProgressStore creates a Consul-backed progress store.
func NewProgressStore(kv *api.KV) *ProgressStore {
	return &ProgressStore{kv: kv}
}

// Pending returns the key families still needing a reshare for the wallet. A
// nil/empty result means "fresh" — reshare all families (§4.4).
func (s *ProgressStore) Pending(walletID string) ([]types.KeyType, error) {
	pair, _, err := s.kv.Get(progressPrefix+walletID, nil)
	if err != nil {
		return nil, fmt.Errorf("progress: get: %w", err)
	}
	if pair == nil {
		return nil, nil
	}
	var rec progressRecord
	if err := json.Unmarshal(pair.Value, &rec); err != nil {
		return nil, nil // treat unparseable as fresh
	}
	return rec.KeyTypes, nil
}

// SetPending records the remaining key families after a partial failure. When
// the remaining set is empty it clears the record instead.
func (s *ProgressStore) SetPending(walletID string, kts []types.KeyType) error {
	if len(kts) == 0 {
		return s.Clear(walletID)
	}
	val, err := json.Marshal(progressRecord{KeyTypes: kts, UpdatedAt: time.Now().UTC()})
	if err != nil {
		return err
	}
	if _, err := s.kv.Put(&api.KVPair{Key: progressPrefix + walletID, Value: val}, nil); err != nil {
		return fmt.Errorf("progress: put: %w", err)
	}
	return nil
}

// Clear removes any pending record for the wallet (full success).
func (s *ProgressStore) Clear(walletID string) error {
	if _, err := s.kv.Delete(progressPrefix+walletID, nil); err != nil {
		return fmt.Errorf("progress: delete: %w", err)
	}
	return nil
}
