package nodeidentity

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var nodeNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{1,63}$`)

type Identity struct {
	NodeName  string `json:"node_name"`
	NodeID    string `json:"node_id"`
	PublicKey string `json:"public_key"`
	CreatedAt string `json:"created_at"`
}

type Generated struct {
	NodeName            string
	NodeID              string
	PublicKey           string
	CreatedAt           string
	IdentityJSON        string
	PrivateKeyHex       string
	IdentityFilename    string
	PrivateKeyFilename  string
}

func ValidateNodeName(nodeName string) error {
	trimmed := strings.TrimSpace(nodeName)
	if trimmed == "" {
		return fmt.Errorf("node name is required")
	}
	if strings.ContainsAny(trimmed, `/\`) {
		return fmt.Errorf("node name must not contain path separators")
	}
	if !nodeNamePattern.MatchString(trimmed) {
		return fmt.Errorf("node name must be 2–64 characters (letters, numbers, ., _, -)")
	}
	return nil
}

func Generate(nodeName string) (*Generated, error) {
	trimmed := strings.TrimSpace(nodeName)
	if err := ValidateNodeName(trimmed); err != nil {
		return nil, err
	}
	nodeName = trimmed

	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate keypair: %w", err)
	}

	nodeID := uuid.NewString()
	createdAt := time.Now().UTC().Format(time.RFC3339)
	identity := Identity{
		NodeName:  nodeName,
		NodeID:    nodeID,
		PublicKey: hex.EncodeToString(pubKey),
		CreatedAt: createdAt,
	}

	identityBytes, err := json.MarshalIndent(identity, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal identity: %w", err)
	}

	return &Generated{
		NodeName:           nodeName,
		NodeID:             nodeID,
		PublicKey:          identity.PublicKey,
		CreatedAt:          createdAt,
		IdentityJSON:       string(identityBytes) + "\n",
		PrivateKeyHex:      hex.EncodeToString(privKey),
		IdentityFilename:   nodeName + "_identity.json",
		PrivateKeyFilename: nodeName + "_private_key.txt",
	}, nil
}

func WriteFiles(installDir string, generated *Generated) error {
	if generated == nil {
		return fmt.Errorf("identity is required")
	}
	identityDir := filepath.Join(installDir, "identity")
	if err := os.MkdirAll(identityDir, 0o755); err != nil {
		return err
	}

	identityPath := filepath.Join(identityDir, generated.IdentityFilename)
	if _, err := os.Stat(identityPath); err == nil {
		return fmt.Errorf("identity file already exists for %s", generated.NodeName)
	}

	privateKeyPath := filepath.Join(identityDir, generated.PrivateKeyFilename)
	if _, err := os.Stat(privateKeyPath); err == nil {
		return fmt.Errorf("private key file already exists for %s", generated.NodeName)
	}

	if err := os.WriteFile(identityPath, []byte(generated.IdentityJSON), 0o600); err != nil {
		return err
	}
	return os.WriteFile(privateKeyPath, []byte(generated.PrivateKeyHex), 0o600)
}
