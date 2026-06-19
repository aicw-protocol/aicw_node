package eligibility

// StakeInitiatorVerifier is a stub for Phase C staking-based initiator verification.
// In Phase C, initiators must prove they have sufficient stake to issue MPC commands.
//
// NOT IMPLEMENTED - This is a placeholder for Phase C.
// Current implementation returns ErrNotImplemented for all operations.
type StakeInitiatorVerifier struct{}

// NewStakeInitiatorVerifier creates a stake-based verifier stub.
func NewStakeInitiatorVerifier() *StakeInitiatorVerifier {
	return &StakeInitiatorVerifier{}
}

// VerifyInitiator is not implemented in Phase A.
func (v *StakeInitiatorVerifier) VerifyInitiator(msg InitiatorMessage) error {
	return ErrNotImplemented
}

// Name returns the verifier name.
func (v *StakeInitiatorVerifier) Name() string {
	return "stake"
}

// StakeMembershipVerifier is a stub for Phase C staking-based membership verification.
// In Phase C, nodes must prove they have sufficient stake to join the MPC network.
//
// NOT IMPLEMENTED - This is a placeholder for Phase C.
// Current implementation returns ErrNotImplemented for all operations.
//
// Phase C Design Notes:
// - Nodes must stake tokens on the AICW chain before joining
// - Staking proof is verified via light client or oracle
// - Slashing is applied for misbehavior (failed to sign, double signing, etc.)
// - This removes the single point of failure present in whitelist-based verification
type StakeMembershipVerifier struct{}

// NewStakeMembershipVerifier creates a stake-based membership verifier stub.
func NewStakeMembershipVerifier() *StakeMembershipVerifier {
	return &StakeMembershipVerifier{}
}

// VerifyMembership is not implemented in Phase A.
func (v *StakeMembershipVerifier) VerifyMembership(nodeID string, pubKey []byte, metadata map[string]string) error {
	return ErrNotImplemented
}

// Refresh is not implemented in Phase A.
func (v *StakeMembershipVerifier) Refresh() error {
	return ErrNotImplemented
}

// Name returns the verifier name.
func (v *StakeMembershipVerifier) Name() string {
	return "stake"
}
