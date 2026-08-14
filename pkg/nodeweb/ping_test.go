package nodeweb

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPingEndpoint(t *testing.T) {
	got := PingEndpoint("https://node.aicw.ai/")
	want := "https://node.aicw.ai/api/nodes/ping"
	if got != want {
		t.Fatalf("PingEndpoint() = %q, want %q", got, want)
	}
}

func TestSendPingSuccess(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	_ = pub

	var got pingRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != pingPath {
			t.Fatalf("path = %s, want %s", r.URL.Path, pingPath)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("unmarshal body: %v", err)
		}

		message := buildNodePingMessage(got.NodeID, got.Timestamp)
		sig, err := base64.StdEncoding.DecodeString(got.SignatureBase64)
		if err != nil {
			t.Fatalf("decode signature: %v", err)
		}
		if !ed25519.Verify(pub, []byte(message), sig) {
			t.Fatal("ping signature verification failed")
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()

	if err := SendPing(context.Background(), server.Client(), server.URL, "test-node-01", priv); err != nil {
		t.Fatalf("SendPing() error = %v", err)
	}
	if got.NodeID != "test-node-01" {
		t.Fatalf("nodeId = %q, want test-node-01", got.NodeID)
	}
	if got.Timestamp == "" || got.SignatureBase64 == "" {
		t.Fatalf("expected signed ping payload, got %#v", got)
	}
}

func TestSendPingHTTPError(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	err = SendPing(context.Background(), server.Client(), server.URL, "missing-node", priv)
	if err == nil {
		t.Fatal("SendPing() expected error for HTTP 404")
	}
}

func TestStartPeriodicPingDisabled(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	stop := StartPeriodicPing(context.Background(), "node-1", priv, Config{Enabled: false, BaseURL: "http://example.com"})
	stop()
}

func TestBuildNodePingMessage(t *testing.T) {
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	got := buildNodePingMessage("node-123", ts)
	want := "AICW Node Ping\nNode ID: node-123\nTimestamp: " + ts
	if got != want {
		t.Fatalf("buildNodePingMessage() = %q, want %q", got, want)
	}
}
