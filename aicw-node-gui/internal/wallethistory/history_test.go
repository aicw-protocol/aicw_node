package wallethistory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRecordLoginAndList(t *testing.T) {
	dir := t.TempDir()

	if err := RecordLogin(dir, "walletA"); err != nil {
		t.Fatalf("record A: %v", err)
	}
	if err := RecordLogin(dir, "walletB"); err != nil {
		t.Fatalf("record B: %v", err)
	}
	if err := RecordLogin(dir, "walletA"); err != nil {
		t.Fatalf("record A again: %v", err)
	}

	wallets, err := ListWallets(dir)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(wallets) != 2 {
		t.Fatalf("expected 2 wallets, got %v", wallets)
	}
	if wallets[0] != "walletA" {
		t.Fatalf("expected walletA first, got %v", wallets)
	}

	if _, err := os.Stat(filepath.Join(dir, fileName)); err != nil {
		t.Fatalf("history file missing: %v", err)
	}
}
