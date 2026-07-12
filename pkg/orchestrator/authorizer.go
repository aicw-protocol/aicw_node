package orchestrator

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/fystack/mpcium/pkg/client"
	"github.com/fystack/mpcium/pkg/types"
)

// AuthorizerSigner produces one authorizer signature over the canonical
// authorizer bytes (types.ComposeAuthorizerRaw). Implementations may hold the
// key locally (test / small deployments) or delegate to a remote/HSM service
// (production; auto_reshare_design.md §5.3D).
type AuthorizerSigner interface {
	// ID is the authorizer identity that must match a node's
	// identity.RequiredAuthorizers / AuthorizerPublicKeys entry.
	ID() string
	// Sign returns the signature over the given authorizer raw bytes.
	Sign(ctx context.Context, authorizerRaw []byte) ([]byte, error)
}

// AuthorizerClient collects signatures from a fixed set of authorizers.
//
// mpcium's node-side verification (identity.AuthorizeInitiatorMessage) requires
// a signature from EVERY configured RequiredAuthorizers, so the client gathers
// signatures from ALL configured signers and fails closed if any is missing.
// The set here MUST cover the nodes' RequiredAuthorizers.
type AuthorizerClient struct {
	signers []AuthorizerSigner
}

// NewAuthorizerClient builds a client over the given signers.
func NewAuthorizerClient(signers []AuthorizerSigner) *AuthorizerClient {
	return &AuthorizerClient{signers: signers}
}

// NewAuthorizerClientFromSpecs builds a client from config. It returns a
// disabled (empty) client when no authorizers are configured (Phase 1).
func NewAuthorizerClientFromSpecs(specs []AuthorizerSpec, timeout time.Duration) (*AuthorizerClient, error) {
	signers := make([]AuthorizerSigner, 0, len(specs))
	for _, spec := range specs {
		if spec.ID == "" {
			return nil, fmt.Errorf("authorizer spec missing id")
		}
		switch spec.Mode {
		case "", "local":
			if spec.KeyPath == "" {
				return nil, fmt.Errorf("authorizer %q: key_path required for local mode", spec.ID)
			}
			s, err := newLocalAuthorizerSigner(spec.ID, spec.KeyPath, spec.Algorithm)
			if err != nil {
				return nil, err
			}
			signers = append(signers, s)
		case "remote":
			if spec.URL == "" {
				return nil, fmt.Errorf("authorizer %q: url required for remote mode", spec.ID)
			}
			signers = append(signers, newRemoteAuthorizerSigner(spec.ID, spec.URL, spec.Token, timeout))
		default:
			return nil, fmt.Errorf("authorizer %q: unknown mode %q (want local|remote)", spec.ID, spec.Mode)
		}
	}
	return &AuthorizerClient{signers: signers}, nil
}

// Enabled reports whether any authorizer is configured. When false the
// orchestrator publishes reshares without authorizer signatures (Phase 1).
func (a *AuthorizerClient) Enabled() bool {
	return a != nil && len(a.signers) > 0
}

// IDs returns the configured authorizer identities (for logging / audit).
func (a *AuthorizerClient) IDs() []string {
	if a == nil {
		return nil
	}
	out := make([]string, len(a.signers))
	for i, s := range a.signers {
		out[i] = s.ID()
	}
	return out
}

// Collect gathers a signature from every configured authorizer over
// authorizerRaw. It is fail-closed: any signer error aborts the whole reshare so
// a partially-signed message is never published.
func (a *AuthorizerClient) Collect(ctx context.Context, authorizerRaw []byte) ([]types.AuthorizerSignature, error) {
	if !a.Enabled() {
		return nil, nil
	}
	out := make([]types.AuthorizerSignature, 0, len(a.signers))
	seen := make(map[string]bool, len(a.signers))
	for _, s := range a.signers {
		id := s.ID()
		if seen[id] {
			return nil, fmt.Errorf("duplicate authorizer id %q", id)
		}
		seen[id] = true

		sig, err := s.Sign(ctx, authorizerRaw)
		if err != nil {
			return nil, fmt.Errorf("authorizer %q sign: %w", id, err)
		}
		if len(sig) == 0 {
			return nil, fmt.Errorf("authorizer %q returned empty signature", id)
		}
		out = append(out, types.AuthorizerSignature{AuthorizerID: id, Signature: sig})
	}
	return out, nil
}

// localAuthorizerSigner signs with a key held on the orchestrator host. This is
// convenient for test networks but weakens the trust model (orchestrator
// compromise => authorizer compromise); prefer remoteAuthorizerSigner in prod.
type localAuthorizerSigner struct {
	id     string
	signer client.Signer
}

func (l *localAuthorizerSigner) ID() string { return l.id }

func (l *localAuthorizerSigner) Sign(_ context.Context, authorizerRaw []byte) ([]byte, error) {
	return l.signer.Sign(authorizerRaw)
}

// newLocalAuthorizerSigner loads an authorizer key file. Authorizer keys are
// ed25519 by convention (mpcium examples/authorizers), matching the node's
// AuthorizerPublicKeys algorithm.
func newLocalAuthorizerSigner(id, keyPath, algorithm string) (AuthorizerSigner, error) {
	keyType := types.EventInitiatorKeyType(algorithm)
	if algorithm == "" {
		keyType = types.EventInitiatorKeyTypeEd25519
	}
	s, err := client.NewLocalSigner(keyType, client.LocalSignerOptions{KeyPath: keyPath})
	if err != nil {
		return nil, fmt.Errorf("load local authorizer %q key %q: %w", id, keyPath, err)
	}
	return &localAuthorizerSigner{id: id, signer: s}, nil
}

// remoteAuthorizerSigner delegates signing to an HTTP endpoint that owns the
// authorizer key (offline service / HSM front, §5.3D). The endpoint receives the
// base64 authorizer raw bytes and returns the base64 signature.
type remoteAuthorizerSigner struct {
	id     string
	url    string
	token  string
	client *http.Client
}

func (r *remoteAuthorizerSigner) ID() string { return r.id }

type remoteSignRequest struct {
	AuthorizerID string `json:"authorizer_id"`
	Raw          string `json:"raw"` // base64(std)
}

type remoteSignResponse struct {
	Signature string `json:"signature"` // base64(std)
	Error     string `json:"error,omitempty"`
}

func (r *remoteAuthorizerSigner) Sign(ctx context.Context, authorizerRaw []byte) ([]byte, error) {
	body, err := json.Marshal(remoteSignRequest{
		AuthorizerID: r.id,
		Raw:          base64.StdEncoding.EncodeToString(authorizerRaw),
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var out remoteSignResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode response (status %d): %w", resp.StatusCode, err)
	}
	if resp.StatusCode != http.StatusOK {
		if out.Error != "" {
			return nil, fmt.Errorf("remote authorizer status %d: %s", resp.StatusCode, out.Error)
		}
		return nil, fmt.Errorf("remote authorizer status %d", resp.StatusCode)
	}
	sig, err := base64.StdEncoding.DecodeString(out.Signature)
	if err != nil {
		return nil, fmt.Errorf("decode signature: %w", err)
	}
	return sig, nil
}

func newRemoteAuthorizerSigner(id, url, token string, timeout time.Duration) AuthorizerSigner {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &remoteAuthorizerSigner{
		id:     id,
		url:    url,
		token:  token,
		client: &http.Client{Timeout: timeout},
	}
}
