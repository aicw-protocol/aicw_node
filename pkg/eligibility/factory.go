package eligibility

import (
	"fmt"

	"github.com/hashicorp/consul/api"
)

// DefaultInitiatorWhitelistPrefix is the Consul key prefix for initiator whitelist.
const DefaultInitiatorWhitelistPrefix = "mpc_eligibility/initiator_whitelist/"

// NewInitiatorVerifier creates an InitiatorVerifier based on configuration.
func NewInitiatorVerifier(config InitiatorVerifierConfig, consulClient *api.Client) (InitiatorVerifier, error) {
	switch config.Mode {
	case "legacy", "":
		// Default to legacy mode for backward compatibility
		return NewLegacyInitiatorVerifier(config.LegacyPubKey, config.LegacyAlgorithm)

	case "whitelist":
		path := config.WhitelistPath
		if path == "" {
			path = DefaultInitiatorWhitelistPrefix
		}
		return NewWhitelistInitiatorVerifier(config.WhitelistSource, path, consulClient)

	case "stake":
		// Phase C stub
		return NewStakeInitiatorVerifier(), nil

	default:
		return nil, fmt.Errorf("eligibility: unknown initiator verifier mode: %s", config.Mode)
	}
}

// NewMembershipVerifier creates a MembershipVerifier based on configuration.
func NewMembershipVerifier(config MembershipVerifierConfig, consulClient *api.Client) (MembershipVerifier, error) {
	switch config.Mode {
	case "whitelist", "":
		// Default to whitelist mode
		path := config.WhitelistPath
		if path == "" {
			path = DefaultMembershipWhitelistPrefix
		}
		return NewWhitelistMembershipVerifier(config.WhitelistSource, path, consulClient)

	case "stake":
		// Phase C stub
		return NewStakeMembershipVerifier(), nil

	default:
		return nil, fmt.Errorf("eligibility: unknown membership verifier mode: %s", config.Mode)
	}
}

// CreateDefaultInitiatorConfig creates a legacy-compatible initiator config.
// This is for backward compatibility with existing Mpcium deployments.
func CreateDefaultInitiatorConfig(pubKey []byte, algorithm string) InitiatorVerifierConfig {
	return InitiatorVerifierConfig{
		Mode:            "legacy",
		LegacyPubKey:    pubKey,
		LegacyAlgorithm: algorithm,
	}
}

// CreateWhitelistInitiatorConfig creates a whitelist-based initiator config.
func CreateWhitelistInitiatorConfig(source, path string) InitiatorVerifierConfig {
	return InitiatorVerifierConfig{
		Mode:            "whitelist",
		WhitelistSource: source,
		WhitelistPath:   path,
	}
}

// CreateWhitelistMembershipConfig creates a whitelist-based membership config.
func CreateWhitelistMembershipConfig(source, path string) MembershipVerifierConfig {
	return MembershipVerifierConfig{
		Mode:            "whitelist",
		WhitelistSource: source,
		WhitelistPath:   path,
	}
}
