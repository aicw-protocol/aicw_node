// Package recovery provides state recovery mechanisms for AICW MPC nodes.
//
// AICW-FORK: This package handles node restart and recovery scenarios.
//
// Recovery scenarios:
// 1. Node restart: Reload peers from Consul, re-establish ECDH
// 2. Network partition: Detect missing peers, wait for reconnection
// 3. Consul outage: Cache peer data locally, retry connection
package recovery

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"

	"github.com/aicw/aicw_node/pkg/identity"
)

// RecoveryManager handles node state recovery.
type RecoveryManager struct {
	mu sync.RWMutex

	nodeID        string
	identityStore identity.DynamicStore
	consulClient  *api.Client

	// Local cache for recovery
	cacheDir   string
	peerCache  map[string]PeerCacheEntry
	lastUpdate time.Time
}

// PeerCacheEntry represents cached peer information.
type PeerCacheEntry struct {
	NodeID       string `json:"node_id"`
	PublicKey    []byte `json:"public_key"`
	LastSeen     string `json:"last_seen"`
	CachedAt     string `json:"cached_at"`
}

// RecoveryConfig holds configuration for recovery manager.
type RecoveryConfig struct {
	CacheDir          string
	CacheTTLSeconds   int
	RetryIntervalSecs int
	MaxRetries        int
}

// DefaultRecoveryConfig returns default recovery configuration.
func DefaultRecoveryConfig() RecoveryConfig {
	return RecoveryConfig{
		CacheDir:          "./.mpc_cache",
		CacheTTLSeconds:   3600, // 1 hour
		RetryIntervalSecs: 5,
		MaxRetries:        12, // 1 minute total
	}
}

// NewRecoveryManager creates a new recovery manager.
func NewRecoveryManager(
	nodeID string,
	identityStore identity.DynamicStore,
	consulClient *api.Client,
	config RecoveryConfig,
) (*RecoveryManager, error) {
	// Ensure cache directory exists
	if err := os.MkdirAll(config.CacheDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	rm := &RecoveryManager{
		nodeID:        nodeID,
		identityStore: identityStore,
		consulClient:  consulClient,
		cacheDir:      config.CacheDir,
		peerCache:     make(map[string]PeerCacheEntry),
	}

	// Load existing cache
	if err := rm.loadCache(); err != nil {
		// Non-fatal: cache may not exist on first run
		fmt.Printf("Note: Could not load peer cache: %v\n", err)
	}

	return rm, nil
}

// RecoverState attempts to recover node state after restart.
func (rm *RecoveryManager) RecoverState() error {
	fmt.Println("Starting state recovery...")

	// Step 1: Try to load peers from Consul
	consulErr := rm.recoverFromConsul()
	if consulErr != nil {
		fmt.Printf("Warning: Consul recovery failed: %v\n", consulErr)

		// Step 2: Fall back to local cache
		cacheErr := rm.recoverFromCache()
		if cacheErr != nil {
			return fmt.Errorf("recovery failed: consul error: %v, cache error: %v", consulErr, cacheErr)
		}
		fmt.Println("Recovered from local cache")
	} else {
		fmt.Println("Recovered from Consul")
	}

	// Step 3: Update cache with recovered state
	if err := rm.saveCache(); err != nil {
		fmt.Printf("Warning: Could not save cache: %v\n", err)
	}

	return nil
}

// recoverFromConsul loads peer state from Consul.
func (rm *RecoveryManager) recoverFromConsul() error {
	if rm.consulClient == nil {
		return fmt.Errorf("consul client not configured")
	}

	return rm.identityStore.LoadPeersFromConsul()
}

// recoverFromCache loads peer state from local cache.
func (rm *RecoveryManager) recoverFromCache() error {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	if len(rm.peerCache) == 0 {
		return fmt.Errorf("peer cache is empty")
	}

	for nodeID, entry := range rm.peerCache {
		if nodeID == rm.nodeID {
			continue
		}

		if err := rm.identityStore.RegisterPeerPublicKey(nodeID, entry.PublicKey); err != nil {
			fmt.Printf("Warning: Could not register cached peer %s: %v\n", nodeID, err)
		}
	}

	return nil
}

// CachePeers caches current peer state for recovery.
func (rm *RecoveryManager) CachePeers() error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	peerIDs := rm.identityStore.GetAllPeerIDs()
	now := time.Now().UTC().Format(time.RFC3339)

	for _, peerID := range peerIDs {
		pubKey, err := rm.identityStore.GetPublicKey(peerID)
		if err != nil {
			continue
		}

		rm.peerCache[peerID] = PeerCacheEntry{
			NodeID:    peerID,
			PublicKey: pubKey,
			LastSeen:  now,
			CachedAt:  now,
		}
	}

	rm.lastUpdate = time.Now()
	return rm.saveCache()
}

// loadCache loads peer cache from disk.
func (rm *RecoveryManager) loadCache() error {
	cachePath := filepath.Join(rm.cacheDir, "peers.json")

	data, err := os.ReadFile(cachePath)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, &rm.peerCache)
}

// saveCache saves peer cache to disk.
func (rm *RecoveryManager) saveCache() error {
	cachePath := filepath.Join(rm.cacheDir, "peers.json")

	data, err := json.MarshalIndent(rm.peerCache, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(cachePath, data, 0600)
}

// WatchAndCache periodically caches peer state.
func (rm *RecoveryManager) WatchAndCache(interval time.Duration, stopCh <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			if err := rm.CachePeers(); err != nil {
				fmt.Printf("Warning: Failed to cache peers: %v\n", err)
			}
		}
	}
}

// HealthCheck returns the recovery manager's health status.
func (rm *RecoveryManager) HealthCheck() map[string]interface{} {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	return map[string]interface{}{
		"cached_peers":     len(rm.peerCache),
		"last_cache_update": rm.lastUpdate.Format(time.RFC3339),
		"consul_available":  rm.consulClient != nil,
	}
}

// GracefulShutdown saves state before shutdown.
func (rm *RecoveryManager) GracefulShutdown() error {
	fmt.Println("Saving state before shutdown...")
	return rm.CachePeers()
}
