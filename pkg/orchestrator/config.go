// Package orchestrator implements the AICW auto-reshare orchestrator
// (auto_reshare_design.md §2–§8): a background process that monitors wallet
// committees, detects when a committee has lost its availability spare or is
// at risk of losing quorum, and publishes deterministic reshare events to the
// MPC network.
//
// The orchestrator is a SEPARATE process from mpc-bridge (§5.1): it holds the
// event-initiator key and runs a long-lived reconcile loop, whereas the Bridge
// only serves user-facing keygen/sign requests. mpcium's TSS/ECDH core is not
// modified — the orchestrator only publishes `mpc:reshare` events via the
// existing mpcium client.
package orchestrator

import (
	"os"
	"time"

	"github.com/spf13/viper"

	"github.com/aicw/aicw_node/pkg/committee"
)

// AuthorizerSpec configures one authorizer the orchestrator must collect a
// reshare signature from (auto_reshare_design.md §5.3D). ID must match a node's
// identity.RequiredAuthorizers / AuthorizerPublicKeys entry.
type AuthorizerSpec struct {
	ID   string `mapstructure:"id"`
	Mode string `mapstructure:"mode"` // "local" | "remote" (default local)

	// local mode.
	KeyPath   string `mapstructure:"key_path"`
	Algorithm string `mapstructure:"algorithm"` // default ed25519

	// remote mode.
	URL      string `mapstructure:"url"`
	Token    string `mapstructure:"token"`
	TokenEnv string `mapstructure:"token_env"` // read token from this env var if set
}

// Config holds orchestrator runtime parameters (auto_reshare_design.md §9.3).
type Config struct {
	Environment string

	// Reconcile loop (§6.4).
	ScanInterval time.Duration

	// ECDH gate (§13.3) — how long to wait for committee-local ECDH before
	// giving up and re-selecting / skipping.
	ECDHGateTimeout time.Duration

	// Cooldowns and concurrency (§6.2).
	CooldownSuccess   time.Duration
	CooldownFailure   time.Duration
	CooldownFailMax   time.Duration
	GlobalMaxInflight int

	// Timeouts (§7.3).
	ReshareResultTimeout time.Duration // per key type
	InflightLockTTL      time.Duration

	// Inflight stale sweeper — optional; off by default (hardening-tasks-composer §1).
	SweepEnabled  bool
	SweepInterval time.Duration

	// Hysteresis (§3.3).
	ConfirmDeadScans int

	// MigrateOversized enables automatic legacy-wallet migration (§13.6): a
	// wallet whose committee is larger than the current policy target is
	// proactively resharded down to the tier size. Off by default to avoid a
	// reshare storm on first deploy; when on it is still rate-limited by the
	// per-wallet cooldown and global_max_inflight.
	MigrateOversized bool

	// node_web (§9.2) — optional bulk active-node endpoint base URL. When empty
	// the orchestrator runs in a degraded mode where Consul `ready/` is used as
	// the ping signal (ping_active := ready).
	NodeWebURL string

	// Initiator key (§5.1).
	EventInitiatorKeyPath   string
	EventInitiatorAlgorithm string

	// NATS.
	NatsURL string

	// Committee policy — SSOT shared with keygen (§4.2/§13.8).
	Policy committee.Policy

	// Authorizers (§5.3D) — when non-empty the orchestrator collects a
	// signature from every listed authorizer and attaches them to each reshare
	// event. Must cover the nodes' identity.RequiredAuthorizers or nodes reject
	// the reshare. Empty => Phase 1 (orchestrator-only + audit).
	Authorizers       []AuthorizerSpec
	AuthorizerTimeout time.Duration
}

// Defaults mirror the recommended parameters in auto_reshare_design.md.
func defaultConfig() Config {
	return Config{
		ScanInterval:            60 * time.Second,
		ECDHGateTimeout:         120 * time.Second,
		CooldownSuccess:         30 * time.Minute,
		CooldownFailure:         5 * time.Minute,
		CooldownFailMax:         60 * time.Minute,
		GlobalMaxInflight:       3,
		ReshareResultTimeout:    10 * time.Minute,
		InflightLockTTL:         15 * time.Minute,
		SweepInterval:           300 * time.Second,
		ConfirmDeadScans:        2,
		EventInitiatorAlgorithm: "ed25519",
	}
}

// LoadConfigFromViper builds the orchestrator config from the loaded viper
// configuration, filling any unset value from the design defaults.
func LoadConfigFromViper() (Config, error) {
	c := defaultConfig()
	c.Environment = viper.GetString("environment")

	if v := viper.GetInt("orchestrator.scan_interval_seconds"); v > 0 {
		c.ScanInterval = time.Duration(v) * time.Second
	}
	if v := viper.GetInt("orchestrator.ecdh_gate.timeout_seconds"); v > 0 {
		c.ECDHGateTimeout = time.Duration(v) * time.Second
	}
	if v := viper.GetInt("orchestrator.cooldown_success_minutes"); v > 0 {
		c.CooldownSuccess = time.Duration(v) * time.Minute
	}
	if v := viper.GetInt("orchestrator.cooldown_failure_minutes"); v > 0 {
		c.CooldownFailure = time.Duration(v) * time.Minute
	}
	if v := viper.GetInt("orchestrator.cooldown_failure_max_minutes"); v > 0 {
		c.CooldownFailMax = time.Duration(v) * time.Minute
	}
	if v := viper.GetInt("orchestrator.global_max_inflight"); v > 0 {
		c.GlobalMaxInflight = v
	}
	if v := viper.GetInt("orchestrator.reshare_result_timeout_minutes"); v > 0 {
		c.ReshareResultTimeout = time.Duration(v) * time.Minute
	}
	if v := viper.GetInt("orchestrator.inflight_lock_ttl_minutes"); v > 0 {
		c.InflightLockTTL = time.Duration(v) * time.Minute
	}
	c.SweepEnabled = viper.GetBool("orchestrator.inflight_sweep_enabled")
	if v := viper.GetInt("orchestrator.inflight_sweep_interval_seconds"); v > 0 {
		c.SweepInterval = time.Duration(v) * time.Second
	}
	if v := viper.GetInt("orchestrator.confirm_dead_scans"); v > 0 {
		c.ConfirmDeadScans = v
	}
	c.MigrateOversized = viper.GetBool("orchestrator.migrate_oversized")

	c.NodeWebURL = viper.GetString("orchestrator.node_web_url")
	if c.NodeWebURL == "" {
		c.NodeWebURL = viper.GetString("node_web.url")
	}

	c.EventInitiatorKeyPath = viper.GetString("orchestrator.event_initiator_key_path")
	if a := viper.GetString("event_initiator_algorithm"); a != "" {
		c.EventInitiatorAlgorithm = a
	}
	c.NatsURL = viper.GetString("nats.url")

	pol, err := committee.LoadPolicyFromViper()
	if err != nil {
		return Config{}, err
	}
	c.Policy = pol

	c.AuthorizerTimeout = 10 * time.Second
	if v := viper.GetInt("orchestrator.authorizers.request_timeout_seconds"); v > 0 {
		c.AuthorizerTimeout = time.Duration(v) * time.Second
	}
	var specs []AuthorizerSpec
	if err := viper.UnmarshalKey("orchestrator.authorizers.list", &specs); err != nil {
		return Config{}, err
	}
	for i := range specs {
		if specs[i].Token == "" && specs[i].TokenEnv != "" {
			specs[i].Token = os.Getenv(specs[i].TokenEnv)
		}
	}
	c.Authorizers = specs

	return c, nil
}
