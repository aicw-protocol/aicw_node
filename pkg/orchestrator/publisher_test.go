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
