package config

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	mpciumconfig "github.com/fystack/mpcium/pkg/config"
	"github.com/spf13/viper"
)

// InitViperConfigMerged loads network-wide defaults first, then merges operator-local
// overrides on top. Operator values win on conflict. If operatorPath is empty, only
// networkPath is loaded (still valid when badger_password comes from -f password.txt).
func InitViperConfigMerged(networkPath, operatorPath string) error {
	if networkPath == "" {
		return fmt.Errorf("network config path is required")
	}

	viper.Reset()
	viper.SetConfigType("yaml")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	viper.SetConfigFile(networkPath)
	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("read network config %q: %w", networkPath, err)
	}
	log.Printf("Reading network config: %s", networkPath)

	if operatorPath != "" {
		viper.SetConfigFile(operatorPath)
		if err := viper.MergeInConfig(); err != nil {
			return fmt.Errorf("merge operator config %q: %w", operatorPath, err)
		}
		log.Printf("Merged operator config: %s", operatorPath)
	}

	NormalizeLegacyPingConfig()
	log.Println("Initialized config successfully!")
	return nil
}

// InitViperConfigSingle loads a single config file (backward compatible with --config only).
func InitViperConfigSingle(configPath string) {
	mpciumconfig.InitViperConfig(configPath)
	NormalizeLegacyPingConfig()
}

// NormalizeLegacyPingConfig maps older onboarding YAML (`dashboard_ping`) to `node_web`
// and strips a trailing /api/nodes/ping path so ping URLs resolve correctly.
func NormalizeLegacyPingConfig() {
	hasLegacy := viper.IsSet("dashboard_ping.enabled") ||
		viper.IsSet("dashboard_ping.url") ||
		viper.IsSet("dashboard_ping.interval")
	if !hasLegacy {
		return
	}

	if viper.IsSet("dashboard_ping.enabled") {
		viper.Set("node_web.ping_enabled", viper.GetBool("dashboard_ping.enabled"))
	}

	if legacyURL := strings.TrimSpace(viper.GetString("dashboard_ping.url")); legacyURL != "" {
		viper.Set("node_web.url", NormalizeNodeWebBaseURL(legacyURL))
	}

	if interval := strings.TrimSpace(viper.GetString("dashboard_ping.interval")); interval != "" {
		if seconds := ParseIntervalSeconds(interval); seconds > 0 {
			viper.Set("node_web.ping_interval_seconds", seconds)
		}
	}
}

// NormalizeNodeWebBaseURL returns the node web base URL without a ping path suffix.
func NormalizeNodeWebBaseURL(raw string) string {
	url := strings.TrimRight(strings.TrimSpace(raw), "/")
	url = strings.TrimSuffix(url, "/api/nodes/ping")
	return strings.TrimRight(url, "/")
}

// ParseIntervalSeconds parses values like "90", "90s", or "2m".
func ParseIntervalSeconds(raw string) int {
	value := strings.TrimSpace(strings.ToLower(raw))
	if value == "" {
		return 0
	}

	switch {
	case strings.HasSuffix(value, "s"):
		seconds, err := strconv.Atoi(strings.TrimSuffix(value, "s"))
		if err == nil && seconds > 0 {
			return seconds
		}
	case strings.HasSuffix(value, "m"):
		minutes, err := strconv.Atoi(strings.TrimSuffix(value, "m"))
		if err == nil && minutes > 0 {
			return minutes * 60
		}
	default:
		seconds, err := strconv.Atoi(value)
		if err == nil && seconds > 0 {
			return seconds
		}
	}

	return 0
}
