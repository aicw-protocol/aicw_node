// Package eligibility provides pluggable verification for MPC command authorization
// and network membership. This is part of AICW's fork of Mpcium.
//
// AICW-FORK: This entire package is new and does not exist in upstream Mpcium.
// It enables dynamic node participation without modifying TSS core logic.
package eligibility

import "errors"

// Common errors for eligibility verification
var (
	ErrVerificationFailed   = errors.New("eligibility: verification failed")
	ErrNotInWhitelist       = errors.New("eligibility: not in whitelist")
	ErrInvalidSignature     = errors.New("eligibility: invalid signature")
	ErrMissingPublicKey     = errors.New("eligibility: missing public key")
	ErrNotImplemented       = errors.New("eligibility: not implemented (Phase C stub)")
)

// InitiatorMessage mirrors the interface from pkg/types for decoupling.
// The actual implementation will use types.InitiatorMessage.
type InitiatorMessage interface {
	Raw() ([]byte, error)
	Sig() []byte
	InitiatorID() string
}

// InitiatorVerifier verifies whether an entity is authorized to issue MPC commands
// (keygen, sign, reshare). This replaces the hardcoded event_initiator_pubkey check.
//
// Phase A: WhitelistInitiatorVerifier (allowed Coordinator pubkeys from config/Consul)
// Phase C: StakeInitiatorVerifier (staking-based verification)
type InitiatorVerifier interface {
	// VerifyInitiator checks if the message is signed by an authorized initiator.
	// Returns nil if verification passes, error otherwise.
	VerifyInitiator(msg InitiatorMessage) error

	// Name returns the verifier implementation name for logging/config.
	Name() string
}

// InitiatorVerifierConfig holds configuration for initiator verification.
type InitiatorVerifierConfig struct {
	// Mode selects the verification mode: "legacy", "whitelist", "stake"
	Mode string

	// LegacyPubKey is the single pubkey for legacy mode (upstream compatible)
	LegacyPubKey []byte

	// LegacyAlgorithm is "ed25519" or "p256" for legacy mode
	LegacyAlgorithm string

	// WhitelistSource is the source for whitelist mode: "consul" or "file"
	WhitelistSource string

	// WhitelistPath is the Consul key prefix or file path for whitelist
	WhitelistPath string
}
