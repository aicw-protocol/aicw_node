package mpc

import (
	"fmt"
	"testing"

	"github.com/aicw/aicw_node/pkg/eligibility"
	"github.com/aicw/aicw_node/pkg/identity"
	mpciumpc "github.com/fystack/mpcium/pkg/mpc"
	mpciumtypes "github.com/fystack/mpcium/pkg/types"
)

type rejoinMockStore struct {
	publicKeys    map[string][]byte
	symmetricKeys map[string][]byte
}

func newRejoinMockStore() *rejoinMockStore {
	return &rejoinMockStore{
		publicKeys:    make(map[string][]byte),
		symmetricKeys: make(map[string][]byte),
	}
}

func (m *rejoinMockStore) GetPublicKey(nodeID string) ([]byte, error) {
	if k, ok := m.publicKeys[nodeID]; ok {
		return k, nil
	}
	return nil, fmt.Errorf("not found")
}

func (m *rejoinMockStore) RegisterPeerPublicKey(nodeID string, key []byte) error {
	m.publicKeys[nodeID] = key
	return nil
}

func (m *rejoinMockStore) UnregisterPeerPublicKey(nodeID string) error {
	delete(m.publicKeys, nodeID)
	return nil
}

func (m *rejoinMockStore) SetSymmetricKey(peerID string, key []byte) {
	m.symmetricKeys[peerID] = key
}

func (m *rejoinMockStore) GetSymmetricKey(peerID string) ([]byte, error) {
	if k, ok := m.symmetricKeys[peerID]; ok {
		return k, nil
	}
	return nil, fmt.Errorf("symmetric key not found")
}

func (m *rejoinMockStore) RemoveSymmetricKey(peerID string) {
	delete(m.symmetricKeys, peerID)
}

func (m *rejoinMockStore) GetSymmetricKeyCount() int { return len(m.symmetricKeys) }
func (m *rejoinMockStore) GetSymetricKeyCount() int  { return len(m.symmetricKeys) }
func (m *rejoinMockStore) CheckSymmetricKeyComplete(desired int) bool {
	return len(m.symmetricKeys) >= desired
}
func (m *rejoinMockStore) GetAllPeerIDs() []string {
	ids := make([]string, 0, len(m.publicKeys))
	for id := range m.publicKeys {
		ids = append(ids, id)
	}
	return ids
}
func (m *rejoinMockStore) GetSelfNodeID() string                                { return "self" }
func (m *rejoinMockStore) GetSelfPublicKey() ([]byte, error)                    { return []byte("self-pk"), nil }
func (m *rejoinMockStore) SetMembershipVerifier(eligibility.MembershipVerifier) {}

func (m *rejoinMockStore) VerifyInitiatorMessage(mpciumtypes.InitiatorMessage) error { return nil }
func (m *rejoinMockStore) AuthorizeInitiatorMessage(mpciumtypes.InitiatorMessage) error {
	return nil
}
func (m *rejoinMockStore) SignMessage(*mpciumtypes.TssMessage) ([]byte, error) {
	return []byte("sig"), nil
}
func (m *rejoinMockStore) VerifyMessage(*mpciumtypes.TssMessage) error { return nil }
func (m *rejoinMockStore) SignEcdhMessage(*mpciumtypes.ECDHMessage) ([]byte, error) {
	return []byte("sig"), nil
}
func (m *rejoinMockStore) VerifySignature(*mpciumtypes.ECDHMessage) error { return nil }
func (m *rejoinMockStore) EncryptMessage(plaintext []byte, _ string) ([]byte, error) {
	return plaintext, nil
}
func (m *rejoinMockStore) DecryptMessage(cipher []byte, _ string) ([]byte, error) {
	return cipher, nil
}
func (m *rejoinMockStore) LoadPeersFromConsul() error { return nil }
func (m *rejoinMockStore) WatchPeerDirectory(func(string, bool)) error {
	return nil
}
func (m *rejoinMockStore) SyncSelfToConsul() error { return nil }

var _ identity.DynamicStore = (*rejoinMockStore)(nil)

type recordingECDHSession struct {
	addPeerCalls []string
	broadcasts   int
}

func (s *recordingECDHSession) AddPeer(peerID string) error {
	s.addPeerCalls = append(s.addPeerCalls, peerID)
	return nil
}
func (s *recordingECDHSession) RemovePeer(string)                    {}
func (s *recordingECDHSession) GetExpectedPeerCount() int            { return 0 }
func (s *recordingECDHSession) GetMeshExpectedPeerCount() int        { return 0 }
func (s *recordingECDHSession) GetMeshCompletedKeyCount() int        { return 0 }
func (s *recordingECDHSession) ClearCeremonyScope()                  {}
func (s *recordingECDHSession) SetCeremonyPeers([]string)            {}
func (s *recordingECDHSession) EnsureECDH([]string) error            { return nil }
func (s *recordingECDHSession) GetCompletedKeyCount() int            { return 0 }
func (s *recordingECDHSession) IsKeyExchangeComplete() bool          { return false }
func (s *recordingECDHSession) SetInnerSession(mpciumpc.ECDHSession) {}
func (s *recordingECDHSession) ListenKeyExchange() error             { return nil }
func (s *recordingECDHSession) BroadcastPublicKey() error {
	s.broadcasts++
	return nil
}

// TestMeshCountsIgnoreCeremonyScope verifies the P0-b root cause: after a
// committee ceremony, ceremonyPeers=3 must not make periodic broadcast think
// the full mesh is complete while one mesh peer still lacks a key.
func TestMeshCountsIgnoreCeremonyScope(t *testing.T) {
	store := newRejoinMockStore()
	session := NewDynamicECDHSession("self", store)

	peers := []string{"p1", "p2", "p3", "p4"}
	session.InitializeWithPeers(peers)

	// Simulate post-keygen ceremony scope (committee size 4 → 3 peers excluding self).
	session.EnsureECDH(append([]string{"self"}, peers[:3]...))

	for _, id := range peers[:3] {
		store.SetSymmetricKey(id, []byte("key-"+id))
	}

	if got := session.GetExpectedPeerCount(); got != 3 {
		t.Fatalf("ceremony expected = %d, want 3", got)
	}
	if got := session.GetCompletedKeyCount(); got != 3 {
		t.Fatalf("store key count = %d, want 3", got)
	}
	if session.IsKeyExchangeComplete() {
		t.Fatal("IsKeyExchangeComplete should be false: p4 has no key")
	}
	if got := session.GetMeshCompletedKeyCount(); got != 3 {
		t.Fatalf("mesh completed = %d, want 3", got)
	}
	if got := session.GetMeshExpectedPeerCount(); got != 4 {
		t.Fatalf("mesh expected = %d, want 4", got)
	}

	store.SetSymmetricKey("p4", []byte("key-p4"))
	if !session.IsKeyExchangeComplete() {
		t.Fatal("IsKeyExchangeComplete should be true after 4/4 mesh keys")
	}

	session.ClearCeremonyScope()
	if got := session.GetExpectedPeerCount(); got != 4 {
		t.Fatalf("after clear, expected = %d, want 4", got)
	}
}

// TestAddPeerRejoinDoesNotError verifies registry rejoin unblocks ECDH AddPeer.
func TestAddPeerRejoinDoesNotError(t *testing.T) {
	store := newRejoinMockStore()
	reg := NewDynamicRegistry("self", 2, nil, store)
	rec := &recordingECDHSession{}
	reg.SetECDHSession(rec)

	pub := []byte("peer-key")
	store.RegisterPeerPublicKey("peer1", pub)

	if err := reg.AddPeer("peer1", pub); err != nil {
		t.Fatalf("first AddPeer: %v", err)
	}
	if err := reg.AddPeer("peer1", pub); err != nil {
		t.Fatalf("rejoin AddPeer must not error, got: %v", err)
	}
	if len(rec.addPeerCalls) != 2 {
		t.Fatalf("AddPeer ECDH calls = %d, want 2", len(rec.addPeerCalls))
	}
}

// TestAddPeerRejoinFlushesStaleKey verifies the ECDH session rejoin path drops
// the stale symmetric key before re-broadcasting.
func TestAddPeerRejoinFlushesStaleKey(t *testing.T) {
	store := newRejoinMockStore()
	session := NewDynamicECDHSession("self", store)
	session.InitializeWithPeers([]string{"peer1"})
	store.SetSymmetricKey("peer1", []byte("stale"))

	if err := session.AddPeer("peer1"); err != nil {
		t.Fatalf("AddPeer rejoin: %v", err)
	}
	if _, err := store.GetSymmetricKey("peer1"); err == nil {
		t.Fatal("stale symmetric key should be removed on rejoin AddPeer")
	}
}
