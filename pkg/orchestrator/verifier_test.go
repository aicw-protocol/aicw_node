package orchestrator

import (
	"testing"

	"github.com/fystack/mpcium/pkg/types"
)

func TestKeyKindFor(t *testing.T) {
	if keyKindFor(types.KeyTypeEd25519) != KeyKindEddsa {
		t.Error("ed25519 must map to eddsa")
	}
	if keyKindFor(types.KeyTypeSecp256k1) != KeyKindEcdsa {
		t.Error("secp256k1 must map to ecdsa")
	}
	if keyKindFor(types.KeyType("bogus")) != "" {
		t.Error("unknown key type must map to empty kind")
	}
}

func TestEqualStringSet(t *testing.T) {
	cases := []struct {
		a, b []string
		want bool
	}{
		{[]string{"n1", "n2", "n3"}, []string{"n3", "n1", "n2"}, true}, // order-independent
		{[]string{"n1", "n2"}, []string{"n1", "n2", "n3"}, false},
		{[]string{"n1", "n1", "n2"}, []string{"n2", "n1"}, true}, // dedup
		{nil, nil, true},
		{[]string{"n1"}, nil, false},
	}
	for i, c := range cases {
		if got := equalStringSet(c.a, c.b); got != c.want {
			t.Errorf("case %d: equalStringSet(%v,%v)=%v want %v", i, c.a, c.b, got, c.want)
		}
	}
}
