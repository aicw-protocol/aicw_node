// Command reshare-orchestrator is the AICW auto-reshare orchestrator
// (auto_reshare_design.md §2). It is a standalone, non-public background
// process that:
//
//   - scans wallet committees from Consul (§8),
//   - monitors ping (node_web) and Consul `ready/` liveness (§3.1),
//   - decides proactive/urgent reshares from the tier spare policy (§3.2),
//   - selects a deterministic committee (pkg/committee, §4), and
//   - publishes ed25519+secp256k1 reshare events via the mpcium client (§4.4),
//
// while enforcing per-wallet locks, cooldowns, and an audit log (§5–§7).
//
// It holds the event-initiator key and MUST run separately from mpc-bridge
// (§5.1). mpcium's TSS/ECDH core is not modified.
package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/spf13/viper"

	"github.com/fystack/mpcium/pkg/client"
	"github.com/fystack/mpcium/pkg/infra"
	"github.com/fystack/mpcium/pkg/logger"
	"github.com/fystack/mpcium/pkg/types"

	aicwconfig "github.com/aicw/aicw_node/pkg/config"
	"github.com/aicw/aicw_node/pkg/orchestrator"
)

const clientID = "reshare-orchestrator"

func main() {
	var (
		configPath        string
		networkConfigPath string
	)
	flag.StringVar(&configPath, "config", "", "operator config file path (merged over network config)")
	flag.StringVar(&networkConfigPath, "network-config", "", "network config file path (contains committee_policy)")
	flag.Parse()

	if networkConfigPath != "" {
		if err := aicwconfig.InitViperConfigMerged(networkConfigPath, configPath); err != nil {
			logger.Fatal("Failed to load config", err)
		}
	} else {
		aicwconfig.InitViperConfigSingle(configPath)
	}

	environment := viper.GetString("environment")
	logger.Init(environment, false)

	cfg, err := orchestrator.LoadConfigFromViper()
	if err != nil {
		logger.Fatal("Failed to load orchestrator config", err)
	}
	if cfg.NatsURL == "" {
		logger.Fatal("nats.url missing in config", nil)
	}

	// Consul (source of truth for keyinfo, ready, whitelist).
	consulClient := infra.GetConsulClient(environment)
	kv := consulClient.KV()

	// NATS + mpcium client (initiator).
	nc, err := nats.Connect(cfg.NatsURL,
		nats.Name(clientID),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
		nats.Timeout(8*time.Second),
	)
	if err != nil {
		logger.Fatal("Failed to connect to NATS", err)
	}
	defer nc.Drain()

	signer, err := loadSigner(cfg)
	if err != nil {
		logger.Fatal("Failed to load event initiator signer", err)
	}
	initiatorPub, _ := signer.PublicKey()

	mpcClient := client.NewMPCClient(client.Options{
		NatsConn: nc,
		Signer:   signer,
		ClientID: clientID,
	})

	authorizer, err := orchestrator.NewAuthorizerClientFromSpecs(cfg.Authorizers, cfg.AuthorizerTimeout)
	if err != nil {
		logger.Fatal("Failed to build authorizer client", err)
	}
	if authorizer.Enabled() {
		logger.Info("Authorizer signatures enabled for reshare (Phase 2)", "authorizers", authorizer.IDs())
	} else {
		logger.Info("Authorizer signatures disabled (Phase 1: orchestrator-only + audit)")
	}

	publisher := orchestrator.NewPublisher(mpcClient, authorizer, cfg.ReshareResultTimeout)
	if err := publisher.Start(); err != nil {
		logger.Fatal("Failed to subscribe to reshare results", err)
	}

	lock, err := orchestrator.NewLockManager(consulClient, cfg.InflightLockTTL, cfg.CooldownSuccess, cfg.CooldownFailure, cfg.CooldownFailMax)
	if err != nil {
		logger.Fatal("Failed to create Consul session lock", err)
	}
	defer lock.Close()

	orch := orchestrator.New(
		cfg,
		orchestrator.NewInventory(kv),
		orchestrator.NewLivenessProvider(kv, cfg.NodeWebURL),
		lock,
		orchestrator.NewAuditor(kv),
		publisher,
		orchestrator.NewProgressStore(kv),
		orchestrator.NewVerifier(kv),
		initiatorPub,
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	orchestrator.StartInflightSweeper(ctx, lock, cfg)

	orch.Run(ctx)
}

// loadSigner loads the event-initiator key (§5.1). Precedence: config path →
// RESHARE_ORCHESTRATOR_EVENT_INITIATOR_KEY_PATH env → ./event_initiator.key.
func loadSigner(cfg orchestrator.Config) (client.Signer, error) {
	keyType := types.EventInitiatorKeyType(cfg.EventInitiatorAlgorithm)

	keyPath := strings.TrimSpace(cfg.EventInitiatorKeyPath)
	if keyPath == "" {
		keyPath = strings.TrimSpace(os.Getenv("RESHARE_ORCHESTRATOR_EVENT_INITIATOR_KEY_PATH"))
	}
	if keyPath == "" {
		keyPath = "./event_initiator.key"
	}

	return client.NewLocalSigner(keyType, client.LocalSignerOptions{KeyPath: keyPath})
}
