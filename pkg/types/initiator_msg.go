// Package types defines message types for AICW MPC network.
//
// AICW-FORK: This file extends the original Mpcium initiator_msg.go
// with support for specifying signing committee participants.
package types

import (
	"encoding/json"
	"sort"
)

type KeyType string

const (
	KeyTypeSecp256k1 KeyType = "secp256k1"
	KeyTypeEd25519   KeyType = "ed25519"
)

type EventInitiatorKeyType string

const (
	EventInitiatorKeyTypeEd25519 EventInitiatorKeyType = "ed25519"
	EventInitiatorKeyTypeP256    EventInitiatorKeyType = "p256"
)

// AuthorizerSignature represents a single authorizer signature attached to an initiator message.
type AuthorizerSignature struct {
	AuthorizerID string `json:"authorizer_id"`
	Signature    []byte `json:"signature"`
}

// InitiatorMessage is anything that carries a payload to verify and its signature.
type InitiatorMessage interface {
	// Raw returns the canonical byte-slice that was signed.
	Raw() ([]byte, error)
	// Sig returns the signature over Raw().
	Sig() []byte
	// InitiatorID returns the ID whose public key we have to look up.
	InitiatorID() string

	GetAuthorizerSignatures() []AuthorizerSignature
}

type GenerateKeyMessage struct {
	WalletID             string                `json:"wallet_id"`
	Signature            []byte                `json:"signature"`
	AuthorizerSignatures []AuthorizerSignature `json:"authorizer_signatures,omitempty"`
}

// SignTxMessage represents a transaction signing request.
//
// AICW-FORK: Added ParticipantPeerIDs field to specify the signing committee.
// This field is included in Raw() to prevent committee tampering.
type SignTxMessage struct {
	KeyType             KeyType  `json:"key_type"`
	WalletID            string   `json:"wallet_id"`
	NetworkInternalCode string   `json:"network_internal_code"`
	TxID                string   `json:"tx_id"`
	Tx                  []byte   `json:"tx"`
	DerivationPath      []uint32 `json:"derivation_path"`

	// AICW-FORK: New field for specifying signing committee.
	// If non-empty, only these peers will participate in the signing session.
	// If empty, all ready peers participate (original behavior).
	//
	// SECURITY: This field is included in Raw() and therefore signed.
	// Any tampering with the committee after signing will invalidate the message.
	ParticipantPeerIDs []string `json:"participant_peer_ids,omitempty"`

	Signature            []byte                `json:"signature"`
	AuthorizerSignatures []AuthorizerSignature `json:"authorizer_signatures,omitempty"`
}

type ResharingMessage struct {
	SessionID            string                `json:"session_id"`
	NodeIDs              []string              `json:"node_ids"` // new peer IDs
	NewThreshold         int                   `json:"new_threshold"`
	KeyType              KeyType               `json:"key_type"`
	WalletID             string                `json:"wallet_id"`
	Signature            []byte                `json:"signature,omitempty"`
	AuthorizerSignatures []AuthorizerSignature `json:"authorizer_signatures,omitempty"`
}

// Raw returns the canonical byte-slice that was signed.
//
// AICW-FORK: Now includes ParticipantPeerIDs in the signed payload.
// This prevents committee tampering - if an attacker modifies the committee
// after the Coordinator signs the message, verification will fail.
//
// SECURITY: The peer IDs are sorted before serialization to ensure
// deterministic output regardless of the order they were specified.
func (m *SignTxMessage) Raw() ([]byte, error) {
	// Sort peer IDs for deterministic serialization
	sortedPeerIDs := make([]string, len(m.ParticipantPeerIDs))
	copy(sortedPeerIDs, m.ParticipantPeerIDs)
	sort.Strings(sortedPeerIDs)

	// Include all fields that should be signed, including the committee
	payload := struct {
		KeyType             KeyType  `json:"key_type"`
		WalletID            string   `json:"wallet_id"`
		NetworkInternalCode string   `json:"network_internal_code"`
		TxID                string   `json:"tx_id"`
		Tx                  []byte   `json:"tx"`
		DerivationPath      []uint32 `json:"derivation_path,omitempty"`
		// AICW-FORK: ParticipantPeerIDs is now included in the signed payload.
		// Empty slice is serialized as null, matching original behavior when omitted.
		ParticipantPeerIDs []string `json:"participant_peer_ids,omitempty"`
	}{
		KeyType:             m.KeyType,
		WalletID:            m.WalletID,
		NetworkInternalCode: m.NetworkInternalCode,
		TxID:                m.TxID,
		Tx:                  m.Tx,
		DerivationPath:      m.DerivationPath,
		ParticipantPeerIDs:  sortedPeerIDs,
	}

	// Only include if not empty (for backward compatibility with upstream)
	if len(sortedPeerIDs) == 0 {
		payload.ParticipantPeerIDs = nil
	}

	return json.Marshal(payload)
}

func (m *SignTxMessage) Sig() []byte {
	return m.Signature
}

func (m *SignTxMessage) InitiatorID() string {
	return m.TxID
}

func (m *SignTxMessage) GetAuthorizerSignatures() []AuthorizerSignature {
	return m.AuthorizerSignatures
}

// ValidateParticipants validates the participant list.
// AICW-FORK: New method for committee validation.
func (m *SignTxMessage) ValidateParticipants(availablePeers []string, threshold int) error {
	if len(m.ParticipantPeerIDs) == 0 {
		return nil // Use all available peers (original behavior)
	}

	// Create set of available peers
	available := make(map[string]struct{})
	for _, p := range availablePeers {
		available[p] = struct{}{}
	}

	// Check all requested participants are available
	for _, p := range m.ParticipantPeerIDs {
		if _, ok := available[p]; !ok {
			return &CommitteeError{
				Code:    ErrCodePeerNotAvailable,
				Message: "requested peer not available: " + p,
			}
		}
	}

	// Check we have enough participants
	if len(m.ParticipantPeerIDs) < threshold+1 {
		return &CommitteeError{
			Code:    ErrCodeInsufficientPeers,
			Message: "insufficient participants for threshold",
		}
	}

	return nil
}

// Committee validation errors
type CommitteeError struct {
	Code    string
	Message string
}

func (e *CommitteeError) Error() string {
	return e.Message
}

const (
	ErrCodePeerNotAvailable  = "PEER_NOT_AVAILABLE"
	ErrCodeInsufficientPeers = "INSUFFICIENT_PEERS"
)

// --- Original message implementations (unchanged) ---

func (m *GenerateKeyMessage) Raw() ([]byte, error) {
	return []byte(m.WalletID), nil
}

func (m *GenerateKeyMessage) Sig() []byte {
	return m.Signature
}

func (m *GenerateKeyMessage) InitiatorID() string {
	return m.WalletID
}

func (m *GenerateKeyMessage) GetAuthorizerSignatures() []AuthorizerSignature {
	return m.AuthorizerSignatures
}

func (m *ResharingMessage) Raw() ([]byte, error) {
	copy := *m           // create a shallow copy
	copy.Signature = nil // modify only the copy
	copy.AuthorizerSignatures = nil
	return json.Marshal(&copy)
}

func (m *ResharingMessage) Sig() []byte {
	return m.Signature
}

func (m *ResharingMessage) InitiatorID() string {
	return m.WalletID
}

func (m *ResharingMessage) GetAuthorizerSignatures() []AuthorizerSignature {
	return m.AuthorizerSignatures
}

// ComposeAuthorizerRaw composes the raw data to be signed by an authorizer
func ComposeAuthorizerRaw(msg InitiatorMessage) ([]byte, error) {
	raw, err := msg.Raw()
	if err != nil {
		return nil, err
	}

	payload := struct {
		InitiatorID  string `json:"initiator_id"`
		InitiatorRaw []byte `json:"initiator_raw"`
		InitiatorSig []byte `json:"initiator_sig"`
	}{
		InitiatorID:  msg.InitiatorID(),
		InitiatorRaw: raw,
		InitiatorSig: msg.Sig(),
	}

	return json.Marshal(payload)
}
