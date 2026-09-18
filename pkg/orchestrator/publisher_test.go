package orchestrator

import (
	"testing"

	"github.com/fystack/mpcium/pkg/types"
)

func TestAttemptedKeyTypes(t *testing.T) {
	// Empty => full sequential set (ed25519, secp256k1).
	got := AttemptedKeyTypes(nil)
	if len(got) != 2 || got[0] != types.KeyTypeEd25519 || got[1] != types.KeyTypeSecp256k1 {
		t.Fatalf("AttemptedKeyTypes(nil) = %v, want [ed25519 secp256k1]", got)
	}

	// Explicit => passthrough.
	only := []types.KeyType{types.KeyTypeSecp256k1}
	got = AttemptedKeyTypes(only)
	if len(got) != 1 || got[0] != types.KeyTypeSecp256k1 {
		t.Fatalf("AttemptedKeyTypes(only) = %v, want [secp256k1]", got)
	}
}

func TestWalletKeyTypes(t *testing.T) {
	// Ed25519-only wallet (AICW default keygen) => only ed25519 is resharded.
	w := Wallet{ID: "w1", Keys: map[KeyKind]WalletKey{KeyKindEddsa: {Kind: KeyKindEddsa}}}
	got := WalletKeyTypes(w)
	if len(got) != 1 || got[0] != types.KeyTypeEd25519 {
		t.Fatalf("WalletKeyTypes(eddsa-only) = %v, want [ed25519]", got)
	}

	// Both families => canonical order ed25519, secp256k1 regardless of map order.
	w.Keys[KeyKindEcdsa] = WalletKey{Kind: KeyKindEcdsa}
	got = WalletKeyTypes(w)
	if len(got) != 2 || got[0] != types.KeyTypeEd25519 || got[1] != types.KeyTypeSecp256k1 {
		t.Fatalf("WalletKeyTypes(both) = %v, want [ed25519 secp256k1]", got)
	}

	// No keyinfo at all => nil, so AttemptedKeyTypes falls back to the full set.
	if got := WalletKeyTypes(Wallet{ID: "w2"}); got != nil {
		t.Fatalf("WalletKeyTypes(empty) = %v, want nil", got)
	}
}

func TestRemainingKeyTypes(t *testing.T) {
	attempted := []types.KeyType{types.KeyTypeEd25519, types.KeyTypeSecp256k1}

	// EdDSA OK / ECDSA FAIL -> remaining = [secp256k1] (§7.1).
	done := map[types.KeyType]bool{types.KeyTypeEd25519: true}
	rem := RemainingKeyTypes(attempted, done)
	if len(rem) != 1 || rem[0] != types.KeyTypeSecp256k1 {
		t.Fatalf("remaining after EdDSA-only success = %v, want [secp256k1]", rem)
	}

	// EdDSA FAIL (never reached ECDSA) -> remaining = both, in order.
	rem = RemainingKeyTypes(attempted, map[types.KeyType]bool{})
	if len(rem) != 2 || rem[0] != types.KeyTypeEd25519 || rem[1] != types.KeyTypeSecp256k1 {
		t.Fatalf("remaining after full failure = %v, want [ed25519 secp256k1]", rem)
	}

	// All done -> empty (progress cleared).
	rem = RemainingKeyTypes(attempted, map[types.KeyType]bool{
		types.KeyTypeEd25519:   true,
		types.KeyTypeSecp256k1: true,
	})
	if len(rem) != 0 {
		t.Fatalf("remaining after full success = %v, want []", rem)
	}
}
