package mpc

import (
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"

	"github.com/aicw/aicw_node/pkg/committee"
)

// newCommitteeModeRegistry builds a registry with committee filtering enabled
// and the given peers marked Consul-ready.
func newCommitteeModeRegistry(t *testing.T, self string, readyPeers []string) (*DynamicRegistry, *rejoinMockStore) {
	t.Helper()
	viper.Set(committee.KeygenFilterEnabledKey, true)
	t.Cleanup(func() { viper.Set(committee.KeygenFilterEnabledKey, false) })

	store := newRejoinMockStore()
	reg := NewDynamicRegistry(self, 2, nil, store)
	reg.SetCommitteePolicy(committee.DefaultPolicy())
	reg.SetECDHSession(&recordingECDHSession{})
	// Fail fast in tests: the gate must never spin for the production 120s.
	reg.SetECDHGateTimeout(2 * time.Second)

	reg.mu.Lock()
	for _, id := range readyPeers {
		reg.peerNodeIDs[id] = struct{}{}
		reg.readyMap[id] = true
	}
	reg.mu.Unlock()
	return reg, store
}

// TestEnsureCeremonyReady_NonMemberDispatcherPasses reproduces the production
// failure: the JetStream keygen consumer delivered a request to a node outside
// the wallet's committee, and the gate spun until the ECDH timeout because
// AreCeremonyReady requires selfIncluded. A non-member must pass immediately
// when every committee member is Consul-ready.
func TestEnsureCeremonyReady_NonMemberDispatcherPasses(t *testing.T) {
	reg, _ := newCommitteeModeRegistry(t, "self", []string{"c1", "c2", "c3", "c4", "c5", "x1"})

	committeeIDs := []string{"c1", "c2", "c3", "c4", "c5"} // self is NOT a member

	start := time.Now()
	if err := reg.EnsureCeremonyReady(committeeIDs); err != nil {
		t.Fatalf("non-member dispatcher gate must pass, got: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("non-member gate must return immediately, took %s", elapsed)
	}
}

// TestEnsureCeremonyReady_NonMemberFailsFastWhenCommitteeNotReady verifies the
// dispatcher still refuses to fan out when a committee member is not ready,
// and does so without waiting for the ECDH-gate timeout.
func TestEnsureCeremonyReady_NonMemberFailsFastWhenCommitteeNotReady(t *testing.T) {
	reg, _ := newCommitteeModeRegistry(t, "self", []string{"c1", "c2", "c3", "c4"})

	committeeIDs := []string{"c1", "c2", "c3", "c4", "c5"} // c5 not ready

	start := time.Now()
	err := reg.EnsureCeremonyReady(committeeIDs)
	if err == nil {
		t.Fatal("expected error when a committee member is not ready")
	}
	if !strings.Contains(err.Error(), "c5") {
		t.Fatalf("error should name the missing member, got: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("non-member gate must fail fast, took %s", elapsed)
	}
}

// TestEnsureCeremonyReady_MemberStillRequiresECDH guards the member path: a
// committee member must still hold a symmetric key with every other member.
func TestEnsureCeremonyReady_MemberStillRequiresECDH(t *testing.T) {
	reg, store := newCommitteeModeRegistry(t, "self", []string{"c1", "c2"})

	committeeIDs := []string{"self", "c1", "c2"}

	// No symmetric keys yet → must time out (2s test budget), not pass.
	if err := reg.EnsureCeremonyReady(committeeIDs); err == nil {
		t.Fatal("member gate must fail without ECDH keys")
	}

	store.SetSymmetricKey("c1", []byte("k1"))
	store.SetSymmetricKey("c2", []byte("k2"))
	if err := reg.EnsureCeremonyReady(committeeIDs); err != nil {
		t.Fatalf("member gate must pass once ECDH keys exist, got: %v", err)
	}
}
