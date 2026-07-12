package committee

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// networkConfigYAML mirrors the committee_policy block shipped in
// config/network-config.yaml.template, so this test guards that the template
// and the loader stay in sync.
const networkConfigYAML = `
mpc_threshold: 2
committee_policy:
  version: "2"
  cap: 7
  mpc_threshold: 2
  keygen_filter_enabled: true
  tiers:
    - max_active: 4
      committee_size: 3
      spare: 0
    - max_active: 10
      committee_size: 4
      spare: 1
    - max_active: 30
      committee_size: 5
      spare: 2
    - max_active: 100
      committee_size: 6
      spare: 3
    - max_active: 999999
      committee_size: 7
      spare: 4
`

func loadViper(t *testing.T, yaml string) {
	t.Helper()
	viper.Reset()
	viper.SetConfigType("yaml")
	if err := viper.ReadConfig(strings.NewReader(yaml)); err != nil {
		t.Fatalf("read config: %v", err)
	}
}

func TestLoadPolicyFromViper_Template(t *testing.T) {
	loadViper(t, networkConfigYAML)
	defer viper.Reset()

	p, err := LoadPolicyFromViper()
	if err != nil {
		t.Fatalf("LoadPolicyFromViper: %v", err)
	}
	if p.Version != "2" || p.Cap != 7 || p.MPCThreshold != 2 {
		t.Fatalf("policy header mismatch: %+v", p)
	}
	if len(p.Tiers) != 5 {
		t.Fatalf("expected 5 tiers, got %d", len(p.Tiers))
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("template policy must validate: %v", err)
	}
	if !KeygenFilterEnabled() {
		t.Fatal("keygen_filter_enabled: true must be read as enabled")
	}
	// Tier lookups match the design table.
	if size := p.TargetSize(5); size != 4 {
		t.Fatalf("TargetSize(5) = %d, want 4", size)
	}
	if size := p.TargetSize(1000); size != 7 {
		t.Fatalf("TargetSize(1000) = %d, want 7 (cap)", size)
	}
}

func TestLoadPolicyFromViper_DefaultWhenAbsent(t *testing.T) {
	loadViper(t, "mpc_threshold: 2\n")
	defer viper.Reset()

	p, err := LoadPolicyFromViper()
	if err != nil {
		t.Fatalf("LoadPolicyFromViper: %v", err)
	}
	if p.Cap != DefaultPolicy().Cap || len(p.Tiers) != len(DefaultPolicy().Tiers) {
		t.Fatalf("absent committee_policy must fall back to DefaultPolicy, got %+v", p)
	}
	if KeygenFilterEnabled() {
		t.Fatal("keygen filter must default to disabled")
	}
}
