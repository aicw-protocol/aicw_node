package eligibility

import (
	"crypto/ed25519"
	"fmt"
)

// LegacyInitiatorVerifier provides backward compatibility with upstream Mpcium.
// It uses a single hardcoded pubkey from config (event_initiator_pubkey).
//
// AICW-FORK: This is extracted from the original identity.VerifyInitiatorMessage()
// to make the verification logic pluggable.
type LegacyInitiatorVerifier struct {
	pubKey    []byte
	algorithm string // "ed25519" or "p256"
}

// NewLegacyInitiatorVerifier creates a verifier compatible with upstream Mpcium.
func NewLegacyInitiatorVerifier(pubKey []byte, algorithm string) (*LegacyInitiatorVerifier, error) {
	if len(pubKey) == 0 {
		return nil, ErrMissingPublicKey
	}

	if algorithm != "ed25519" && algorithm != "p256" {
		return nil, fmt.Errorf("eligibility: unsupported algorithm: %s", algorithm)
	}

	return &LegacyInitiatorVerifier{
		pubKey:    pubKey,
		algorithm: algorithm,
	}, nil
}

// VerifyInitiator verifies the message using the single configured pubkey.
func (v *LegacyInitiatorVerifier) VerifyInitiator(msg InitiatorMessage) error {
	raw, err := msg.Raw()
	if err != nil {
		return fmt.Errorf("eligibility: failed to get raw message: %w", err)
	}

	sig := msg.Sig()
	if len(sig) == 0 {
		return ErrInvalidSignature
	}

	switch v.algorithm {
	case "ed25519":
		return v.verifyEd25519(raw, sig)
	case "p256":
		return v.verifyP256(raw, sig)
	default:
		return fmt.Errorf("eligibility: unsupported algorithm: %s", v.algorithm)
	}
}

func (v *LegacyInitiatorVerifier) verifyEd25519(raw, sig []byte) error {
	if len(v.pubKey) != ed25519.PublicKeySize {
		return fmt.Errorf("eligibility: invalid ed25519 public key size: %d", len(v.pubKey))
	}

	if !ed25519.Verify(v.pubKey, raw, sig) {
		return ErrInvalidSignature
	}
	return nil
}

func (v *LegacyInitiatorVerifier) verifyP256(raw, sig []byte) error {
	// P256 verification requires crypto/ecdsa and crypto/elliptic
	// For now, delegate to the original encryption package
	// This will be implemented when integrating with the full codebase
	return fmt.Errorf("eligibility: P256 verification not yet implemented in standalone mode")
}

// Name returns the verifier name.
func (v *LegacyInitiatorVerifier) Name() string {
	return "legacy"
}

// Algorithm returns the algorithm used by this verifier.
func (v *LegacyInitiatorVerifier) Algorithm() string {
	return v.algorithm
}
