package wallethistory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const fileName = "wallet-history.json"

type Entry struct {
	Wallet      string `json:"wallet"`
	LastLoginAt string `json:"lastLoginAt"`
}

type Store struct {
	Wallets []Entry `json:"wallets"`
}

func filePath(installDir string) string {
	return filepath.Join(installDir, fileName)
}

func Load(installDir string) ([]Entry, error) {
	raw, err := os.ReadFile(filePath(installDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var store Store
	if err := json.Unmarshal(raw, &store); err != nil {
		return nil, err
	}

	out := make([]Entry, 0, len(store.Wallets))
	for _, entry := range store.Wallets {
		wallet := strings.TrimSpace(entry.Wallet)
		if wallet == "" {
			continue
		}
		out = append(out, Entry{
			Wallet:      wallet,
			LastLoginAt: entry.LastLoginAt,
		})
	}
	return out, nil
}

func ListWallets(installDir string) ([]string, error) {
	entries, err := Load(installDir)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].LastLoginAt > entries[j].LastLoginAt
	})
	wallets := make([]string, 0, len(entries))
	for _, entry := range entries {
		wallets = append(wallets, entry.Wallet)
	}
	return wallets, nil
}

func RecordLogin(installDir, wallet string) error {
	wallet = strings.TrimSpace(wallet)
	if wallet == "" {
		return nil
	}

	entries, err := Load(installDir)
	if err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	found := false
	for i, entry := range entries {
		if entry.Wallet == wallet {
			entries[i].LastLoginAt = now
			found = true
			break
		}
	}
	if !found {
		entries = append(entries, Entry{Wallet: wallet, LastLoginAt: now})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].LastLoginAt > entries[j].LastLoginAt
	})

	raw, err := json.MarshalIndent(Store{Wallets: entries}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filePath(installDir), raw, 0o644)
}
