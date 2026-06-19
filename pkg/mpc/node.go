// Package mpc provides MPC node functionality for AICW network.
//
// AICW-FORK: This file extends the original Mpcium node.go with support
// for specifying signing committee participants via SignTxMessage.
package mpc

import (
	"fmt"
	"slices"

	"github.com/aicw/aicw_node/pkg/eligibility"
	"github.com/aicw/aicw_node/pkg/identity"
	"github.com/aicw/aicw_node/pkg/types"
)

const (
	PurposeKeygen  string = "keygen"
	PurposeSign    string = "sign"
	PurposeReshare string = "reshare"
)

// DynamicNode extends Node with dynamic peer management.
//
// AICW-FORK: This is a new implementation that supports:
// - Dynamic peer additions/removals
// - Committee specification in signing requests
// - Pluggable eligibility verification
type DynamicNode struct {
	nodeID string

	// Dependencies
	identityStore      identity.DynamicStore
	registry           DynamicPeerRegistry
	initiatorVerifier  eligibility.InitiatorVerifier
	membershipVerifier eligibility.MembershipVerifier

	// Configuration
	mpcThreshold int
}

// NewDynamicNode creates a new dynamic MPC node.
func NewDynamicNode(
	nodeID string,
	mpcThreshold int,
	identityStore identity.DynamicStore,
	registry DynamicPeerRegistry,
) *DynamicNode {
	return &DynamicNode{
		nodeID:        nodeID,
		mpcThreshold:  mpcThreshold,
		identityStore: identityStore,
		registry:      registry,
	}
}

// SetInitiatorVerifier sets the verifier for MPC command authorization.
func (n *DynamicNode) SetInitiatorVerifier(verifier eligibility.InitiatorVerifier) {
	n.initiatorVerifier = verifier
}

// SetMembershipVerifier sets the verifier for peer membership.
func (n *DynamicNode) SetMembershipVerifier(verifier eligibility.MembershipVerifier) {
	n.membershipVerifier = verifier
	n.registry.SetMembershipVerifier(verifier)
}

// ValidateSigningRequest validates a signing request before creating a session.
//
// AICW-FORK: This validates:
// 1. The initiator signature (if verifier is set)
// 2. The committee specification (if provided)
// 3. Sufficient ready peers for threshold
func (n *DynamicNode) ValidateSigningRequest(msg *types.SignTxMessage) error {
	// 1. Verify initiator (optional in Phase A whitelist mode)
	if n.initiatorVerifier != nil {
		if err := n.initiatorVerifier.VerifyInitiator(msg); err != nil {
			return fmt.Errorf("initiator verification failed: %w", err)
		}
	}

	// 2. Get available peers
	readyPeers := n.registry.GetReadyPeersIncludeSelf()

	// 3. Validate committee if specified
	if err := msg.ValidateParticipants(readyPeers, n.mpcThreshold); err != nil {
		return fmt.Errorf("committee validation failed: %w", err)
	}

	return nil
}

// GetSigningParticipants returns the participants for a signing session.
//
// AICW-FORK: If SignTxMessage.ParticipantPeerIDs is specified, only those
// peers are included. Otherwise, all ready peers participate (original behavior).
//
// The committee is filtered against both:
// 1. Currently ready peers
// 2. Original key participants (for keys created before dynamic membership)
func (n *DynamicNode) GetSigningParticipants(
	msg *types.SignTxMessage,
	keyParticipants []string,
) ([]string, error) {
	readyPeers := n.registry.GetReadyPeersIncludeSelf()

	// If committee is specified in the message, use it
	if len(msg.ParticipantPeerIDs) > 0 {
		return n.filterParticipants(msg.ParticipantPeerIDs, readyPeers, keyParticipants)
	}

	// Otherwise, use all ready peers that are also key participants
	return n.filterParticipants(keyParticipants, readyPeers, keyParticipants)
}

// filterParticipants filters the requested participants against available peers.
func (n *DynamicNode) filterParticipants(
	requested []string,
	readyPeers []string,
	keyParticipants []string,
) ([]string, error) {
	result := make([]string, 0, len(requested))

	for _, peerID := range requested {
		// Must be ready
		if !slices.Contains(readyPeers, peerID) {
			continue
		}

		// Must be a key participant (for backward compatibility)
		if len(keyParticipants) > 0 && !slices.Contains(keyParticipants, peerID) {
			continue
		}

		result = append(result, peerID)
	}

	// Check threshold
	if len(result) < n.mpcThreshold+1 {
		return nil, &types.CommitteeError{
			Code:    types.ErrCodeInsufficientPeers,
			Message: fmt.Sprintf("insufficient participants: got %d, need %d", len(result), n.mpcThreshold+1),
		}
	}

	// Ensure self is included
	if !slices.Contains(result, n.nodeID) {
		return nil, &types.CommitteeError{
			Code:    types.ErrCodePeerNotAvailable,
			Message: "self node is not in the participant list",
		}
	}

	return result, nil
}

// NodeID returns this node's ID.
func (n *DynamicNode) NodeID() string {
	return n.nodeID
}

// MPCThreshold returns the MPC threshold.
func (n *DynamicNode) MPCThreshold() int {
	return n.mpcThreshold
}

// IsReady returns true if enough peers are ready for MPC operations.
func (n *DynamicNode) IsReady() bool {
	return n.registry.AreMajorityReady()
}

// GetReadyPeerCount returns the number of ready peers including self.
func (n *DynamicNode) GetReadyPeerCount() int64 {
	return n.registry.GetReadyPeersCount()
}
