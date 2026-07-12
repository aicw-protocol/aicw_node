package committee

import (
	"fmt"

	"github.com/spf13/viper"
)

// Config keys (see auto_reshare_design.md §13.8).
const (
	// PolicyKey is the viper key holding the committee policy block.
	PolicyKey = "committee_policy"
	// KeygenFilterEnabledKey toggles the keygen committee party filter (§13.5).
	//
	// Default is FALSE: enabling it changes keygen from "all ready peers" to a
	// tier-sized committee, which is the design target but also alters live
	// ceremony behavior. It must be turned on deliberately (and consistently
	// across ALL nodes, since the committee is computed locally and must match).
	KeygenFilterEnabledKey = "committee_policy.keygen_filter_enabled"
)

// LoadPolicyFromViper builds the committee Policy from the loaded viper config.
// When no committee_policy block is present it returns DefaultPolicy(). Partial
// blocks are completed from the defaults so operators can override single keys.
func LoadPolicyFromViper() (Policy, error) {
	if !viper.IsSet(PolicyKey) {
		return DefaultPolicy(), nil
	}

	p := Policy{}
	if err := viper.UnmarshalKey(PolicyKey, &p); err != nil {
		return Policy{}, fmt.Errorf("committee: failed to unmarshal %q: %w", PolicyKey, err)
	}

	def := DefaultPolicy()
	if p.Version == "" {
		p.Version = def.Version
	}
	if p.Cap == 0 {
		p.Cap = def.Cap
	}
	if p.MPCThreshold == 0 {
		// Fall back to the network-wide mpc_threshold if set, else default.
		if viper.IsSet("mpc_threshold") {
			p.MPCThreshold = viper.GetInt("mpc_threshold")
		} else {
			p.MPCThreshold = def.MPCThreshold
		}
	}
	if len(p.Tiers) == 0 {
		p.Tiers = def.Tiers
	}

	if err := p.Validate(); err != nil {
		return Policy{}, err
	}
	return p, nil
}

// KeygenFilterEnabled reports whether the keygen committee party filter is on.
func KeygenFilterEnabled() bool {
	return viper.GetBool(KeygenFilterEnabledKey)
}
