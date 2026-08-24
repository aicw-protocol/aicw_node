package activity

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Event describes a notable operator activity (selection, signing, reward).
type Event struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	NodeName  string `json:"nodeName,omitempty"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

const maxEvents = 50

type Tracker struct {
	events      []Event
	seenLogKeys map[string]bool
	rewardSnap  map[string]nodeRewardSnap
}

type nodeRewardSnap struct {
	ReferralOpens int
	RewardSol     float64
	RewardToken   float64
}

func NewTracker() *Tracker {
	return &Tracker{
		seenLogKeys: map[string]bool{},
		rewardSnap:  map[string]nodeRewardSnap{},
	}
}

var logPatterns = []struct {
	re      *regexp.Regexp
	typ     string
	message string
}{
	{
		re:      regexp.MustCompile(`(?i)Creating signing session`),
		typ:     "signing_selected",
		message: "Selected for MPC signing",
	},
	{
		re:      regexp.MustCompile(`(?i)Initializing signing session`),
		typ:     "signing_started",
		message: "Signing session started",
	},
	{
		re:      regexp.MustCompile(`(?i)\[SIGN\] Sign successfully`),
		typ:     "signing_completed",
		message: "Signing completed successfully",
	},
	{
		re:      regexp.MustCompile(`(?i)Initializing.*keygen|keygen ceremony`),
		typ:     "keygen_selected",
		message: "Selected for wallet key generation",
	},
	{
		re:      regexp.MustCompile(`(?i)Reshare succeeded`),
		typ:     "reshare_completed",
		message: "Reshare ceremony completed",
	},
}

func parseNodeName(line string) string {
	if idx := strings.Index(line, "]"); idx > 0 && strings.HasPrefix(line, "[") {
		name := strings.TrimSpace(line[1:idx])
		if name != "" {
			return name
		}
	}
	return ""
}

func logKey(nodeName, line string) string {
	return nodeName + "|" + line
}

// IngestLogs scans new log lines and appends matching activity events.
func (t *Tracker) IngestLogs(lines []string) {
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		nodeName := parseNodeName(trimmed)
		key := logKey(nodeName, trimmed)
		if t.seenLogKeys[key] {
			continue
		}

		matched := false
		for _, pattern := range logPatterns {
			if !pattern.re.MatchString(trimmed) {
				continue
			}
			t.seenLogKeys[key] = true
			matched = true
			msg := pattern.message
			if nodeName != "" {
				msg = fmt.Sprintf("%s — %s", nodeName, pattern.message)
			}
			t.append(Event{
				ID:        fmt.Sprintf("log:%s", key),
				Type:      pattern.typ,
				NodeName:  nodeName,
				Message:   msg,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			})
			break
		}
		if !matched && len(t.seenLogKeys) > 5000 {
			// Bound memory if logs contain many non-matching lines.
			t.seenLogKeys[key] = true
		}
	}
}

type NodeReward struct {
	NodeID            string
	NodeName          string
	ReferralWalletOpens int
	RewardSol         float64
	RewardToken       float64
}

// IngestRewards compares node reward counters and emits events on increases.
func (t *Tracker) IngestRewards(nodes []NodeReward) {
	for _, node := range nodes {
		id := node.NodeID
		if id == "" {
			id = node.NodeName
		}
		if id == "" {
			continue
		}

		prev, ok := t.rewardSnap[id]
		curr := nodeRewardSnap{
			ReferralOpens: node.ReferralWalletOpens,
			RewardSol:     node.RewardSol,
			RewardToken:   node.RewardToken,
		}
		if !ok {
			t.rewardSnap[id] = curr
			continue
		}

		label := node.NodeName
		if label == "" {
			label = id
		}

		if curr.ReferralOpens > prev.ReferralOpens {
			delta := curr.ReferralOpens - prev.ReferralOpens
			t.append(Event{
				ID:        fmt.Sprintf("reward:open:%s:%d", id, curr.ReferralOpens),
				Type:      "wallet_referral",
				NodeName:  label,
				Message:   fmt.Sprintf("%s — selected for wallet issuance (%d time(s))", label, delta),
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			})
		}

		if curr.RewardSol > prev.RewardSol+0.0000001 {
			delta := curr.RewardSol - prev.RewardSol
			t.append(Event{
				ID:        fmt.Sprintf("reward:sol:%s:%.9f", id, curr.RewardSol),
				Type:      "reward_sol",
				NodeName:  label,
				Message:   fmt.Sprintf("%s — earned %.4f SOL (wallet issuance reward)", label, delta),
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			})
		}

		if curr.RewardToken > prev.RewardToken+0.0000001 {
			delta := curr.RewardToken - prev.RewardToken
			t.append(Event{
				ID:        fmt.Sprintf("reward:token:%s:%.9f", id, curr.RewardToken),
				Type:      "reward_token",
				NodeName:  label,
				Message:   fmt.Sprintf("%s — earned %.4f tokens (signing reward)", label, delta),
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			})
		}

		t.rewardSnap[id] = curr
	}
}

func (t *Tracker) append(event Event) {
	for _, existing := range t.events {
		if existing.ID == event.ID {
			return
		}
	}
	t.events = append([]Event{event}, t.events...)
	if len(t.events) > maxEvents {
		t.events = t.events[:maxEvents]
	}
}

// Events returns the most recent activity events (newest first).
func (t *Tracker) Events() []Event {
	out := make([]Event, len(t.events))
	copy(out, t.events)
	return out
}
