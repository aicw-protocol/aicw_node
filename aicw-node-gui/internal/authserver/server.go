package authserver

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Result struct {
	Wallet              string
	Message             string
	ChallengeToken      string
	SignatureBase64     string
	SignedMessageBase64 string
}

type Server struct {
	mu                 sync.Mutex
	server             *http.Server
	result             *Result
	err                error
	successRedirectURL string
}

func allowedDashboardRedirect(raw string, webBaseURL string) bool {
	target, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") {
		return false
	}
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(webBaseURL), "/"))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return false
	}
	if target.Host != base.Host {
		return false
	}
	path := strings.TrimRight(target.Path, "/")
	return path == "/dashboard"
}

func dashboardRedirectURL(webBaseURL string) string {
	return strings.TrimRight(strings.TrimSpace(webBaseURL), "/") + "/dashboard"
}

func Start(ctx context.Context, webBaseURL string) (*Server, string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", err
	}

	s := &Server{
		successRedirectURL: dashboardRedirectURL(webBaseURL),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", s.handleCallback)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if s.successRedirectURL != "" && allowedDashboardRedirect(s.successRedirectURL, webBaseURL) {
			http.Redirect(w, r, s.successRedirectURL, http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("AICW Node sign-in received. You can close this tab and return to the app."))
	})

	s.server = &http.Server{Handler: mux}
	go func() {
		_ = s.server.Serve(listener)
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.server.Shutdown(shutdownCtx)
	}()

	return s, "http://" + listener.Addr().String() + "/callback", nil
}

func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	result := &Result{
		Wallet:              query.Get("wallet"),
		Message:             query.Get("message"),
		ChallengeToken:      query.Get("challengeToken"),
		SignatureBase64:     query.Get("signatureBase64"),
		SignedMessageBase64: query.Get("signedMessageBase64"),
	}

	if result.Wallet == "" || result.Message == "" || result.ChallengeToken == "" || result.SignatureBase64 == "" {
		http.Error(w, "missing callback parameters", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	s.result = result
	s.mu.Unlock()

	if s.successRedirectURL != "" {
		http.Redirect(w, r, s.successRedirectURL, http.StatusFound)
		return
	}

	http.Redirect(w, r, "/?ok=1", http.StatusFound)
}

func (s *Server) WaitResult(timeout time.Duration) (*Result, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		if s.result != nil {
			out := s.result
			s.mu.Unlock()
			return out, nil
		}
		if s.err != nil {
			err := s.err
			s.mu.Unlock()
			return nil, err
		}
		s.mu.Unlock()
		time.Sleep(200 * time.Millisecond)
	}
	return nil, fmt.Errorf("timed out waiting for browser sign-in")
}

func BuildCallbackURL(base, wallet, message, challengeToken, signatureBase64 string) string {
	u, _ := url.Parse(base)
	q := u.Query()
	q.Set("wallet", wallet)
	q.Set("message", message)
	q.Set("challengeToken", challengeToken)
	q.Set("signatureBase64", signatureBase64)
	u.RawQuery = q.Encode()
	return u.String()
}
