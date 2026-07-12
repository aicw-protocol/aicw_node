package config

import (
	"path/filepath"
	"testing"

	"github.com/spf13/viper"

	"github.com/aicw/aicw_node/pkg/committee"
)

// repoConfig resolves a path under the repo-root config/ directory from this
// package (pkg/config).
func repoConfig(name string) string {
	return filepath.Join("..", "..", "config", name)
}

// TestNetworkConfigTemplateLoads guarantees the shipped network-config template
// parses through the real node loader and that its committee_policy block is
// valid and matches the design tier table (auto_reshare_design.md §13.8).
func TestNetworkConfigTemplateLoads(t *testing.T) {
	if err := InitViperConfigMerged(repoConfig("network-config.yaml.template"), ""); err != nil {
		t.Fatalf("load network-config template: %v", err)
	}
	defer viper.Reset()

	p, err := committee.LoadPolicyFromViper()
	if err != nil {
		t.Fatalf("committee policy from template: %v", err)
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("template committee policy invalid: %v", err)
	}
	// Template ships the feature OFF for a safe default rollout.
	if committee.KeygenFilterEnabled() {
		t.Fatal("network-config template must ship keygen_filter_enabled: false")
	}
	if got := viper.GetInt("ecdh_gate.timeout_seconds"); got != 120 {
		t.Fatalf("ecdh_gate.timeout_seconds = %d, want 120", got)
	}
}

// TestOrchestratorConfigTemplateLoads guarantees the orchestrator template,
// merged over the shared network-config, parses and yields a valid config.
func TestOrchestratorConfigTemplateLoads(t *testing.T) {
	if err := InitViperConfigMerged(
		repoConfig("network-config.yaml.template"),
		repoConfig("orchestrator-config.yaml.template"),
	); err != nil {
		t.Fatalf("load orchestrator template: %v", err)
	}
	defer viper.Reset()

	if got := viper.GetInt("orchestrator.scan_interval_seconds"); got != 60 {
		t.Fatalf("orchestrator.scan_interval_seconds = %d, want 60", got)
	}
	if got := viper.GetInt("orchestrator.global_max_inflight"); got != 3 {
		t.Fatalf("orchestrator.global_max_inflight = %d, want 3", got)
	}
	if viper.GetBool("orchestrator.migrate_oversized") {
		t.Fatal("orchestrator template must ship migrate_oversized: false")
	}
	// committee_policy is inherited from the network-config layer.
	p, err := committee.LoadPolicyFromViper()
	if err != nil || p.Validate() != nil {
		t.Fatalf("merged committee policy invalid: %v", err)
	}
}
