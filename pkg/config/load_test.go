package config

import (
	"testing"

	"github.com/spf13/viper"
)

func TestNormalizeNodeWebBaseURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"http://localhost:4003/api/nodes/ping", "http://localhost:4003"},
		{"https://node.aicw.ai/api/nodes/ping/", "https://node.aicw.ai"},
		{"http://localhost:4003/", "http://localhost:4003"},
	}

	for _, tc := range tests {
		if got := NormalizeNodeWebBaseURL(tc.in); got != tc.want {
			t.Fatalf("NormalizeNodeWebBaseURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseIntervalSeconds(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"90s", 90},
		{"90", 90},
		{"2m", 120},
		{"", 0},
	}

	for _, tc := range tests {
		if got := ParseIntervalSeconds(tc.in); got != tc.want {
			t.Fatalf("ParseIntervalSeconds(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeLegacyPingConfig(t *testing.T) {
	viper.Reset()
	viper.Set("dashboard_ping.enabled", true)
	viper.Set("dashboard_ping.url", "http://localhost:4003/api/nodes/ping")
	viper.Set("dashboard_ping.interval", "90s")

	NormalizeLegacyPingConfig()

	if !viper.GetBool("node_web.ping_enabled") {
		t.Fatal("expected node_web.ping_enabled=true")
	}
	if got := viper.GetString("node_web.url"); got != "http://localhost:4003" {
		t.Fatalf("node_web.url = %q", got)
	}
	if got := viper.GetInt("node_web.ping_interval_seconds"); got != 90 {
		t.Fatalf("node_web.ping_interval_seconds = %d", got)
	}
}
