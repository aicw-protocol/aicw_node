package main

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePrivateKeyPath_prefersTxt(t *testing.T) {
	dir := t.TempDir()
	nodeName := "alice"

	txtPath := filepath.Join(dir, nodeName+"_private_key.txt")
	keyPath := filepath.Join(dir, nodeName+"_private.key")
	if err := os.WriteFile(txtPath, []byte("aa"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte("bb"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := resolvePrivateKeyPath(dir, nodeName)
	if err != nil {
		t.Fatalf("resolvePrivateKeyPath: %v", err)
	}
	if got != txtPath {
		t.Fatalf("expected %s, got %s", txtPath, got)
	}
}

func TestResolvePrivateKeyPath_fallbackToKey(t *testing.T) {
	dir := t.TempDir()
	nodeName := "bob"

	keyPath := filepath.Join(dir, nodeName+"_private.key")
	if err := os.WriteFile(keyPath, []byte("cc"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := resolvePrivateKeyPath(dir, nodeName)
	if err != nil {
		t.Fatalf("resolvePrivateKeyPath: %v", err)
	}
	if got != keyPath {
		t.Fatalf("expected %s, got %s", keyPath, got)
	}
}

func TestLoadSelfIdentity_readsPrivateKeyFromFallback(t *testing.T) {
	dir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origWD)
	})

	nodeName := "carol"
	priv := make([]byte, 32)
	for i := range priv {
		priv[i] = byte(i)
	}

	identityDir := "identity"
	if err := os.MkdirAll(identityDir, 0o750); err != nil {
		t.Fatal(err)
	}

	identityJSON := `{
  "node_name": "carol",
  "node_id": "11111111-2222-3333-4444-555555555555",
  "public_key": "deadbeef"
}`
	if err := os.WriteFile(filepath.Join(identityDir, nodeName+"_identity.json"), []byte(identityJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(identityDir, nodeName+"_private.key"), []byte(hex.EncodeToString(priv)), 0o600); err != nil {
		t.Fatal(err)
	}

	nodeID, loadedPriv, err := loadSelfIdentity(identityDir, nodeName, "")
	if err != nil {
		t.Fatalf("loadSelfIdentity: %v", err)
	}
	if nodeID != "11111111-2222-3333-4444-555555555555" {
		t.Fatalf("unexpected node_id: %s", nodeID)
	}
	if string(loadedPriv) != string(priv) {
		t.Fatalf("private key mismatch")
	}
}
