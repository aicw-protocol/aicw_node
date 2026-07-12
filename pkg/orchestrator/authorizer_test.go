package orchestrator

import (
	"context"
	"errors"
	"testing"
)

type fakeAuthorizerSigner struct {
	id  string
	sig []byte
	err error
}

func (f *fakeAuthorizerSigner) ID() string { return f.id }
func (f *fakeAuthorizerSigner) Sign(_ context.Context, _ []byte) ([]byte, error) {
	return f.sig, f.err
}

func TestAuthorizerClientDisabled(t *testing.T) {
	var a *AuthorizerClient
	if a.Enabled() {
		t.Fatal("nil client must be disabled")
	}
	empty := NewAuthorizerClient(nil)
	if empty.Enabled() {
		t.Fatal("empty client must be disabled")
	}
	sigs, err := empty.Collect(context.Background(), []byte("raw"))
	if err != nil || sigs != nil {
		t.Fatalf("disabled Collect = %v, %v; want nil, nil", sigs, err)
	}
}

func TestAuthorizerClientCollectAll(t *testing.T) {
	a := NewAuthorizerClient([]AuthorizerSigner{
		&fakeAuthorizerSigner{id: "authorizer1", sig: []byte("sig1")},
		&fakeAuthorizerSigner{id: "authorizer2", sig: []byte("sig2")},
	})
	if !a.Enabled() {
		t.Fatal("client with signers must be enabled")
	}
	if got := a.IDs(); len(got) != 2 || got[0] != "authorizer1" || got[1] != "authorizer2" {
		t.Fatalf("IDs = %v", got)
	}
	sigs, err := a.Collect(context.Background(), []byte("raw"))
	if err != nil {
		t.Fatalf("Collect error: %v", err)
	}
	if len(sigs) != 2 {
		t.Fatalf("got %d sigs, want 2", len(sigs))
	}
	if sigs[0].AuthorizerID != "authorizer1" || string(sigs[0].Signature) != "sig1" {
		t.Fatalf("sig0 = %+v", sigs[0])
	}
	if sigs[1].AuthorizerID != "authorizer2" || string(sigs[1].Signature) != "sig2" {
		t.Fatalf("sig1 = %+v", sigs[1])
	}
}

func TestAuthorizerClientFailClosed(t *testing.T) {
	// A signer error must abort the whole collection (no partial signing).
	a := NewAuthorizerClient([]AuthorizerSigner{
		&fakeAuthorizerSigner{id: "authorizer1", sig: []byte("sig1")},
		&fakeAuthorizerSigner{id: "authorizer2", err: errors.New("hsm offline")},
	})
	if _, err := a.Collect(context.Background(), []byte("raw")); err == nil {
		t.Fatal("expected error when a signer fails")
	}

	// Empty signature is also rejected.
	b := NewAuthorizerClient([]AuthorizerSigner{
		&fakeAuthorizerSigner{id: "authorizer1", sig: nil},
	})
	if _, err := b.Collect(context.Background(), []byte("raw")); err == nil {
		t.Fatal("expected error on empty signature")
	}

	// Duplicate authorizer IDs are rejected.
	c := NewAuthorizerClient([]AuthorizerSigner{
		&fakeAuthorizerSigner{id: "dup", sig: []byte("a")},
		&fakeAuthorizerSigner{id: "dup", sig: []byte("b")},
	})
	if _, err := c.Collect(context.Background(), []byte("raw")); err == nil {
		t.Fatal("expected error on duplicate authorizer id")
	}
}

func TestNewAuthorizerClientFromSpecs(t *testing.T) {
	// No specs => disabled client, no error.
	a, err := NewAuthorizerClientFromSpecs(nil, 0)
	if err != nil {
		t.Fatalf("empty specs error: %v", err)
	}
	if a.Enabled() {
		t.Fatal("empty specs must yield disabled client")
	}

	// Missing id.
	if _, err := NewAuthorizerClientFromSpecs([]AuthorizerSpec{{Mode: "remote", URL: "https://x"}}, 0); err == nil {
		t.Fatal("expected error for missing id")
	}
	// Local without key_path.
	if _, err := NewAuthorizerClientFromSpecs([]AuthorizerSpec{{ID: "a", Mode: "local"}}, 0); err == nil {
		t.Fatal("expected error for local without key_path")
	}
	// Remote without url.
	if _, err := NewAuthorizerClientFromSpecs([]AuthorizerSpec{{ID: "a", Mode: "remote"}}, 0); err == nil {
		t.Fatal("expected error for remote without url")
	}
	// Unknown mode.
	if _, err := NewAuthorizerClientFromSpecs([]AuthorizerSpec{{ID: "a", Mode: "bogus", URL: "x"}}, 0); err == nil {
		t.Fatal("expected error for unknown mode")
	}
	// Valid remote spec builds an enabled client.
	c, err := NewAuthorizerClientFromSpecs([]AuthorizerSpec{{ID: "a", Mode: "remote", URL: "https://x/sign"}}, 0)
	if err != nil {
		t.Fatalf("valid remote spec error: %v", err)
	}
	if !c.Enabled() || len(c.IDs()) != 1 {
		t.Fatalf("expected enabled client with 1 authorizer, got %v", c.IDs())
	}
}
