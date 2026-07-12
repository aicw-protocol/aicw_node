package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

// orchestratorConfigYAML mirrors config/orchestrator-config.yaml.template merged
// with the shared network committee_policy, exercising the same viper path the
// reshare-orchestrator binary uses.
const orchestratorConfigYAML = `
environment: production
event_initiator_algorithm: ed25519
nats:
  url: "nats://10.0.0.1:4222"
consul:
  address: "10.0.0.2:8500"
mpc_threshold: 2
committee_policy:
  version: "2"
  cap: 7
  mpc_threshold: 2
  tiers:
    - max_active: 4
      committee_size: 3
      spare: 0
    - max_active: 999999
      committee_size: 7
      spare: 4
orchestrator:
  scan_interval_seconds: 45
  ecdh_gate:
    timeout_seconds: 90
  cooldown_success_minutes: 20
  cooldown_failure_minutes: 3
  cooldown_failure_max_minutes: 40
  global_max_inflight: 5
  reshare_result_timeout_minutes: 8
  inflight_lock_ttl_minutes: 12
  confirm_dead_scans: 3
  migrate_oversized: true
  event_initiator_key_path: "/secrets/reshare_initiator.key"
  node_web_url: "https://nodes.example.com"
  authorizers:
    request_timeout_seconds: 7
    list:
      - id: authorizer1
        mode: local
        key_path: /secrets/authorizer1.authorizer.key
        algorithm: ed25519
      - id: authorizer2
        mode: remote
        url: https://authorizer2.internal/sign
        token_env: TEST_AUTHZ2_TOKEN
node_web:
  url: "https://fallback.example.com"
`

func TestLoadConfigFromViper_Full(t *testing.T) {
	viper.Reset()
	viper.SetConfigType("yaml")
	if err := viper.ReadConfig(strings.NewReader(orchestratorConfigYAML)); err != nil {
		t.Fatalf("read config: %v", err)
	}
	defer viper.Reset()

	c, err := LoadConfigFromViper()
	if err != nil {
		t.Fatalf("LoadConfigFromViper: %v", err)
	}

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"Environment", c.Environment, "production"},
		{"ScanInterval", c.ScanInterval, 45 * time.Second},
		{"ECDHGateTimeout", c.ECDHGateTimeout, 90 * time.Second},
		{"CooldownSuccess", c.CooldownSuccess, 20 * time.Minute},
		{"CooldownFailure", c.CooldownFailure, 3 * time.Minute},
		{"CooldownFailMax", c.CooldownFailMax, 40 * time.Minute},
		{"GlobalMaxInflight", c.GlobalMaxInflight, 5},
		{"ReshareResultTimeout", c.ReshareResultTimeout, 8 * time.Minute},
		{"InflightLockTTL", c.InflightLockTTL, 12 * time.Minute},
		{"ConfirmDeadScans", c.ConfirmDeadScans, 3},
		{"MigrateOversized", c.MigrateOversized, true},
		{"EventInitiatorKeyPath", c.EventInitiatorKeyPath, "/secrets/reshare_initiator.key"},
		{"EventInitiatorAlgorithm", c.EventInitiatorAlgorithm, "ed25519"},
		{"NatsURL", c.NatsURL, "nats://10.0.0.1:4222"},
		{"NodeWebURL (orchestrator override wins)", c.NodeWebURL, "https://nodes.example.com"},
	}
	for _, ch := range checks {
		if ch.got != ch.want {
			t.Errorf("%s = %v, want %v", ch.name, ch.got, ch.want)
		}
	}

	if err := c.Policy.Validate(); err != nil {
		t.Fatalf("loaded policy must validate: %v", err)
	}
	if c.Policy.Cap != 7 {
		t.Errorf("Policy.Cap = %d, want 7", c.Policy.Cap)
	}

	if c.AuthorizerTimeout != 7*time.Second {
		t.Errorf("AuthorizerTimeout = %v, want 7s", c.AuthorizerTimeout)
	}
	if len(c.Authorizers) != 2 {
		t.Fatalf("Authorizers = %d, want 2", len(c.Authorizers))
	}
	if c.Authorizers[0].ID != "authorizer1" || c.Authorizers[0].Mode != "local" ||
		c.Authorizers[0].KeyPath != "/secrets/authorizer1.authorizer.key" {
		t.Errorf("authorizer[0] = %+v", c.Authorizers[0])
	}
	if c.Authorizers[1].ID != "authorizer2" || c.Authorizers[1].Mode != "remote" ||
		c.Authorizers[1].URL != "https://authorizer2.internal/sign" {
		t.Errorf("authorizer[1] = %+v", c.Authorizers[1])
	}
}

func TestLoadConfigFromViper_AuthorizerTokenEnv(t *testing.T) {
	yaml := `
nats:
  url: "nats://x:4222"
orchestrator:
  authorizers:
    list:
      - id: authorizer2
        mode: remote
        url: https://authorizer2.internal/sign
        token_env: TEST_AUTHZ2_TOKEN
`
	t.Setenv("TEST_AUTHZ2_TOKEN", "secret-token")
	viper.Reset()
	viper.SetConfigType("yaml")
	if err := viper.ReadConfig(strings.NewReader(yaml)); err != nil {
		t.Fatalf("read config: %v", err)
	}
	defer viper.Reset()

	c, err := LoadConfigFromViper()
	if err != nil {
		t.Fatalf("LoadConfigFromViper: %v", err)
	}
	if len(c.Authorizers) != 1 {
		t.Fatalf("Authorizers = %d, want 1", len(c.Authorizers))
	}
	if c.Authorizers[0].Token != "secret-token" {
		t.Errorf("token_env not resolved: Token = %q, want secret-token", c.Authorizers[0].Token)
	}
	// Default timeout applies when unset.
	if c.AuthorizerTimeout != 10*time.Second {
		t.Errorf("default AuthorizerTimeout = %v, want 10s", c.AuthorizerTimeout)
	}
}

func TestLoadConfigFromViper_NodeWebFallback(t *testing.T) {
	yaml := `
nats:
  url: "nats://x:4222"
node_web:
  url: "https://fallback.example.com"
`
	viper.Reset()
	viper.SetConfigType("yaml")
	if err := viper.ReadConfig(strings.NewReader(yaml)); err != nil {
		t.Fatalf("read config: %v", err)
	}
	defer viper.Reset()

	c, err := LoadConfigFromViper()
	if err != nil {
		t.Fatalf("LoadConfigFromViper: %v", err)
	}
	// No orchestrator.node_web_url -> falls back to node_web.url.
	if c.NodeWebURL != "https://fallback.example.com" {
		t.Errorf("NodeWebURL fallback = %q, want fallback.example.com", c.NodeWebURL)
	}
	// Unset knobs keep design defaults.
	if c.ScanInterval != 60*time.Second {
		t.Errorf("default ScanInterval = %v, want 60s", c.ScanInterval)
	}
	if c.GlobalMaxInflight != 3 {
		t.Errorf("default GlobalMaxInflight = %d, want 3", c.GlobalMaxInflight)
	}
}
