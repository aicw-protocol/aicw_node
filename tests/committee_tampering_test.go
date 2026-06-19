// Package tests provides integration tests for AICW MPC network.
//
// AICW-FORK: Committee tampering rejection test for Phase A security.
package tests

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"github.com/aicw/aicw_node/pkg/types"
)

// TestCommitteeTamperingRejection verifies that modifying participant_peer_ids
// after signing causes verification to fail.
func TestCommitteeTamperingRejection(t *testing.T) {
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("key generation failed: %v", err)
	}

	originalMsg := &types.SignTxMessage{
		KeyType:             types.KeyTypeEd25519,
		WalletID:            "test-wallet-001",
		NetworkInternalCode: "solana-localnet",
		TxID:                "tx-tampering-test",
		Tx:                  []byte("test transaction data"),
		ParticipantPeerIDs:  []string{"node1", "node2"},
	}

	rawOriginal, err := originalMsg.Raw()
	if err != nil {
		t.Fatalf("Raw() failed: %v", err)
	}
	t.Logf("original Raw (hex): %x", rawOriginal)

	signature := ed25519.Sign(privKey, rawOriginal)
	originalMsg.Signature = signature
	t.Logf("signature created (len=%d)", len(signature))

	if !ed25519.Verify(pubKey, rawOriginal, signature) {
		t.Fatal("original message signature verification failed")
	}
	t.Log("original message signature verified")

	tamperedMsg := &types.SignTxMessage{
		KeyType:             originalMsg.KeyType,
		WalletID:            originalMsg.WalletID,
		NetworkInternalCode: originalMsg.NetworkInternalCode,
		TxID:                originalMsg.TxID,
		Tx:                  originalMsg.Tx,
		ParticipantPeerIDs:  []string{"node1", "node3"},
		Signature:           originalMsg.Signature,
	}

	rawTampered, err := tamperedMsg.Raw()
	if err != nil {
		t.Fatalf("tampered message Raw() failed: %v", err)
	}
	t.Logf("tampered Raw (hex): %x", rawTampered)

	if string(rawOriginal) == string(rawTampered) {
		t.Fatal("Raw() unchanged - participant_peer_ids not included in Raw()")
	}
	t.Log("Raw() differs as expected")

	if ed25519.Verify(pubKey, rawTampered, signature) {
		t.Fatal("tampered message signature verification succeeded - security bug")
	}
	t.Log("tampered message signature rejected as expected")
}

// TestCommitteeTamperingOrderDoesNotMatter verifies that the order of
// participant IDs doesn't affect the signature (they are sorted).
func TestCommitteeTamperingOrderDoesNotMatter(t *testing.T) {
	msg1 := &types.SignTxMessage{
		KeyType:             types.KeyTypeEd25519,
		WalletID:            "wallet",
		NetworkInternalCode: "net",
		TxID:                "tx",
		Tx:                  []byte("data"),
		ParticipantPeerIDs:  []string{"node2", "node1", "node3"},
	}

	msg2 := &types.SignTxMessage{
		KeyType:             types.KeyTypeEd25519,
		WalletID:            "wallet",
		NetworkInternalCode: "net",
		TxID:                "tx",
		Tx:                  []byte("data"),
		ParticipantPeerIDs:  []string{"node3", "node1", "node2"},
	}

	raw1, _ := msg1.Raw()
	raw2, _ := msg2.Raw()

	if string(raw1) != string(raw2) {
		t.Fatal("same committee in different order produced different Raw()")
	}
	t.Log("participant order does not affect Raw()")
}

// TestCommitteeTamperingAddMember tests adding a member to the committee.
func TestCommitteeTamperingAddMember(t *testing.T) {
	pubKey, privKey, _ := ed25519.GenerateKey(rand.Reader)

	originalMsg := &types.SignTxMessage{
		KeyType:             types.KeyTypeEd25519,
		WalletID:            "wallet",
		NetworkInternalCode: "net",
		TxID:                "tx",
		Tx:                  []byte("data"),
		ParticipantPeerIDs:  []string{"node1", "node2"},
	}

	rawOriginal, _ := originalMsg.Raw()
	signature := ed25519.Sign(privKey, rawOriginal)

	tamperedMsg := &types.SignTxMessage{
		KeyType:             types.KeyTypeEd25519,
		WalletID:            "wallet",
		NetworkInternalCode: "net",
		TxID:                "tx",
		Tx:                  []byte("data"),
		ParticipantPeerIDs:  []string{"node1", "node2", "node3"},
		Signature:           signature,
	}

	rawTampered, _ := tamperedMsg.Raw()

	if string(rawOriginal) == string(rawTampered) {
		t.Fatal("add-member tampering not detected")
	}

	if ed25519.Verify(pubKey, rawTampered, signature) {
		t.Fatal("add-member tampered message verified - security bug")
	}

	t.Log("add-member tampering rejected")
}

// TestCommitteeTamperingRemoveMember tests removing a member from the committee.
func TestCommitteeTamperingRemoveMember(t *testing.T) {
	pubKey, privKey, _ := ed25519.GenerateKey(rand.Reader)

	originalMsg := &types.SignTxMessage{
		KeyType:             types.KeyTypeEd25519,
		WalletID:            "wallet",
		NetworkInternalCode: "net",
		TxID:                "tx",
		Tx:                  []byte("data"),
		ParticipantPeerIDs:  []string{"node1", "node2", "node3"},
	}

	rawOriginal, _ := originalMsg.Raw()
	signature := ed25519.Sign(privKey, rawOriginal)

	tamperedMsg := &types.SignTxMessage{
		KeyType:             types.KeyTypeEd25519,
		WalletID:            "wallet",
		NetworkInternalCode: "net",
		TxID:                "tx",
		Tx:                  []byte("data"),
		ParticipantPeerIDs:  []string{"node1", "node2"},
		Signature:           signature,
	}

	rawTampered, _ := tamperedMsg.Raw()

	if string(rawOriginal) == string(rawTampered) {
		t.Fatal("remove-member tampering not detected")
	}

	if ed25519.Verify(pubKey, rawTampered, signature) {
		t.Fatal("remove-member tampered message verified - security bug")
	}

	t.Log("remove-member tampering rejected")
}

// TestEmptyCommitteeBackwardCompatibility tests that empty committee
// doesn't break existing behavior.
func TestEmptyCommitteeBackwardCompatibility(t *testing.T) {
	msg := &types.SignTxMessage{
		KeyType:             types.KeyTypeEd25519,
		WalletID:            "wallet",
		NetworkInternalCode: "net",
		TxID:                "tx",
		Tx:                  []byte("data"),
		ParticipantPeerIDs:  nil,
	}

	raw, err := msg.Raw()
	if err != nil {
		t.Fatalf("Raw() failed with empty committee: %v", err)
	}

	rawStr := string(raw)
	if contains(rawStr, "participant_peer_ids") {
		t.Log("Raw JSON:", rawStr)
		t.Fatal("empty committee should not include participant_peer_ids in Raw()")
	}

	t.Log("empty committee backward compatibility OK")
}
