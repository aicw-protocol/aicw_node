package identity

import (
	"crypto/ed25519"
	"encoding/hex"
	"testing"

	"github.com/spf13/viper"

	mpciumtypes "github.com/fystack/mpcium/pkg/types"
)

type staticInitiatorMessage struct {
	raw []byte
	sig []byte
}

func (m staticInitiatorMessage) Raw() ([]byte, error) { return m.raw, nil }
func (m staticInitiatorMessage) Sig() []byte          { return m.sig }
func (m staticInitiatorMessage) InitiatorID() string  { return "test" }
func (m staticInitiatorMessage) GetAuthorizerSignatures() []mpciumtypes.AuthorizerSignature {
	return nil
}

func TestLoadInitiatorKeysBridgeOnly(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	bridgePub, _, _ := ed25519.GenerateKey(nil)
	bridgeHex := hex.EncodeToString(bridgePub)

	viper.Set("event_initiator_algorithm", "ed25519")
	viper.Set("event_initiator_pubkey", bridgeHex)

	keys, err := loadInitiatorKeys()
	if err != nil {
		t.Fatalf("loadInitiatorKeys: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}
}

func TestLoadInitiatorKeysBridgeAndReshare(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	bridgePub, _, _ := ed25519.GenerateKey(nil)
	resharePub, _, _ := ed25519.GenerateKey(nil)

	viper.Set("event_initiator_algorithm", "ed25519")
	viper.Set("event_initiator_pubkey", hex.EncodeToString(bridgePub))
	viper.Set("reshare_initiator_pubkey", hex.EncodeToString(resharePub))

	keys, err := loadInitiatorKeys()
	if err != nil {
		t.Fatalf("loadInitiatorKeys: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
}

func TestLoadInitiatorKeysRejectsDuplicatePubkey(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	pub, _, _ := ed25519.GenerateKey(nil)
	pubHex := hex.EncodeToString(pub)

	viper.Set("event_initiator_algorithm", "ed25519")
	viper.Set("event_initiator_pubkey", pubHex)
	viper.Set("reshare_initiator_pubkey", pubHex)

	if _, err := loadInitiatorKeys(); err == nil {
		t.Fatal("expected error for duplicate pubkeys")
	}
}

func TestVerifyInitiatorMessageAcceptsEitherKey(t *testing.T) {
	_, bridgePriv, _ := ed25519.GenerateKey(nil)
	bridgePub := bridgePriv.Public().(ed25519.PublicKey)
	_, resharePriv, _ := ed25519.GenerateKey(nil)
	resharePub := resharePriv.Public().(ed25519.PublicKey)

	store := &DynamicFileStore{
		initiatorKeys: []*InitiatorKey{
			{Algorithm: mpciumtypes.EventInitiatorKeyTypeEd25519, Ed25519: bridgePub},
			{Algorithm: mpciumtypes.EventInitiatorKeyTypeEd25519, Ed25519: resharePub},
		},
	}

	raw := []byte("mpc-reshare-payload")
	bridgeSig := ed25519.Sign(bridgePriv, raw)
	reshareSig := ed25519.Sign(resharePriv, raw)
	_, otherPriv, _ := ed25519.GenerateKey(nil)
	otherSig := ed25519.Sign(otherPriv, raw)

	if err := store.VerifyInitiatorMessage(staticInitiatorMessage{raw: raw, sig: bridgeSig}); err != nil {
		t.Fatalf("bridge signature rejected: %v", err)
	}
	if err := store.VerifyInitiatorMessage(staticInitiatorMessage{raw: raw, sig: reshareSig}); err != nil {
		t.Fatalf("reshare signature rejected: %v", err)
	}
	if err := store.VerifyInitiatorMessage(staticInitiatorMessage{raw: raw, sig: otherSig}); err == nil {
		t.Fatal("expected unknown signer to be rejected")
	}
}
