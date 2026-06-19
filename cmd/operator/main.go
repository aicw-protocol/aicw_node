// Package main provides the operator CLI for AICW MPC network.
//
// AICW-FORK: This CLI tool manages whitelists for Phase A.
//
// SECURITY WARNING - SINGLE POINT OF FAILURE:
// This tool has write access to eligibility whitelists.
// Whoever controls this tool controls who can:
// 1. Issue MPC commands (initiator whitelist)
// 2. Join the MPC network (membership whitelist)
//
// In production, this should be:
// - Protected with strong authentication
// - Audited for all operations
// - Eventually replaced with multi-sig or on-chain governance (Phase C)
package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/hashicorp/consul/api"
)

const (
	InitiatorWhitelistPrefix   = "mpc_eligibility/initiator_whitelist/"
	MembershipWhitelistPrefix  = "mpc_eligibility/membership_whitelist/"
	PeerIdentityPrefix         = "mpc_node_identity/"
)

// WhitelistEntry represents an entry in the initiator whitelist.
type WhitelistEntry struct {
	PublicKey   string `json:"public_key"`
	Algorithm   string `json:"algorithm"`
	Description string `json:"description"`
	AddedAt     string `json:"added_at"`
	AddedBy     string `json:"added_by"`
}

// MembershipEntry represents an entry in the membership whitelist.
type MembershipEntry struct {
	NodeID       string            `json:"node_id"`
	PublicKey    string            `json:"public_key"`
	RegisteredAt string            `json:"registered_at"`
	AddedBy      string            `json:"added_by"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	client, err := api.NewClient(api.DefaultConfig())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating Consul client: %v\n", err)
		os.Exit(1)
	}

	switch os.Args[1] {
	case "add-initiator":
		if len(os.Args) < 5 {
			fmt.Println("Usage: operator add-initiator <pubkey-hex> <algorithm> <description>")
			os.Exit(1)
		}
		addInitiator(client, os.Args[2], os.Args[3], os.Args[4])

	case "remove-initiator":
		if len(os.Args) < 3 {
			fmt.Println("Usage: operator remove-initiator <pubkey-hex>")
			os.Exit(1)
		}
		removeInitiator(client, os.Args[2])

	case "list-initiators":
		listInitiators(client)

	case "add-member":
		if len(os.Args) < 4 {
			fmt.Println("Usage: operator add-member <node-id> <pubkey-hex>")
			os.Exit(1)
		}
		addMember(client, os.Args[2], os.Args[3])

	case "remove-member":
		if len(os.Args) < 3 {
			fmt.Println("Usage: operator remove-member <node-id>")
			os.Exit(1)
		}
		removeMember(client, os.Args[2])

	case "list-members":
		listMembers(client)

	case "list-nodes":
		listNodes(client)

	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`AICW MPC Operator CLI

SECURITY WARNING: This tool has write access to eligibility whitelists.
Whoever controls this tool controls who can participate in the MPC network.

Commands:
  Initiator Whitelist (who can issue MPC commands):
    add-initiator <pubkey-hex> <algorithm> <description>
    remove-initiator <pubkey-hex>
    list-initiators

  Membership Whitelist (who can join the network):
    add-member <node-id> <pubkey-hex>
    remove-member <node-id>
    list-members

  Node Status:
    list-nodes    Show all registered nodes

Environment:
  CONSUL_HTTP_ADDR    Consul address (default: localhost:8500)
  OPERATOR_ID         Operator identifier for audit (default: "operator")`)
}

func addInitiator(client *api.Client, pubKeyHex, algorithm, description string) {
	// Validate pubkey
	_, err := hex.DecodeString(pubKeyHex)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid pubkey hex: %v\n", err)
		os.Exit(1)
	}

	// Validate algorithm
	if algorithm != "ed25519" && algorithm != "p256" {
		fmt.Fprintf(os.Stderr, "Invalid algorithm: must be 'ed25519' or 'p256'\n")
		os.Exit(1)
	}

	entry := WhitelistEntry{
		PublicKey:   pubKeyHex,
		Algorithm:   algorithm,
		Description: description,
		AddedAt:     time.Now().UTC().Format(time.RFC3339),
		AddedBy:     getOperatorID(),
	}

	data, _ := json.Marshal(entry)
	key := InitiatorWhitelistPrefix + pubKeyHex

	_, err = client.KV().Put(&api.KVPair{Key: key, Value: data}, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error adding initiator: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Added initiator: %s\n", pubKeyHex[:16]+"...")
}

func removeInitiator(client *api.Client, pubKeyHex string) {
	key := InitiatorWhitelistPrefix + pubKeyHex
	_, err := client.KV().Delete(key, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error removing initiator: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Removed initiator: %s\n", pubKeyHex[:16]+"...")
}

func listInitiators(client *api.Client) {
	pairs, _, err := client.KV().List(InitiatorWhitelistPrefix, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing initiators: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Initiator Whitelist:")
	fmt.Println("--------------------")
	for _, pair := range pairs {
		var entry WhitelistEntry
		json.Unmarshal(pair.Value, &entry)
		fmt.Printf("  PubKey: %s...\n", entry.PublicKey[:16])
		fmt.Printf("  Algorithm: %s\n", entry.Algorithm)
		fmt.Printf("  Description: %s\n", entry.Description)
		fmt.Printf("  Added: %s by %s\n\n", entry.AddedAt, entry.AddedBy)
	}
	fmt.Printf("Total: %d initiators\n", len(pairs))
}

func addMember(client *api.Client, nodeID, pubKeyHex string) {
	// Validate pubkey
	_, err := hex.DecodeString(pubKeyHex)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid pubkey hex: %v\n", err)
		os.Exit(1)
	}

	entry := MembershipEntry{
		NodeID:       nodeID,
		PublicKey:    pubKeyHex,
		RegisteredAt: time.Now().UTC().Format(time.RFC3339),
		AddedBy:      getOperatorID(),
	}

	data, _ := json.Marshal(entry)
	key := MembershipWhitelistPrefix + nodeID

	_, err = client.KV().Put(&api.KVPair{Key: key, Value: data}, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error adding member: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Added member: %s\n", nodeID)
}

func removeMember(client *api.Client, nodeID string) {
	key := MembershipWhitelistPrefix + nodeID
	_, err := client.KV().Delete(key, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error removing member: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Removed member: %s\n", nodeID)
}

func listMembers(client *api.Client) {
	pairs, _, err := client.KV().List(MembershipWhitelistPrefix, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing members: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Membership Whitelist:")
	fmt.Println("---------------------")
	for _, pair := range pairs {
		var entry MembershipEntry
		json.Unmarshal(pair.Value, &entry)
		fmt.Printf("  NodeID: %s\n", entry.NodeID)
		fmt.Printf("  PubKey: %s...\n", entry.PublicKey[:16])
		fmt.Printf("  Added: %s by %s\n\n", entry.RegisteredAt, entry.AddedBy)
	}
	fmt.Printf("Total: %d members\n", len(pairs))
}

func listNodes(client *api.Client) {
	// List registered node identities
	identities, _, err := client.KV().List(PeerIdentityPrefix, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing node identities: %v\n", err)
		os.Exit(1)
	}

	// List ready nodes
	ready, _, err := client.KV().List("ready/", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing ready nodes: %v\n", err)
		os.Exit(1)
	}

	readySet := make(map[string]bool)
	for _, r := range ready {
		nodeID := r.Key[6:] // Remove "ready/" prefix
		readySet[nodeID] = true
	}

	fmt.Println("Registered Nodes:")
	fmt.Println("-----------------")
	for _, pair := range identities {
		nodeID := pair.Key[len(PeerIdentityPrefix):]
		status := "offline"
		if readySet[nodeID] {
			status = "ready"
		}
		fmt.Printf("  %s [%s]\n", nodeID, status)
	}
	fmt.Printf("\nTotal: %d registered, %d ready\n", len(identities), len(ready))
}

func getOperatorID() string {
	if id := os.Getenv("OPERATOR_ID"); id != "" {
		return id
	}
	return "operator"
}
