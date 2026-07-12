package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/consul/api"

	"github.com/aicw/aicw_node/pkg/eligibility"
)

const readyPrefix = "ready/"

// LivenessProvider builds a network liveness Snapshot (§3.1).
type LivenessProvider struct {
	kv         *api.KV
	nodeWebURL string
	httpClient *http.Client
}

// NewLivenessProvider creates a liveness provider. When nodeWebURL is empty the
// provider degrades gracefully: Consul `ready/` is used as the ping signal.
func NewLivenessProvider(kv *api.KV, nodeWebURL string) *LivenessProvider {
	return &LivenessProvider{
		kv:         kv,
		nodeWebURL: strings.TrimRight(strings.TrimSpace(nodeWebURL), "/"),
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// Snapshot reads ready, whitelist, and ping-active sets.
func (l *LivenessProvider) Snapshot(ctx context.Context) (Snapshot, error) {
	ready, err := l.readSet(readyPrefix)
	if err != nil {
		return Snapshot{}, fmt.Errorf("liveness: ready: %w", err)
	}
	whitelist, err := l.readSet(eligibility.DefaultMembershipWhitelistPrefix)
	if err != nil {
		return Snapshot{}, fmt.Errorf("liveness: whitelist: %w", err)
	}

	ping := ready // degraded default: ready acts as the ping proxy.
	if l.nodeWebURL != "" {
		active, perr := l.pingActive(ctx)
		if perr != nil {
			// Ping is an early-warning signal only (§3.1); on error fall back to
			// ready so pre-flight (which requires ready) still gates correctly.
			fmt.Printf("warning: liveness: node_web active query failed (%v); using ready as ping proxy\n", perr)
		} else {
			ping = active
		}
	}

	return Snapshot{Ready: ready, Whitelist: whitelist, PingActive: ping}, nil
}

// readSet lists a Consul prefix and returns the set of trailing node IDs.
func (l *LivenessProvider) readSet(prefix string) (map[string]bool, error) {
	pairs, _, err := l.kv.List(prefix, nil)
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(pairs))
	for _, p := range pairs {
		id := strings.TrimPrefix(p.Key, prefix)
		id = strings.Trim(id, "/")
		if id != "" {
			set[id] = true
		}
	}
	return set, nil
}

// pingActive queries the node_web bulk active-node endpoint (§9.2):
//
//	GET {base}/api/nodes/active  ->  {"active":["<nodeId>", ...]}  (or a bare array)
func (l *LivenessProvider) pingActive(ctx context.Context) (map[string]bool, error) {
	url := l.nodeWebURL + "/api/nodes/active"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := l.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}

	// Accept either {"active":[...]} or a bare ["..."] array.
	var wrapped struct {
		Active []string `json:"active"`
	}
	if err := json.Unmarshal(body, &wrapped); err == nil && wrapped.Active != nil {
		return toSet(wrapped.Active), nil
	}
	var bare []string
	if err := json.Unmarshal(body, &bare); err == nil {
		return toSet(bare), nil
	}
	return nil, fmt.Errorf("unrecognized node_web active response")
}

func toSet(ids []string) map[string]bool {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id != "" {
			set[id] = true
		}
	}
	return set
}
