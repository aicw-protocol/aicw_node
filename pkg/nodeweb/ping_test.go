package nodeweb

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPingEndpoint(t *testing.T) {
	got := PingEndpoint("https://node.aicw.ai/")
	want := "https://node.aicw.ai/api/nodes/ping"
	if got != want {
		t.Fatalf("PingEndpoint() = %q, want %q", got, want)
	}
}

func TestSendPingSuccess(t *testing.T) {
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
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()

	if err := SendPing(context.Background(), server.Client(), server.URL, "test-node-01"); err != nil {
		t.Fatalf("SendPing() error = %v", err)
	}
	if got.NodeID != "test-node-01" {
		t.Fatalf("nodeId = %q, want test-node-01", got.NodeID)
	}
}

func TestSendPingHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	err := SendPing(context.Background(), server.Client(), server.URL, "missing-node")
	if err == nil {
		t.Fatal("SendPing() expected error for HTTP 404")
	}
}

func TestStartPeriodicPingDisabled(t *testing.T) {
	stop := StartPeriodicPing(context.Background(), "node-1", Config{Enabled: false, BaseURL: "http://example.com"})
	stop()
}
