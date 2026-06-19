package eligibility

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/hashicorp/consul/api"
)

// WhitelistInitiatorVerifier verifies initiator messages against a whitelist
// of allowed Coordinator public keys. This is the Phase A implementation.
//
// SECURITY WARNING - SINGLE POINT OF FAILURE:
// The whitelist write permission is currently operator-only.
// Whoever controls the whitelist controls who can issue MPC commands.
// This is acceptable for testnet (Phase A) but must be decentralized
// or multi-sig protected before mainnet deployment (Phase C).
//
// Attack vector: If the operator's credentials are compromised,
// an attacker can add their own key to the whitelist and issue
// arbitrary signing commands.
type WhitelistInitiatorVerifier struct {
	mu sync.RWMutex

	// allowedKeys maps pubkey hex -> algorithm
	allowedKeys map[string]string

	// source is "consul" or "file"
	source string

	// consulClient for Consul-based whitelist
	consulClient *api.Client

	// consulPrefix is the key prefix for Consul (e.g., "mpc_initiator_whitelist/")
	consulPrefix string

	// filePath for file-based whitelist
	filePath string
}

// WhitelistEntry represents an allowed initiator in the whitelist.
type WhitelistEntry struct {
	PublicKey   string `json:"public_key"`   // hex-encoded
	Algorithm   string `json:"algorithm"`    // "ed25519" or "p256"
	Description string `json:"description"`  // human-readable description
	AddedAt     string `json:"added_at"`     // ISO8601 timestamp
	AddedBy     string `json:"added_by"`     // operator identifier
}

// NewWhitelistInitiatorVerifier creates a whitelist-based verifier.
// source: "consul" or "file"
// path: Consul prefix or file path
func NewWhitelistInitiatorVerifier(source, path string, consulClient *api.Client) (*WhitelistInitiatorVerifier, error) {
	v := &WhitelistInitiatorVerifier{
		allowedKeys:  make(map[string]string),
		source:       source,
		consulClient: consulClient,
		consulPrefix: path,
		filePath:     path,
	}

	if err := v.Refresh(); err != nil {
		return nil, fmt.Errorf("eligibility: failed to load whitelist: %w", err)
	}

	return v, nil
}

// VerifyInitiator checks if the message is signed by a whitelisted initiator.
func (v *WhitelistInitiatorVerifier) VerifyInitiator(msg InitiatorMessage) error {
	raw, err := msg.Raw()
	if err != nil {
		return fmt.Errorf("eligibility: failed to get raw message: %w", err)
	}

	sig := msg.Sig()
	if len(sig) == 0 {
		return ErrInvalidSignature
	}

	v.mu.RLock()
	defer v.mu.RUnlock()

	// Try each allowed key until one verifies
	for pubKeyHex, algorithm := range v.allowedKeys {
		pubKey, err := hex.DecodeString(pubKeyHex)
		if err != nil {
			continue
		}

		var verified bool
		switch algorithm {
		case "ed25519":
			if len(pubKey) == ed25519.PublicKeySize {
				verified = ed25519.Verify(pubKey, raw, sig)
			}
		case "p256":
			// P256 verification - would need to implement or import
			continue
		}

		if verified {
			return nil
		}
	}

	return ErrNotInWhitelist
}

// Refresh reloads the whitelist from the configured source.
func (v *WhitelistInitiatorVerifier) Refresh() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	switch v.source {
	case "consul":
		return v.loadFromConsul()
	case "file":
		return v.loadFromFile()
	default:
		return fmt.Errorf("eligibility: unknown whitelist source: %s", v.source)
	}
}

func (v *WhitelistInitiatorVerifier) loadFromConsul() error {
	if v.consulClient == nil {
		return fmt.Errorf("eligibility: consul client not configured")
	}

	kv := v.consulClient.KV()
	pairs, _, err := kv.List(v.consulPrefix, nil)
	if err != nil {
		return fmt.Errorf("eligibility: failed to list consul keys: %w", err)
	}

	newKeys := make(map[string]string)
	for _, pair := range pairs {
		var entry WhitelistEntry
		if err := json.Unmarshal(pair.Value, &entry); err != nil {
			// Try simple format: value is just the algorithm
			newKeys[pair.Key[len(v.consulPrefix):]] = string(pair.Value)
			continue
		}
		newKeys[entry.PublicKey] = entry.Algorithm
	}

	v.allowedKeys = newKeys
	return nil
}

func (v *WhitelistInitiatorVerifier) loadFromFile() error {
	data, err := os.ReadFile(v.filePath)
	if err != nil {
		return fmt.Errorf("eligibility: failed to read whitelist file: %w", err)
	}

	var entries []WhitelistEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("eligibility: failed to parse whitelist file: %w", err)
	}

	newKeys := make(map[string]string)
	for _, entry := range entries {
		newKeys[entry.PublicKey] = entry.Algorithm
	}

	v.allowedKeys = newKeys
	return nil
}

// Name returns the verifier name.
func (v *WhitelistInitiatorVerifier) Name() string {
	return "whitelist"
}

// AllowedKeyCount returns the number of allowed keys.
func (v *WhitelistInitiatorVerifier) AllowedKeyCount() int {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return len(v.allowedKeys)
}
