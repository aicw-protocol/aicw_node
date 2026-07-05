package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// resolvePrivateKeyPath returns the path to a node's plaintext private key file.
// Prefer {name}_private_key.txt (aicw convention); fall back to {name}_private.key
// (mpcium-cli convention) when the txt file is absent.
func resolvePrivateKeyPath(dir, nodeName string) (string, error) {
	txtPath := filepath.Join(dir, nodeName+"_private_key.txt")
	if _, err := os.Stat(txtPath); err == nil {
		return txtPath, nil
	}
	keyPath := filepath.Join(dir, nodeName+"_private.key")
	if _, err := os.Stat(keyPath); err == nil {
		return keyPath, nil
	}
	return "", fmt.Errorf("private key not found (tried %s and %s)", txtPath, keyPath)
}

// resolveEncryptedPrivateKeyPath mirrors resolvePrivateKeyPath for age-encrypted keys.
func resolveEncryptedPrivateKeyPath(dir, nodeName string) (string, error) {
	txtAge := filepath.Join(dir, nodeName+"_private_key.txt.age")
	if _, err := os.Stat(txtAge); err == nil {
		return txtAge, nil
	}
	keyAge := filepath.Join(dir, nodeName+"_private.key.age")
	if _, err := os.Stat(keyAge); err == nil {
		return keyAge, nil
	}
	return "", fmt.Errorf("encrypted private key not found (tried %s and %s)", txtAge, keyAge)
}

// loadSelfIdentity loads the node's identity file and returns nodeID and private key.
// Unlike original mpcium, this doesn't require peers.json - nodeID comes from identity file.
func loadSelfIdentity(identityDir, nodeName, agePasswordFile string) (string, []byte, error) {
	if identityDir == "" {
		identityDir = "identity"
	}
	identityPath := filepath.Join(identityDir, nodeName+"_identity.json")

	if _, err := os.Stat(identityPath); os.IsNotExist(err) {
		return "", nil, fmt.Errorf("identity file not found: %s", identityPath)
	}

	data, err := os.ReadFile(identityPath)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read identity file: %w", err)
	}

	var identityData struct {
		NodeID    string `json:"node_id"`
		NodeName  string `json:"node_name"`
		PublicKey string `json:"public_key"`
	}
	if err := parseJSON(data, &identityData); err != nil {
		return "", nil, fmt.Errorf("failed to parse identity file: %w", err)
	}

	nodeID := identityData.NodeID
	if nodeID == "" {
		return "", nil, fmt.Errorf("node_id is empty in identity file")
	}

	if encryptedPath, err := resolveEncryptedPrivateKeyPath(identityDir, nodeName); err == nil {
		_ = encryptedPath
		if agePasswordFile == "" {
			return "", nil, fmt.Errorf("encrypted private key found but no password file provided")
		}
		return "", nil, fmt.Errorf("encrypted private key not yet supported in AICW node")
	}

	privateKeyPath, err := resolvePrivateKeyPath(identityDir, nodeName)
	if err != nil {
		return "", nil, err
	}

	privateKeyHex, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read private key file: %w", err)
	}

	privateKey, err := hex.DecodeString(strings.TrimSpace(string(privateKeyHex)))
	if err != nil {
		return "", nil, fmt.Errorf("failed to decode private key: %w", err)
	}

	return nodeID, privateKey, nil
}

// parseJSON is a simple JSON parser
func parseJSON(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}
