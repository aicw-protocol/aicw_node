// Package nodeweb reports node liveness to the AICW node web registry.
// This is a standalone HTTP sidecar — it does not touch TSS, ECDH, or trust logic.
package nodeweb

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/fystack/mpcium/pkg/logger"
)

const (
	defaultPingIntervalSeconds = 90
	defaultHTTPTimeout         = 10 * time.Second
	pingPath                   = "/api/nodes/ping"
)

// Config controls periodic pings to the node web API.
type Config struct {
	Enabled         bool
	BaseURL         string
	IntervalSeconds int
}

type pingRequest struct {
	NodeID          string `json:"nodeId"`
	Timestamp       string `json:"timestamp"`
	SignatureBase64 string `json:"signatureBase64"`
}

func buildNodePingMessage(nodeID, timestamp string) string {
	return fmt.Sprintf("AICW Node Ping\nNode ID: %s\nTimestamp: %s", nodeID, timestamp)
}

// PingEndpoint builds the full ping URL from a configured base URL.
func PingEndpoint(baseURL string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") + pingPath
}

// SendPing posts a signed ping for nodeID. Errors are returned to the caller.
func SendPing(ctx context.Context, client *http.Client, baseURL, nodeID string, privateKey ed25519.PrivateKey) error {
	if strings.TrimSpace(baseURL) == "" {
		return fmt.Errorf("node web base URL is empty")
	}
	if strings.TrimSpace(nodeID) == "" {
		return fmt.Errorf("node ID is empty")
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		return fmt.Errorf("invalid node private key for ping signing")
	}

	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	message := buildNodePingMessage(nodeID, timestamp)
	signature := ed25519.Sign(privateKey, []byte(message))

	body, err := json.Marshal(pingRequest{
		NodeID:          nodeID,
		Timestamp:       timestamp,
		SignatureBase64: base64.StdEncoding.EncodeToString(signature),
	})
	if err != nil {
		return fmt.Errorf("marshal ping body: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		PingEndpoint(baseURL),
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("create ping request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("ping request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("ping returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}

	return nil
}

// StartPeriodicPing runs ping in the background until ctx is cancelled.
// Returns a stop function (no-op when disabled). Ping failures are logged only.
func StartPeriodicPing(ctx context.Context, nodeID string, privateKey ed25519.PrivateKey, cfg Config) func() {
	if !cfg.Enabled {
		return func() {}
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		logger.Warn("node_web.ping_enabled is true but node_web.url is empty; ping disabled")
		return func() {}
	}
	if strings.TrimSpace(nodeID) == "" {
		logger.Warn("node web ping disabled: node ID is empty")
		return func() {}
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		logger.Warn("node web ping disabled: invalid private key for signing")
		return func() {}
	}

	intervalSeconds := cfg.IntervalSeconds
	if intervalSeconds <= 0 {
		intervalSeconds = defaultPingIntervalSeconds
	}

	client := &http.Client{Timeout: defaultHTTPTimeout}
	endpoint := PingEndpoint(cfg.BaseURL)
	ticker := time.NewTicker(time.Duration(intervalSeconds) * time.Second)
	pingCtx, cancel := context.WithCancel(ctx)

	logger.Info(
		"Node web ping enabled",
		"nodeID", nodeID,
		"endpoint", endpoint,
		"intervalSeconds", intervalSeconds,
	)

	send := func() {
		if err := SendPing(pingCtx, client, cfg.BaseURL, nodeID, privateKey); err != nil {
			logger.Warn("Node web ping failed", "nodeID", nodeID, "error", err)
			return
		}
		logger.Debug("Node web ping succeeded", "nodeID", nodeID)
	}

	go func() {
		send()
		for {
			select {
			case <-pingCtx.Done():
				logger.Info("Node web ping stopped", "nodeID", nodeID)
				return
			case <-ticker.C:
				send()
			}
		}
	}()

	return func() {
		cancel()
		ticker.Stop()
	}
}
