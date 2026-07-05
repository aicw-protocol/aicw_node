package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// GenerateOptions controls one-shot node identity creation (aicw-node init).
type GenerateOptions struct {
	NodeName   string
	OutputDir  string
	Overwrite  bool
}

// GenerateResult holds paths and values shown to the operator after init.
type GenerateResult struct {
	NodeName   string
	NodeID     string
	PublicKey  string
	IdentityPath string
	PrivateKeyPath string
}

// GenerateNodeIdentity creates a node identity using the same algorithm and on-disk
// format as mpcium-cli generate-identity (Ed25519 keypair, hex-encoded 64-byte
// private key, identity JSON fields). The only difference is node_id comes from a
// freshly generated UUID instead of peers.json, and the private key is written as
// {name}_private_key.txt (the filename aicw-node reads by default).
func GenerateNodeIdentity(opts GenerateOptions) (*GenerateResult, error) {
	if opts.NodeName == "" {
		return nil, fmt.Errorf("node name is required")
	}
	outputDir := opts.OutputDir
	if outputDir == "" {
		outputDir = "identity"
	}

	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create identity directory: %w", err)
	}

	identityPath := filepath.Join(outputDir, opts.NodeName+"_identity.json")
	privateKeyPath := filepath.Join(outputDir, opts.NodeName+"_private_key.txt")

	if !opts.Overwrite {
		if _, err := os.Stat(identityPath); err == nil {
			return nil, fmt.Errorf("identity file %s already exists (use --overwrite)", identityPath)
		}
		if _, err := os.Stat(privateKeyPath); err == nil {
			return nil, fmt.Errorf("private key file %s already exists (use --overwrite)", privateKeyPath)
		}
	}

	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate keypair: %w", err)
	}

	nodeID := uuid.NewString()
	identity := NodeIdentity{
		NodeName:  opts.NodeName,
		NodeID:    nodeID,
		PublicKey: hex.EncodeToString(pubKey),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	identityBytes, err := json.MarshalIndent(identity, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal identity: %w", err)
	}
	if err := os.WriteFile(identityPath, identityBytes, 0o600); err != nil {
		return nil, fmt.Errorf("failed to write identity JSON: %w", err)
	}

	privateKeyHex := hex.EncodeToString(privKey)
	if err := os.WriteFile(privateKeyPath, []byte(privateKeyHex), 0o600); err != nil {
		return nil, fmt.Errorf("failed to write private key: %w", err)
	}

	return &GenerateResult{
		NodeName:       opts.NodeName,
		NodeID:         nodeID,
		PublicKey:      identity.PublicKey,
		IdentityPath:   identityPath,
		PrivateKeyPath: privateKeyPath,
	}, nil
}
