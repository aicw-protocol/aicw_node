package orchestrator

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hashicorp/consul/api"

	"github.com/fystack/mpcium/pkg/keyinfo"
)

const keyinfoPrefix = "threshold_keyinfo/"

// Inventory reads wallet committees from Consul (auto_reshare_design.md §8).
// Source of truth is the Consul prefix scan of threshold_keyinfo/{eddsa|ecdsa}:*.
type Inventory struct {
	kv *api.KV
}

// NewInventory creates a Consul-backed wallet inventory.
func NewInventory(kv *api.KV) *Inventory {
	return &Inventory{kv: kv}
}

// List returns every wallet with its per-key-family committee/threshold/version.
func (in *Inventory) List() ([]Wallet, error) {
	pairs, _, err := in.kv.List(keyinfoPrefix, nil)
	if err != nil {
		return nil, fmt.Errorf("inventory: list %q: %w", keyinfoPrefix, err)
	}

	wallets := make(map[string]*Wallet)
	for _, p := range pairs {
		suffix := strings.TrimPrefix(p.Key, keyinfoPrefix) // e.g. "eddsa:<uuid>"
		kind, walletID, ok := splitKeyinfoSuffix(suffix)
		if !ok {
			continue
		}

		var info keyinfo.KeyInfo
		if err := json.Unmarshal(p.Value, &info); err != nil {
			// Skip malformed entries rather than failing the whole scan.
			fmt.Printf("warning: inventory: skipping malformed keyinfo %q: %v\n", p.Key, err)
			continue
		}

		w := wallets[walletID]
		if w == nil {
			w = &Wallet{ID: walletID, Keys: make(map[KeyKind]WalletKey)}
			wallets[walletID] = w
		}
		w.Keys[kind] = WalletKey{
			Kind:      kind,
			Committee: info.ParticipantPeerIDs,
			Threshold: info.Threshold,
			Version:   info.Version,
		}
	}

	out := make([]Wallet, 0, len(wallets))
	for _, w := range wallets {
		out = append(out, *w)
	}
	return out, nil
}

// splitKeyinfoSuffix parses "eddsa:<uuid>" / "ecdsa:<uuid>".
func splitKeyinfoSuffix(suffix string) (KeyKind, string, bool) {
	idx := strings.IndexByte(suffix, ':')
	if idx <= 0 {
		return "", "", false
	}
	kind := KeyKind(suffix[:idx])
	walletID := suffix[idx+1:]
	if walletID == "" {
		return "", "", false
	}
	switch kind {
	case KeyKindEddsa, KeyKindEcdsa:
		return kind, walletID, true
	default:
		return "", "", false
	}
}
