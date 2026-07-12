// Package main provides the AICW MPC node entry point with dynamic peer management.
//
// AICW-FORK: This is a new entry point that uses DynamicFileStore, DynamicRegistry,
// and DynamicECDHSession instead of the original static versions.
//
// Key differences from original mpcium/cmd/mpcium/main.go:
// - No peers.json dependency: peers are discovered dynamically from Consul
// - Uses DynamicFileStore: only loads self identity at startup
// - Uses DynamicRegistry: supports runtime peer additions/removals
// - Uses DynamicECDHSession: supports key exchange with dynamically joined peers
// - Integrates MembershipVerifier for eligibility checking
package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/nats-io/nats.go"
	"github.com/spf13/viper"
	"github.com/urfave/cli/v3"

	// Original mpcium packages (unchanged)
	"github.com/fystack/mpcium/pkg/config"
	"github.com/fystack/mpcium/pkg/constant"
	"github.com/fystack/mpcium/pkg/event"
	"github.com/fystack/mpcium/pkg/eventconsumer"
	"github.com/fystack/mpcium/pkg/healthcheck"
	"github.com/fystack/mpcium/pkg/infra"
	"github.com/fystack/mpcium/pkg/keyinfo"
	"github.com/fystack/mpcium/pkg/kvstore"
	"github.com/fystack/mpcium/pkg/logger"
	"github.com/fystack/mpcium/pkg/messaging"
	mpciumpc "github.com/fystack/mpcium/pkg/mpc"

	// AICW dynamic packages
	"github.com/aicw/aicw_node/pkg/committee"
	aicwconfig "github.com/aicw/aicw_node/pkg/config"
	"github.com/aicw/aicw_node/pkg/eligibility"
	"github.com/aicw/aicw_node/pkg/identity"
	"github.com/aicw/aicw_node/pkg/mpc"
	"github.com/aicw/aicw_node/pkg/nodeweb"
)

const (
	Version                    = "0.1.0-aicw"
	DefaultBackupPeriodSeconds = 300
)

func printBanner() {
	c := "\033[38;2;0;206;200m"
	d := "\033[38;2;0;140;136m"
	r := "\033[0m"

	fmt.Printf(`
%s     ___    ____________       __  ______  ______
    /   |  /  _/ ____/ |     / / / __ / / ____/
   / /| |  / // /    | | /| / / / /_/ / / /    
  / ___ |_/ // /___  | |/ |/ / / ____/ / /___  
 /_/  |_/___/\____/  |__/|__/ /_/     \____/  
%s    AICW Dynamic MPC Node  v%s
    ------------------------------------------%s

`, c, d, Version, r)
}

func main() {
	app := &cli.Command{
		Name:    "aicw-node",
		Usage:   "AICW Dynamic MPC Node for threshold signatures",
		Version: Version,
		Commands: []*cli.Command{
			{
				Name:  "init",
				Usage: "Generate node identity (UUID + Ed25519 keypair) in one step",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "name",
						Aliases:  []string{"n"},
						Usage:    "Node name (used for identity file names)",
						Required: true,
					},
					&cli.StringFlag{
						Name:    "output-dir",
						Aliases: []string{"o"},
						Usage:   "Directory for identity files",
						Value:   "identity",
					},
					&cli.BoolFlag{
						Name:    "overwrite",
						Aliases: []string{"f"},
						Usage:   "Overwrite existing identity files",
						Value:   false,
					},
				},
				Action: runInit,
			},
			{
				Name:  "start",
				Usage: "Start an AICW MPC node with dynamic peer management",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "name",
						Aliases:  []string{"n"},
						Usage:    "Node name",
						Required: true,
					},
					&cli.StringFlag{
						Name:    "network-config",
						Aliases: []string{"N"},
						Usage:   "Network-wide config (chain_code, initiator pubkey, NATS, Consul, threshold)",
					},
					&cli.StringFlag{
						Name:    "config",
						Aliases: []string{"c"},
						Usage:   "Operator-local config merged on top of network-config (or standalone legacy config.yaml)",
					},
					&cli.StringFlag{
						Name:    "identity-dir",
						Aliases: []string{"i"},
						Usage:   "Directory containing identity files",
						Value:   "identity",
					},
					&cli.StringFlag{
						Name:    "password-file",
						Aliases: []string{"f"},
						Usage:   "Path to file containing BadgerDB password",
					},
					&cli.StringFlag{
						Name:    "identity-password-file",
						Aliases: []string{"k"},
						Usage:   "Path to file containing password for decrypting .age encrypted node private key",
					},
					&cli.BoolFlag{
						Name:  "debug",
						Usage: "Enable debug logging",
						Value: false,
					},
				},
				Action: runNode,
			},
			{
				Name:  "version",
				Usage: "Display version information",
				Action: func(ctx context.Context, c *cli.Command) error {
					fmt.Printf("aicw-node (AICW) version %s\n", Version)
					return nil
				},
			},
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func runNode(ctx context.Context, c *cli.Command) error {
	nodeName := c.String("name")
	identityDir := c.String("identity-dir")
	networkConfigPath := c.String("network-config")
	configPath := c.String("config")
	passwordFile := c.String("password-file")
	agePasswordFile := c.String("identity-password-file")
	debug := c.Bool("debug")

	viper.SetDefault("backup_enabled", true)
	viper.SetDefault("node_web.ping_enabled", false)
	viper.SetDefault("node_web.ping_interval_seconds", 90)
	if networkConfigPath != "" {
		if err := aicwconfig.InitViperConfigMerged(networkConfigPath, configPath); err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
	} else {
		aicwconfig.InitViperConfigSingle(configPath)
	}
	environment := viper.GetString("environment")
	logger.Init(environment, debug)

	printBanner()

	if passwordFile != "" {
		if err := loadPasswordFromFile(passwordFile); err != nil {
			return fmt.Errorf("failed to load password from file: %w", err)
		}
	}

	appConfig := config.LoadConfig()
	checkRequiredConfigValues(appConfig)

	// === Original: Consul client (unchanged) ===
	consulClient := infra.GetConsulClient(environment)
	keyinfoStore := keyinfo.NewStore(consulClient.KV())

	// === AICW-FORK: Load self identity and generate nodeID ===
	// Unlike original, we don't load peers.json - peers are discovered from Consul
	nodeID, privateKey, err := loadSelfIdentity(identityDir, nodeName, agePasswordFile)
	if err != nil {
		logger.Fatal("Failed to load self identity", err)
	}
	logger.Info("Loaded self identity", "nodeID", nodeID, "nodeName", nodeName, "identityDir", identityDir)

	// === Original: BadgerKV (unchanged) ===
	badgerKV := newBadgerKV(nodeName, nodeID, appConfig)
	defer badgerKV.Close()

	if viper.GetBool("backup_enabled") {
		backupPeriodSeconds := viper.GetInt("backup_period_seconds")
		stopBackup := startPeriodicBackup(ctx, badgerKV, backupPeriodSeconds)
		defer stopBackup()
	}

	// === AICW-FORK: Create DynamicFileStore instead of NewFileStore ===
	// Key difference: only loads self identity, peers are loaded from Consul dynamically
	dynamicStore, err := identity.NewDynamicFileStore(identityDir, nodeName, privateKey, consulClient)
	if err != nil {
		logger.Fatal("Failed to create dynamic identity store", err)
	}

	// === AICW-FORK: Create and set MembershipVerifier ===
	membershipVerifier := createMembershipVerifier(consulClient)
	dynamicStore.SetMembershipVerifier(membershipVerifier)

	// === AICW-FORK: Register self to Consul ===
	if err := dynamicStore.SyncSelfToConsul(); err != nil {
		logger.Fatal("Failed to register self to Consul", err)
	}
	logger.Info("Registered self identity to Consul", "nodeID", nodeID)

	// === Original: NATS connection (unchanged) ===
	natsConn, err := getNATSConnection(environment, appConfig)
	if err != nil {
		logger.Fatal("Failed to connect to NATS", err)
	}

	pubsub := messaging.NewNATSPubSub(natsConn)
	directMessaging := messaging.NewNatsDirectMessaging(natsConn)

	// === Original: JetStream brokers (unchanged) ===
	maxConcurrentKeygen := viper.GetInt("max_concurrent_keygen")
	if maxConcurrentKeygen == 0 {
		maxConcurrentKeygen = eventconsumer.DefaultConcurrentKeygen
	}
	maxConcurrentSigning := viper.GetInt("max_concurrent_signing")
	if maxConcurrentSigning == 0 {
		maxConcurrentSigning = eventconsumer.DefaultConcurrentSigning
	}

	keygenBroker, err := messaging.NewJetStreamBroker(ctx, natsConn, event.KeygenBrokerStream, []string{
		event.KeygenRequestTopic,
	}, messaging.WithMaxAckPending(maxConcurrentKeygen))
	if err != nil {
		logger.Fatal("Failed to create keygen jetstream broker", err)
	}

	signingBroker, err := messaging.NewJetStreamBroker(ctx, natsConn, event.SigningPublisherStream, []string{
		event.SigningRequestTopic,
	}, messaging.WithMaxAckPending(maxConcurrentSigning))
	if err != nil {
		logger.Fatal("Failed to create signing jetstream broker", err)
	}

	// === Original: Message queues (unchanged) ===
	mqManager := messaging.NewNATsMessageQueueManager("mpc", event.ResultStreamSubjects(), natsConn)

	genKeyResultQueue := mqManager.NewMessageQueue("mpc_keygen_result", event.KeygenResultSubscriptionSubject(""))
	defer genKeyResultQueue.Close()
	singingResultQueue := mqManager.NewMessageQueue("mpc_signing_result", event.SigningResultSubscriptionSubject(""))
	defer singingResultQueue.Close()
	reshareResultQueue := mqManager.NewMessageQueue("mpc_reshare_result", event.ReshareResultSubscriptionSubject(""))
	defer reshareResultQueue.Close()

	logger.Info("Starting AICW MPC node", "version", Version, "ID", nodeID, "name", nodeName)

	// === AICW-FORK: Create DynamicRegistry instead of NewRegistry ===
	mpcThreshold := viper.GetInt("mpc_threshold")
	if mpcThreshold < 1 {
		mpcThreshold = 1
	}

	dynamicRegistry := mpc.NewDynamicRegistry(nodeID, mpcThreshold, consulClient, dynamicStore)
	dynamicRegistry.SetMembershipVerifier(membershipVerifier)

	// === AICW-FORK (P1, §13.5): committee-selection policy for keygen filter ===
	// Defaults to the tier policy in §13.8 when no committee_policy is configured.
	// The keygen party filter itself is gated by committee_policy.keygen_filter_enabled
	// (default false), so this is inert until deliberately enabled network-wide.
	if committeePolicy, err := committee.LoadPolicyFromViper(); err != nil {
		logger.Warn("Failed to load committee policy; keygen committee filter disabled", "error", err)
	} else {
		dynamicRegistry.SetCommitteePolicy(committeePolicy)
		logger.Info("Loaded committee policy",
			"version", committeePolicy.Version,
			"cap", committeePolicy.Cap,
			"keygen_filter_enabled", committee.KeygenFilterEnabled(),
		)
	}

	// AICW-FORK (§13.3/§13.8): committee-local ECDH gate wait budget. Only takes
	// effect when the committee filter is enabled; defaults to 120s.
	if secs := viper.GetInt("ecdh_gate.timeout_seconds"); secs > 0 {
		dynamicRegistry.SetECDHGateTimeout(time.Duration(secs) * time.Second)
	}

	// === AICW-FORK: Create DynamicECDHSession and connect to registry ===
	dynamicECDHSession := mpc.NewDynamicECDHSession(nodeID, dynamicStore)
	dynamicRegistry.SetECDHSession(dynamicECDHSession)

	// === AICW-FORK: Create inner ECDH session using original mpcium logic ===
	// The inner session handles the actual X25519 key exchange.
	// We get peer IDs from the store (loaded from Consul) excluding self.
	peerIDsForECDH := dynamicStore.GetAllPeerIDs()
	innerECDHSession := mpciumpc.NewECDHSession(nodeID, peerIDsForECDH, pubsub, dynamicStore)
	dynamicECDHSession.SetInnerSession(innerECDHSession)
	dynamicECDHSession.InitializeWithPeers(peerIDsForECDH) // Initialize dynamic peer list

	// === AICW-FORK: Register existing peers from store to registry ===
	// This is needed because LoadPeersFromConsul() in NewDynamicFileStore()
	// only loads peers into the store, not the registry.
	for _, peerID := range dynamicStore.GetAllPeerIDs() {
		pubKey, err := dynamicStore.GetPublicKey(peerID)
		if err != nil {
			logger.Warn("Could not get public key for existing peer", "peerID", peerID, "error", err)
			continue
		}
		if err := dynamicRegistry.AddPeer(peerID, pubKey); err != nil {
			// "peer already registered" is fine, other errors should be logged
			if !strings.Contains(err.Error(), "already registered") {
				logger.Warn("Could not add existing peer to registry", "peerID", peerID, "error", err)
			}
		} else {
			logger.Info("Registered existing peer from Consul", "peerID", peerID)
		}
	}

	// === AICW-FORK: Start watching for peer changes ===
	if err := dynamicRegistry.WatchPeerDirectory(); err != nil {
		logger.Fatal("Failed to start peer directory watch", err)
	}
	logger.Info("Started watching peer directory in Consul")

	// === Original: CKD (unchanged) ===
	chainCodeHex := viper.GetString("chain_code")
	ckd, err := mpciumpc.NewCKDFromHex(chainCodeHex)
	if err != nil {
		logger.Fatal("Failed to create ckd store", err)
	}

	// === AICW-FORK: Get current peer list for MPC node ===
	// Unlike original, this list can grow dynamically
	peerNodeIDs := dynamicRegistry.GetAllPeerIDs()

	// === Original: MPC Node creation ===
	// Note: mpc.NewNode accepts PeerRegistry interface, which DynamicRegistry implements
	mpcNode := mpciumpc.NewNode(
		nodeID,
		peerNodeIDs,
		pubsub,
		directMessaging,
		badgerKV,
		keyinfoStore,
		dynamicRegistry, // DynamicRegistry implements PeerRegistry
		dynamicStore,    // DynamicFileStore implements identity.Store
		ckd,
	)
	defer mpcNode.Close()

	// === Original: Event consumers (unchanged) ===
	eventConsumer := eventconsumer.NewEventConsumer(
		mpcNode,
		pubsub,
		genKeyResultQueue,
		singingResultQueue,
		reshareResultQueue,
		dynamicStore, // DynamicFileStore implements identity.Store
	)
	eventConsumer.Run()
	defer eventConsumer.Close()

	timeoutConsumer := eventconsumer.NewTimeOutConsumer(natsConn, singingResultQueue)
	timeoutConsumer.Run()
	defer timeoutConsumer.Close()

	// === Original: Keygen and Signing consumers ===
	// These accept PeerRegistry interface
	keygenConsumer := eventconsumer.NewKeygenConsumer(natsConn, keygenBroker, pubsub, dynamicRegistry, genKeyResultQueue)
	signingConsumer := eventconsumer.NewSigningConsumer(natsConn, signingBroker, pubsub, dynamicRegistry, singingResultQueue)

	// === Original: Mark node as ready (unchanged) ===
	if err := dynamicRegistry.Ready(); err != nil {
		logger.Error("Failed to mark peer registry as ready", err)
	}
	logger.Info("[READY] AICW Node is ready", "nodeID", nodeID)

	// === Original: Health check server (unchanged) ===
	var healthServer *healthcheck.Server
	if viper.GetBool("healthcheck.enabled") {
		healthAddr := viper.GetString("healthcheck.address")
		if healthAddr == "" {
			healthAddr = ":8080"
		}
		healthServer = healthcheck.NewServer(healthAddr, dynamicRegistry, natsConn, consulClient)
		go func() {
			if err := healthServer.Start(); err != nil {
				logger.Error("Health check server error", err)
			}
		}()
	}

	// === Original: Signal handling (unchanged) ===
	logger.Info("Starting consumers", "nodeID", nodeID)
	appContext, cancel := context.WithCancel(context.Background())

	stopNodeWebPing := nodeweb.StartPeriodicPing(appContext, nodeID, nodeweb.Config{
		Enabled:         viper.GetBool("node_web.ping_enabled"),
		BaseURL:         viper.GetString("node_web.url"),
		IntervalSeconds: viper.GetInt("node_web.ping_interval_seconds"),
	})
	defer stopNodeWebPing()

	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan
		logger.Warn("Shutdown signal received, canceling context...")
		cancel()

		if healthServer != nil {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			if err := healthServer.Shutdown(shutdownCtx); err != nil {
				logger.Error("Failed to shutdown health check server", err)
			}
		}

		if err := dynamicRegistry.Resign(); err != nil {
			logger.Error("Failed to resign from peer registry", err)
		}

		// AICW-FORK: Stop peer directory watch
		dynamicStore.StopWatch()

		if err := keygenConsumer.Close(); err != nil {
			logger.Error("Failed to close keygen consumer", err)
		}
		if err := signingConsumer.Close(); err != nil {
			logger.Error("Failed to close signing consumer", err)
		}

		err := natsConn.Drain()
		if err != nil {
			logger.Error("Failed to drain NATS connection", err)
		}
	}()

	// === Original: Run consumers (unchanged) ===
	var wg sync.WaitGroup
	errChan := make(chan error, 3)

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := keygenConsumer.Run(appContext); err != nil {
			if appContext.Err() != context.Canceled {
				logger.Error("error running keygen consumer", err)
				errChan <- fmt.Errorf("keygen consumer error: %w", err)
			} else {
				logger.Info("Keygen consumer finished successfully")
			}
			return
		}
		logger.Info("Keygen consumer finished successfully")
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := signingConsumer.Run(appContext); err != nil {
			if appContext.Err() != context.Canceled {
				logger.Error("error running signing consumer", err)
				errChan <- fmt.Errorf("signing consumer error: %w", err)
			} else {
				logger.Info("Signing consumer finished successfully")
			}
			return
		}
		logger.Info("Signing consumer finished successfully")
	}()

	go func() {
		wg.Wait()
		logger.Info("All consumers have finished")
		close(errChan)
	}()

	for err := range errChan {
		if err != nil {
			logger.Error("Consumer error received", err)
			cancel()
			return err
		}
	}

	return nil
}

// createMembershipVerifier creates the appropriate verifier based on config.
func createMembershipVerifier(consulClient *api.Client) eligibility.MembershipVerifier {
	// Check eligibility mode from config
	mode := viper.GetString("eligibility.membership.mode")
	if mode == "" {
		mode = "whitelist" // Default to whitelist for AICW
	}

	whitelistPath := viper.GetString("eligibility.membership.consul_path")
	if whitelistPath == "" {
		whitelistPath = eligibility.DefaultMembershipWhitelistPrefix
	}

	config := eligibility.MembershipVerifierConfig{
		Mode:            mode,
		WhitelistSource: "consul",
		WhitelistPath:   whitelistPath,
	}

	verifier, err := eligibility.NewMembershipVerifier(config, consulClient)
	if err != nil {
		logger.Warn("Failed to create membership verifier, using stub", "error", err)
		return eligibility.NewStakeMembershipVerifier() // Stub that allows all
	}

	return verifier
}

// loadPasswordFromFile reads the BadgerDB password from a file
func loadPasswordFromFile(filePath string) error {
	passwordBytes, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read password file %s: %w", filePath, err)
	}

	password := strings.TrimSpace(string(passwordBytes))
	if password == "" {
		return fmt.Errorf("password file %s is empty", filePath)
	}

	viper.Set("badger_password", password)
	return nil
}

// checkRequiredConfigValues validates required configuration.
func checkRequiredConfigValues(appConfig *config.AppConfig) {
	if viper.GetString("badger_password") == "" {
		logger.Fatal("Badger password is required", nil)
	}

	if viper.GetString("event_initiator_pubkey") == "" {
		logger.Fatal("Event initiator public key is required", nil)
	}

	chainCode := strings.TrimSpace(viper.GetString("chain_code"))
	if chainCode == "" {
		logger.Fatal("chain_code is required in config.yaml", nil)
	}
	if len(chainCode) != 64 {
		logger.Fatal("chain_code must be 32-byte hex (64 chars)", nil)
	}
}

// newBadgerKV creates a new BadgerDB instance.
func newBadgerKV(nodeName, nodeID string, appConfig *config.AppConfig) *kvstore.BadgerKVStore {
	basePath := viper.GetString("db_path")
	if basePath == "" {
		basePath = filepath.Join(".", "db")
	}
	dbPath := filepath.Join(basePath, nodeName)

	backupDir := viper.GetString("backup_dir")
	if backupDir == "" {
		backupDir = filepath.Join(".", "backups")
	}

	cfg := kvstore.BadgerConfig{
		NodeID:              nodeName,
		EncryptionKey:       []byte(appConfig.BadgerPassword),
		BackupEncryptionKey: []byte(appConfig.BadgerPassword),
		BackupDir:           backupDir,
		DBPath:              dbPath,
	}

	badgerKv, err := kvstore.NewBadgerKVStore(cfg)
	if err != nil {
		logger.Fatal("Failed to create badger kv store", err)
	}
	logger.Info("Connected to badger kv store", "path", dbPath, "backup_dir", backupDir)
	return badgerKv
}

// startPeriodicBackup starts a background backup job.
func startPeriodicBackup(ctx context.Context, badgerKV *kvstore.BadgerKVStore, periodSeconds int) func() {
	if periodSeconds <= 0 {
		periodSeconds = DefaultBackupPeriodSeconds
	}
	backupTicker := time.NewTicker(time.Duration(periodSeconds) * time.Second)
	backupCtx, backupCancel := context.WithCancel(ctx)
	go func() {
		for {
			select {
			case <-backupCtx.Done():
				logger.Info("Backup background job stopped")
				return
			case <-backupTicker.C:
				logger.Info("Running periodic BadgerDB backup...")
				err := badgerKV.Backup()
				if err != nil {
					logger.Error("Periodic BadgerDB backup failed", err)
				} else {
					logger.Info("Periodic BadgerDB backup completed successfully")
				}
			}
		}
	}()
	return backupCancel
}

// getNATSConnection establishes NATS connection.
func getNATSConnection(environment string, appConfig *config.AppConfig) (*nats.Conn, error) {
	url := appConfig.NATs.URL
	opts := []nats.Option{
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2 * time.Second),
		nats.ReconnectBufSize(16 * 1024 * 1024),
		nats.Dialer(&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}),
		nats.PingInterval(20 * time.Second),
		nats.MaxPingsOutstanding(3),
		nats.CustomInboxPrefix("_INBOX_mpcium_aicw"),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			if err != nil {
				logger.Warn("Disconnected from NATS", "error", err.Error())
			} else {
				logger.Warn("Disconnected from NATS")
			}
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			logger.Info("Reconnected to NATS", "url", nc.ConnectedUrl())
		}),
		nats.ClosedHandler(func(nc *nats.Conn) {
			logger.Info("NATS connection closed!")
		}),
	}

	if environment == constant.EnvProduction {
		var clientCert, clientKey, caCert string
		if appConfig.NATs.TLS != nil {
			clientCert = appConfig.NATs.TLS.ClientCert
			clientKey = appConfig.NATs.TLS.ClientKey
			caCert = appConfig.NATs.TLS.CACert
		}

		if clientCert == "" {
			clientCert = filepath.Join(".", "certs", "client-cert.pem")
		}
		if clientKey == "" {
			clientKey = filepath.Join(".", "certs", "client-key.pem")
		}
		if caCert == "" {
			caCert = filepath.Join(".", "certs", "rootCA.pem")
		}

		opts = append(opts,
			nats.ClientCert(clientCert, clientKey),
			nats.RootCAs(caCert),
			nats.UserInfo(appConfig.NATs.Username, appConfig.NATs.Password),
		)
	}

	return nats.Connect(url, opts...)
}
