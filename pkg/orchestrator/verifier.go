package orchestrator

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/hashicorp/consul/api"

	"github.com/fystack/mpcium/pkg/keyinfo"
	"github.com/fystack/mpcium/pkg/types"
)

const (
	pubkeyPrefix = "orchestrator/reshare/pubkey/" // {walletID}/{kind}
	freezePrefix = "orchestrator/reshare/freeze/" // {walletID}
)

// FreezeRecord flags a wallet whose reshare produced an unexpected public key
// change (auto_reshare_design.md §5E). A frozen wallet is skipped by the
// reconcile loop and requires operator intervention.
type FreezeRecord struct {
	WalletID  string    `json:"wallet_id"`
	Reason    string    `json:"reason"`
	KeyType   string    `json:"key_type"`
	Expected  string    `json:"expected_pubkey"`
	Got       string    `json:"got_pubkey"`
	FrozenAt  time.Time `json:"frozen_at"`
}

// VerificationResult summarizes post-reshare verification (§7.4 + §5E).
type VerificationResult struct {
	OK       bool
	Frozen   bool
	Problems []string
}

// Verifier performs Consul-only post-reshare verification (§7.4 success
// verification and §5E public-key immutability guard).
type Verifier struct {
	kv      *api.KV
	keyinfo keyinfo.Store
}

// NewVerifier creates a verifier backed by Consul KV and the keyinfo store.
func NewVerifier(kv *api.KV) *Verifier {
	return &Verifier{kv: kv, keyinfo: keyinfo.NewStore(kv)}
}

// keyKindFor maps a signing key family to the keyinfo/consul namespace.
func keyKindFor(kt types.KeyType) KeyKind {
	switch kt {
	case types.KeyTypeEd25519:
		return KeyKindEddsa
	case types.KeyTypeSecp256k1:
		return KeyKindEcdsa
	default:
		return ""
	}
}

// Verify checks the completed key families of a reshare against §7.4/§5E:
//
//   - §7.4: keyinfo.ParticipantPeerIDs == newCommittee AND Version incremented.
//   - §5E:  the resulting pubkey must equal the previously recorded pubkey
//     (reshare redistributes shares, it must NOT change the pubkey). The first
//     observation is trusted-on-first-use and recorded. A mismatch freezes the
//     wallet and is reported as a problem.
//
// oldVersions holds the pre-reshare keyinfo Version per family (from inventory).
func (v *Verifier) Verify(walletID string, newCommittee []string, oldVersions map[KeyKind]int, res ReshareResult) VerificationResult {
	out := VerificationResult{OK: true}

	for kt, done := range res.Done {
		if !done {
			continue
		}
		kind := keyKindFor(kt)
		if kind == "" {
			out.OK = false
			out.Problems = append(out.Problems, fmt.Sprintf("%s: unknown key family", kt))
			continue
		}

		// §7.4 — keyinfo committee + version.
		info, err := v.keyinfo.Get(fmt.Sprintf("%s:%s", kind, walletID))
		if err != nil {
			out.OK = false
			out.Problems = append(out.Problems, fmt.Sprintf("%s: keyinfo read failed: %v", kind, err))
		} else {
			if !equalStringSet(info.ParticipantPeerIDs, newCommittee) {
				out.OK = false
				out.Problems = append(out.Problems, fmt.Sprintf(
					"%s: keyinfo committee %v != expected %v", kind, info.ParticipantPeerIDs, newCommittee))
			}
			if old, ok := oldVersions[kind]; ok && info.Version <= old {
				out.OK = false
				out.Problems = append(out.Problems, fmt.Sprintf(
					"%s: keyinfo version not incremented (old=%d, new=%d)", kind, old, info.Version))
			}
		}

		// §5E — public key immutability.
		newPub := res.PubKeys[kt]
		if len(newPub) == 0 {
			out.OK = false
			out.Problems = append(out.Problems, fmt.Sprintf("%s: empty result pubkey", kind))
			continue
		}
		stored, rerr := v.readPubkey(walletID, kind)
		if rerr != nil {
			out.OK = false
			out.Problems = append(out.Problems, fmt.Sprintf("%s: pubkey read failed: %v", kind, rerr))
			continue
		}
		if stored == nil {
			// Trust-on-first-use: record the baseline pubkey.
			if werr := v.writePubkey(walletID, kind, newPub); werr != nil {
				out.Problems = append(out.Problems, fmt.Sprintf("%s: pubkey baseline write failed: %v", kind, werr))
			}
			continue
		}
		if !bytes.Equal(stored, newPub) {
			out.OK = false
			out.Frozen = true
			out.Problems = append(out.Problems, fmt.Sprintf(
				"%s: PUBKEY CHANGED after reshare (expected %s, got %s) — wallet frozen",
				kind, hex.EncodeToString(stored), hex.EncodeToString(newPub)))
			_ = v.Freeze(FreezeRecord{
				WalletID: walletID,
				Reason:   "reshare changed public key",
				KeyType:  string(kind),
				Expected: hex.EncodeToString(stored),
				Got:      hex.EncodeToString(newPub),
				FrozenAt: time.Now().UTC(),
			})
		}
	}

	return out
}

// IsFrozen reports whether a wallet is flagged frozen (§5E).
func (v *Verifier) IsFrozen(walletID string) (bool, error) {
	pair, _, err := v.kv.Get(freezePrefix+walletID, nil)
	if err != nil {
		return false, fmt.Errorf("freeze: get: %w", err)
	}
	return pair != nil, nil
}

// Freeze writes a freeze flag for a wallet.
func (v *Verifier) Freeze(rec FreezeRecord) error {
	val, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if _, err := v.kv.Put(&api.KVPair{Key: freezePrefix + rec.WalletID, Value: val}, nil); err != nil {
		return fmt.Errorf("freeze: put: %w", err)
	}
	return nil
}

func (v *Verifier) readPubkey(walletID string, kind KeyKind) ([]byte, error) {
	pair, _, err := v.kv.Get(fmt.Sprintf("%s%s/%s", pubkeyPrefix, walletID, kind), nil)
	if err != nil {
		return nil, err
	}
	if pair == nil {
		return nil, nil
	}
	return append([]byte(nil), pair.Value...), nil
}

func (v *Verifier) writePubkey(walletID string, kind KeyKind, pub []byte) error {
	_, err := v.kv.Put(&api.KVPair{
		Key:   fmt.Sprintf("%s%s/%s", pubkeyPrefix, walletID, kind),
		Value: append([]byte(nil), pub...),
	}, nil)
	return err
}

// equalStringSet reports whether two string slices contain the same set of
// elements (order-independent, duplicates ignored).
func equalStringSet(a, b []string) bool {
	as := append([]string(nil), a...)
	bs := append([]string(nil), b...)
	sort.Strings(as)
	sort.Strings(bs)
	as = dedupSorted(as)
	bs = dedupSorted(bs)
	if len(as) != len(bs) {
		return false
	}
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}

func dedupSorted(s []string) []string {
	out := s[:0]
	var prev string
	for i, v := range s {
		if i == 0 || v != prev {
			out = append(out, v)
			prev = v
		}
	}
	return out
}
