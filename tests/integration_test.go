// Package tests provides integration tests for AICW MPC network.
//
// AICW-FORK: Tests for Phase A dynamic peer functionality.
package tests

import (
	"testing"

	"github.com/aicw/aicw_node/pkg/eligibility"
	"github.com/aicw/aicw_node/pkg/identity"
	"github.com/aicw/aicw_node/pkg/mpc"
	"github.com/aicw/aicw_node/pkg/types"
	mpciumtypes "github.com/fystack/mpcium/pkg/types"
)

// TestEligibilityWhitelist tests the whitelist-based eligibility verification.
func TestEligibilityWhitelist(t *testing.T) {
	t.Run("InitiatorVerifier_Legacy", func(t *testing.T) {
		// Test legacy mode (single pubkey)
		pubKey := make([]byte, 32)
		verifier, err := eligibility.NewLegacyInitiatorVerifier(pubKey, "ed25519")
		if err != nil {
			t.Fatalf("Failed to create legacy verifier: %v", err)
		}

		if verifier.Name() != "legacy" {
			t.Errorf("Expected name 'legacy', got '%s'", verifier.Name())
		}
	})

	t.Run("MembershipVerifier_Whitelist_EmptyFile", func(t *testing.T) {
		// Test whitelist mode with non-existent file (should be valid empty whitelist)
		verifier, err := eligibility.NewWhitelistMembershipVerifier("file", "/nonexistent", nil)
		if err != nil {
			t.Fatalf("Failed to create whitelist verifier: %v", err)
		}

		// Should fail for any node (empty whitelist)
		err = verifier.VerifyMembership("node1", []byte{}, nil)
		if err == nil {
			t.Error("Expected error for non-whitelisted node")
		}
	})

	t.Run("StakeVerifier_NotImplemented", func(t *testing.T) {
		// Test stake stubs return not implemented error
		stakeInit := eligibility.NewStakeInitiatorVerifier()
		if stakeInit.Name() != "stake" {
			t.Errorf("Expected name 'stake', got '%s'", stakeInit.Name())
		}

		err := stakeInit.VerifyInitiator(nil)
		if err != eligibility.ErrNotImplemented {
			t.Errorf("Expected ErrNotImplemented, got %v", err)
		}

		stakeMem := eligibility.NewStakeMembershipVerifier()
		err = stakeMem.VerifyMembership("node1", nil, nil)
		if err != eligibility.ErrNotImplemented {
			t.Errorf("Expected ErrNotImplemented, got %v", err)
		}
	})
}

// TestSignTxMessageRaw tests that participant_peer_ids is included in Raw().
func TestSignTxMessageRaw(t *testing.T) {
	t.Run("Raw_IncludesParticipants", func(t *testing.T) {
		msg := &types.SignTxMessage{
			KeyType:             types.KeyTypeSecp256k1,
			WalletID:            "wallet1",
			NetworkInternalCode: "ETH",
			TxID:                "tx123",
			Tx:                  []byte("transaction data"),
			ParticipantPeerIDs:  []string{"node2", "node1", "node3"},
		}

		raw1, err := msg.Raw()
		if err != nil {
			t.Fatalf("Failed to get Raw(): %v", err)
		}

		// Same participants in different order should produce same raw
		msg2 := &types.SignTxMessage{
			KeyType:             types.KeyTypeSecp256k1,
			WalletID:            "wallet1",
			NetworkInternalCode: "ETH",
			TxID:                "tx123",
			Tx:                  []byte("transaction data"),
			ParticipantPeerIDs:  []string{"node3", "node1", "node2"},
		}

		raw2, err := msg2.Raw()
		if err != nil {
			t.Fatalf("Failed to get Raw(): %v", err)
		}

		// Should be identical (participants are sorted)
		if string(raw1) != string(raw2) {
			t.Error("Raw() should be deterministic regardless of participant order")
		}
	})

	t.Run("Raw_EmptyParticipants", func(t *testing.T) {
		msg := &types.SignTxMessage{
			KeyType:             types.KeyTypeSecp256k1,
			WalletID:            "wallet1",
			NetworkInternalCode: "ETH",
			TxID:                "tx123",
			Tx:                  []byte("transaction data"),
			ParticipantPeerIDs:  nil, // Empty
		}

		raw, err := msg.Raw()
		if err != nil {
			t.Fatalf("Failed to get Raw(): %v", err)
		}

		// Should not contain participant_peer_ids key (backward compatible)
		if contains(string(raw), "participant_peer_ids") {
			t.Error("Raw() should not include participant_peer_ids when empty")
		}
	})

	t.Run("ValidateParticipants", func(t *testing.T) {
		msg := &types.SignTxMessage{
			ParticipantPeerIDs: []string{"node1", "node2"},
		}

		available := []string{"node1", "node2", "node3"}
		threshold := 1 // 2-of-3

		err := msg.ValidateParticipants(available, threshold)
		if err != nil {
			t.Fatalf("Validation should pass: %v", err)
		}

		// Test insufficient peers
		msg.ParticipantPeerIDs = []string{"node1"}
		err = msg.ValidateParticipants(available, threshold)
		if err == nil {
			t.Error("Expected error for insufficient peers")
		}

		// Test unavailable peer
		msg.ParticipantPeerIDs = []string{"node1", "node4"}
		err = msg.ValidateParticipants(available, threshold)
		if err == nil {
			t.Error("Expected error for unavailable peer")
		}
	})
}

// TestDynamicRegistry tests dynamic peer management.
func TestDynamicRegistry(t *testing.T) {
	t.Run("AddRemovePeer", func(t *testing.T) {
		// Create a mock identity store
		store := &mockIdentityStore{
			publicKeys: make(map[string][]byte),
		}

		registry := mpc.NewDynamicRegistry("node1", 1, nil, store)

		// Add a peer
		err := registry.AddPeer("node2", []byte("pubkey2"))
		if err != nil {
			t.Fatalf("Failed to add peer: %v", err)
		}

		peers := registry.GetAllPeerIDs()
		if len(peers) != 1 || peers[0] != "node2" {
			t.Error("Peer not added correctly")
		}

		// Remove the peer
		err = registry.RemovePeer("node2")
		if err != nil {
			t.Fatalf("Failed to remove peer: %v", err)
		}

		peers = registry.GetAllPeerIDs()
		if len(peers) != 0 {
			t.Error("Peer not removed correctly")
		}
	})

	t.Run("CannotAddSelf", func(t *testing.T) {
		store := &mockIdentityStore{publicKeys: make(map[string][]byte)}
		registry := mpc.NewDynamicRegistry("node1", 1, nil, store)

		err := registry.AddPeer("node1", []byte("pubkey"))
		if err == nil {
			t.Error("Should not be able to add self as peer")
		}
	})
}

// mockIdentityStore is a simple mock for testing.
type mockIdentityStore struct {
	publicKeys    map[string][]byte
	symmetricKeys map[string][]byte
}

func (m *mockIdentityStore) GetPublicKey(nodeID string) ([]byte, error) {
	if key, ok := m.publicKeys[nodeID]; ok {
		return key, nil
	}
	return nil, &types.CommitteeError{Message: "not found"}
}

func (m *mockIdentityStore) RegisterPeerPublicKey(nodeID string, publicKey []byte) error {
	m.publicKeys[nodeID] = publicKey
	return nil
}

func (m *mockIdentityStore) UnregisterPeerPublicKey(nodeID string) error {
	delete(m.publicKeys, nodeID)
	return nil
}

func (m *mockIdentityStore) GetAllPeerIDs() []string {
	var ids []string
	for id := range m.publicKeys {
		ids = append(ids, id)
	}
	return ids
}

func (m *mockIdentityStore) GetSelfNodeID() string { return "node1" }

func (m *mockIdentityStore) GetSelfPublicKey() ([]byte, error) { return []byte("self"), nil }

func (m *mockIdentityStore) SetSymmetricKey(peerID string, key []byte) {
	if m.symmetricKeys == nil {
		m.symmetricKeys = make(map[string][]byte)
	}
	m.symmetricKeys[peerID] = key
}

func (m *mockIdentityStore) GetSymmetricKey(peerID string) ([]byte, error) {
	if key, ok := m.symmetricKeys[peerID]; ok {
		return key, nil
	}
	return nil, &types.CommitteeError{Message: "not found"}
}

func (m *mockIdentityStore) RemoveSymmetricKey(peerID string) {
	delete(m.symmetricKeys, peerID)
}

func (m *mockIdentityStore) GetSymmetricKeyCount() int {
	return len(m.symmetricKeys)
}

func (m *mockIdentityStore) CheckSymmetricKeyComplete(desired int) bool {
	return len(m.symmetricKeys) >= desired
}

// GetSymetricKeyCount returns the count (preserving original mpcium typo for interface compat)
func (m *mockIdentityStore) GetSymetricKeyCount() int {
	return len(m.symmetricKeys)
}

// Original mpcium identity.Store interface methods (stub implementations for testing)
func (m *mockIdentityStore) VerifyInitiatorMessage(msg mpciumtypes.InitiatorMessage) error {
	return nil
}

func (m *mockIdentityStore) AuthorizeInitiatorMessage(msg mpciumtypes.InitiatorMessage) error {
	return nil
}

func (m *mockIdentityStore) SignMessage(msg *mpciumtypes.TssMessage) ([]byte, error) {
	return []byte("mock-signature"), nil
}

func (m *mockIdentityStore) VerifyMessage(msg *mpciumtypes.TssMessage) error {
	return nil
}

func (m *mockIdentityStore) SignEcdhMessage(msg *mpciumtypes.ECDHMessage) ([]byte, error) {
	return []byte("mock-ecdh-signature"), nil
}

func (m *mockIdentityStore) VerifySignature(msg *mpciumtypes.ECDHMessage) error {
	return nil
}

func (m *mockIdentityStore) EncryptMessage(plaintext []byte, peerID string) ([]byte, error) {
	return plaintext, nil // No encryption in mock
}

func (m *mockIdentityStore) DecryptMessage(cipher []byte, peerID string) ([]byte, error) {
	return cipher, nil // No decryption in mock
}

func (m *mockIdentityStore) SetMembershipVerifier(verifier eligibility.MembershipVerifier) {}

func (m *mockIdentityStore) LoadPeersFromConsul() error { return nil }

func (m *mockIdentityStore) WatchPeerDirectory(callback func(nodeID string, added bool)) error {
	return nil
}

func (m *mockIdentityStore) SyncSelfToConsul() error { return nil }

// Ensure mockIdentityStore implements DynamicStore
var _ identity.DynamicStore = (*mockIdentityStore)(nil)

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestStateRecovery tests node state recovery after restart.
func TestStateRecovery(t *testing.T) {
	t.Run("RegistryRecovery", func(t *testing.T) {
		// Simulate recovery scenario:
		// 1. Node starts fresh
		// 2. Loads peers from Consul
		// 3. Re-establishes ECDH keys
		
		// This is a placeholder for actual recovery testing
		// In a real test, we would:
		// - Start a node
		// - Add peers
		// - Restart the node
		// - Verify peers are recovered from Consul
		
		t.Log("State recovery test placeholder - requires Consul integration")
	})
}

// Benchmark tests for performance validation
func BenchmarkSignTxMessageRaw(b *testing.B) {
	msg := &types.SignTxMessage{
		KeyType:             types.KeyTypeSecp256k1,
		WalletID:            "wallet1",
		NetworkInternalCode: "ETH",
		TxID:                "tx123",
		Tx:                  make([]byte, 1024),
		ParticipantPeerIDs:  []string{"node1", "node2", "node3", "node4", "node5"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = msg.Raw()
	}
}
