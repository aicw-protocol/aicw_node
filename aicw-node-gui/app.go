package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aicw/aicw_node/aicw-node-gui/internal/authserver"
	"github.com/aicw/aicw_node/aicw-node-gui/internal/config"
	"github.com/aicw/aicw_node/aicw-node-gui/internal/install"
	"github.com/aicw/aicw_node/aicw-node-gui/internal/nodeidentity"
	"github.com/aicw/aicw_node/aicw-node-gui/internal/nodeprocess"
	"github.com/aicw/aicw_node/aicw-node-gui/internal/nodeweb"
	"github.com/aicw/aicw_node/aicw-node-gui/internal/setupfiles"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const guiVersion = "0.1.26-gui"

type Session struct {
	Wallet    string `json:"wallet"`
	Verified  bool   `json:"verified"`
	LoginAt   string `json:"loginAt"`
}

type App struct {
	ctx context.Context

	mu          sync.Mutex
	exePath     string
	installDir  string
	installState *install.State
	session     *Session
	webClient   *nodeweb.Client
	nodeProc    *nodeprocess.Manager

	licenseAccepted bool
	installScope    string
	closePromptOpen bool
	quitting        bool

	registerMu       sync.Mutex
	registerActive   bool
	registerPhase    string
	registerJobName  string
	registerResult   *RegisterNodeResult
}

func NewApp() *App {
	return &App{
		installDir:   config.DefaultLocalAppDataInstallDir(),
		installState: &install.State{},
		webClient:    nodeweb.NewClient(config.WebBaseURL()),
		nodeProc:     nodeprocess.NewManager(),
		installScope: "current_user",
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if exe, err := os.Executable(); err == nil {
		a.exePath = exe
	}
	a.loadPersistedState()
	a.ensureInstallBootstrapped()
	a.ensureSharedFilesIfMissing(a.installDir)
}

func (a *App) ensureInstallBootstrapped() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.installState.Installed {
		return
	}

	if install.IsNodeBinaryPresent(a.installDir) {
		a.markInstalledLocked()
		return
	}

	source := install.FindBundledNodeBinary(a.exePath)
	if source == "" {
		return
	}

	if err := install.Install(source, a.installDir); err != nil {
		return
	}
	a.markInstalledLocked()
}

func (a *App) markInstalledLocked() {
	a.installState = &install.State{
		Installed:   true,
		InstallDir:  a.installDir,
		InstalledAt: time.Now().UTC().Format(time.RFC3339),
		Version:     guiVersion,
	}
	_ = a.saveInstallStateLocked()
}

func (a *App) stateFilePath() string {
	return filepath.Join(a.installDir, config.StateFileName)
}

func (a *App) sessionFilePath() string {
	return filepath.Join(a.installDir, config.SessionFileName)
}

func (a *App) loadPersistedState() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if state, err := install.LoadState(a.stateFilePath()); err == nil && state.InstallDir != "" {
		a.installState = state
		a.installDir = state.InstallDir
	} else if state, err := install.LoadState(a.stateFilePath()); err == nil {
		a.installState = state
	}

	if raw, err := os.ReadFile(a.sessionFilePath()); err == nil {
		var session Session
		if json.Unmarshal(raw, &session) == nil && session.Wallet != "" {
			a.session = &session
		}
	}
}

func (a *App) saveInstallStateLocked() error {
	a.installState.InstallDir = a.installDir
	return install.SaveState(a.stateFilePath(), a.installState)
}

func (a *App) saveSessionLocked() error {
	if a.session == nil {
		return os.Remove(a.sessionFilePath())
	}
	raw, err := json.MarshalIndent(a.session, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.sessionFilePath()), 0o755); err != nil {
		return err
	}
	return os.WriteFile(a.sessionFilePath(), raw, 0o644)
}

type BootstrapView struct {
	Installed       bool   `json:"installed"`
	InstallDir      string `json:"installDir"`
	DefaultInstallDir string `json:"defaultInstallDir"`
	WebBaseURL      string `json:"webBaseUrl"`
	Version         string `json:"version"`
	Wallet          string `json:"wallet,omitempty"`
	WalletVerified  bool   `json:"walletVerified"`
	NodeRunning     bool   `json:"nodeRunning"`
}

func (a *App) GetBootstrap() BootstrapView {
	a.mu.Lock()
	defer a.mu.Unlock()

	view := BootstrapView{
		Installed:         a.installState.Installed,
		InstallDir:        a.installDir,
		DefaultInstallDir: config.DefaultLocalAppDataInstallDir(),
		WebBaseURL:        config.WebBaseURL(),
		Version:           guiVersion,
		NodeRunning:       a.nodeProc.IsRunning() || len(nodeprocess.DiscoverRunningNodeNames(a.installDir)) > 0,
	}
	if a.session != nil {
		view.Wallet = a.session.Wallet
		view.WalletVerified = a.session.Verified
	}
	return view
}

func (a *App) AcceptLicense() {
	a.licenseAccepted = true
}

func (a *App) SetInstallScope(scope string) {
	if scope == "all_users" || scope == "current_user" {
		a.installScope = scope
	}
}

func (a *App) SetInstallDir(dir string) {
	if dir != "" {
		a.installDir = dir
	}
}

func (a *App) GetInstallDir() string {
	return a.installDir
}

type InstallResult struct {
	OK         bool   `json:"ok"`
	InstallDir string `json:"installDir,omitempty"`
	Error      string `json:"error,omitempty"`
}

func (a *App) RunInstall() InstallResult {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.licenseAccepted {
		return InstallResult{Error: "Accept the license agreement first."}
	}

	source := install.FindBundledNodeBinary(a.exePath)
	if err := install.Install(source, a.installDir); err != nil {
		return InstallResult{Error: err.Error()}
	}

	a.installState = &install.State{
		Installed:   true,
		InstallDir:  a.installDir,
		InstalledAt: time.Now().UTC().Format(time.RFC3339),
		Version:     guiVersion,
	}
	if err := a.saveInstallStateLocked(); err != nil {
		return InstallResult{Error: err.Error()}
	}

	return InstallResult{OK: true, InstallDir: a.installDir}
}

func (a *App) OpenExternalURL(url string) {
	if url == "" {
		return
	}
	runtime.BrowserOpenURL(a.ctx, url)
}

type BrowserSignInResult struct {
	OK      bool   `json:"ok"`
	Wallet  string `json:"wallet,omitempty"`
	Error   string `json:"error,omitempty"`
	AuthURL string `json:"authUrl,omitempty"`
}

func (a *App) SignInWithBrowser() BrowserSignInResult {
	ctx, cancel := context.WithCancel(a.ctx)
	defer cancel()

	server, callbackURL, err := authserver.Start(ctx, a.webClient.BaseURL)
	if err != nil {
		return BrowserSignInResult{Error: err.Error()}
	}

	authURL := nodeweb.AuthGUIURL(a.webClient.BaseURL, callbackURL)
	runtime.BrowserOpenURL(a.ctx, authURL)

	result, err := server.WaitResult(3 * time.Minute)
	if err != nil {
		return BrowserSignInResult{Error: err.Error(), AuthURL: authURL}
	}

	verify, err := a.webClient.VerifyLogin(
		result.ChallengeToken,
		result.Wallet,
		result.SignatureBase64,
		result.Message,
	)
	if err != nil {
		return BrowserSignInResult{Error: err.Error(), AuthURL: authURL}
	}

	a.mu.Lock()
	a.session = &Session{
		Wallet:   verify.Wallet,
		Verified: true,
		LoginAt:  time.Now().UTC().Format(time.RFC3339),
	}
	_ = a.saveSessionLocked()
	a.mu.Unlock()

	return BrowserSignInResult{OK: true, Wallet: verify.Wallet, AuthURL: authURL}
}

type RegisterNodeResult struct {
	OK        bool   `json:"ok"`
	Pending   bool   `json:"pending,omitempty"`
	Error     string `json:"error,omitempty"`
	NodeID    string `json:"nodeId,omitempty"`
	NodeName  string `json:"nodeName,omitempty"`
	PublicKey string `json:"publicKey,omitempty"`
	AuthURL   string `json:"authUrl,omitempty"`
}

func (a *App) setRegisterPhase(phase string) {
	a.registerMu.Lock()
	a.registerPhase = phase
	a.registerMu.Unlock()
	runtime.EventsEmit(a.ctx, "register:phase", phase)
}

func (a *App) finishRegisterJob(result RegisterNodeResult) {
	a.registerMu.Lock()
	a.registerActive = false
	a.registerPhase = ""
	a.registerResult = &result
	a.registerMu.Unlock()
	runtime.EventsEmit(a.ctx, "register:finished", result)
}

type RegisterStatusView struct {
	Active   bool               `json:"active"`
	Phase    string             `json:"phase"`
	NodeName string             `json:"nodeName,omitempty"`
	Result   *RegisterNodeResult `json:"result,omitempty"`
}

func (a *App) GetRegisterStatus() RegisterStatusView {
	a.registerMu.Lock()
	defer a.registerMu.Unlock()

	view := RegisterStatusView{
		Active:   a.registerActive,
		Phase:    a.registerPhase,
		NodeName: a.registerJobName,
	}
	if a.registerResult != nil {
		copy := *a.registerResult
		view.Result = &copy
	}
	return view
}

func (a *App) emitRegisterFailure(authURL, message string) {
	a.finishRegisterJob(RegisterNodeResult{Error: message, AuthURL: authURL})
}

func (a *App) configureLocalNodeFiles(installDir string, generated *nodeidentity.Generated) error {
	onboarding, err := a.webClient.GetOnboardingConfig()
	if err != nil {
		return err
	}

	nodeWebURL := onboarding.NodeWebURL
	if nodeWebURL == "" {
		nodeWebURL = a.webClient.BaseURL
	}
	operatorYAML := setupfiles.BuildOperatorConfigYAML(nodeWebURL, onboarding.PingIntervalSeconds)
	if _, err := setupfiles.EnsureSharedFiles(installDir, onboarding.NetworkConfigYaml, operatorYAML); err != nil {
		return err
	}
	return nodeidentity.WriteFiles(installDir, generated)
}

func (a *App) runRegisterNodeJob(
	server *authserver.Server,
	generated *nodeidentity.Generated,
	wallet, installDir, authURL string,
) {
	result, err := server.WaitResult(3 * time.Minute)
	if err != nil {
		a.emitRegisterFailure(authURL, err.Error())
		return
	}
	if result.Wallet != wallet {
		a.emitRegisterFailure(authURL, "Signed wallet does not match the desktop session.")
		return
	}

	a.setRegisterPhase("configuring")
	if err := a.configureLocalNodeFiles(installDir, generated); err != nil {
		a.emitRegisterFailure(authURL, err.Error())
		return
	}

	a.setRegisterPhase("registering")
	_, err = a.webClient.RegisterNode(nodeweb.RegisterNodeRequest{
		NodeID:              generated.NodeID,
		NodeName:            generated.NodeName,
		PublicKey:           generated.PublicKey,
		ChallengeToken:      result.ChallengeToken,
		Wallet:              result.Wallet,
		SignatureBase64:     result.SignatureBase64,
		Message:             result.Message,
		SignedMessageBase64: result.SignedMessageBase64,
	})
	if err != nil {
		a.emitRegisterFailure(authURL, err.Error())
		return
	}

	a.finishRegisterJob(RegisterNodeResult{
		OK:        true,
		NodeID:    generated.NodeID,
		NodeName:  generated.NodeName,
		PublicKey: generated.PublicKey,
		AuthURL:   authURL,
	})
}

func (a *App) RegisterNode(nodeName string) RegisterNodeResult {
	nodeName = strings.TrimSpace(nodeName)
	if err := nodeidentity.ValidateNodeName(nodeName); err != nil {
		return RegisterNodeResult{Error: err.Error()}
	}

	a.registerMu.Lock()
	if a.registerActive {
		a.registerMu.Unlock()
		return RegisterNodeResult{Error: "A registration is already in progress."}
	}
	a.registerActive = true
	a.registerPhase = "waiting_wallet"
	a.registerJobName = nodeName
	a.registerResult = nil
	a.registerMu.Unlock()

	a.mu.Lock()
	wallet := ""
	verified := false
	if a.session != nil {
		wallet = a.session.Wallet
		verified = a.session.Verified
	}
	installDir := a.installDir
	a.mu.Unlock()

	if wallet == "" {
		a.registerMu.Lock()
		a.registerActive = false
		a.registerPhase = ""
		a.registerMu.Unlock()
		return RegisterNodeResult{Error: "Sign in with your wallet first."}
	}
	if !verified {
		a.registerMu.Lock()
		a.registerActive = false
		a.registerPhase = ""
		a.registerMu.Unlock()
		return RegisterNodeResult{Error: "Use Sign in with Browser so your wallet is verified before registering a node."}
	}

	status, err := a.webClient.GetWalletStatus(wallet)
	if err != nil {
		a.registerMu.Lock()
		a.registerActive = false
		a.registerPhase = ""
		a.registerMu.Unlock()
		return RegisterNodeResult{Error: err.Error()}
	}
	if !status.Eligibility.CanRegister {
		reason := "Not eligible to register a node"
		if status.Eligibility.BlockReason != nil && *status.Eligibility.BlockReason != "" {
			reason = *status.Eligibility.BlockReason
		}
		a.registerMu.Lock()
		a.registerActive = false
		a.registerPhase = ""
		a.registerMu.Unlock()
		return RegisterNodeResult{Error: reason}
	}

	generated, err := nodeidentity.Generate(nodeName)
	if err != nil {
		a.registerMu.Lock()
		a.registerActive = false
		a.registerPhase = ""
		a.registerMu.Unlock()
		return RegisterNodeResult{Error: err.Error()}
	}

	ctx, cancel := context.WithCancel(a.ctx)

	server, callbackURL, err := authserver.Start(ctx, a.webClient.BaseURL)
	if err != nil {
		cancel()
		a.registerMu.Lock()
		a.registerActive = false
		a.registerPhase = ""
		a.registerMu.Unlock()
		return RegisterNodeResult{Error: err.Error()}
	}

	authURL := nodeweb.AuthRegisterURL(
		a.webClient.BaseURL,
		callbackURL,
		generated.NodeID,
		generated.NodeName,
		generated.PublicKey,
	)
	runtime.BrowserOpenURL(a.ctx, authURL)
	a.setRegisterPhase("waiting_wallet")

	go func() {
		defer cancel()
		defer func() {
			if recovered := recover(); recovered != nil {
				a.emitRegisterFailure(authURL, fmt.Sprintf("registration crashed: %v", recovered))
			}
		}()
		a.runRegisterNodeJob(server, generated, wallet, installDir, authURL)
	}()

	return RegisterNodeResult{
		OK:       true,
		Pending:  true,
		NodeName: generated.NodeName,
		AuthURL:  authURL,
	}
}

func (a *App) EnsureSharedSetup() NodeActionResult {
	a.mu.Lock()
	installDir := a.installDir
	a.mu.Unlock()

	if err := a.writeSharedFiles(installDir); err != nil {
		return NodeActionResult{Error: err.Error()}
	}
	return NodeActionResult{OK: true}
}

func (a *App) ensureSharedFilesIfMissing(installDir string) {
	shared := install.InspectSharedSetup(installDir)
	if shared.NetworkConfigFound && shared.PasswordFound {
		return
	}
	_ = a.writeSharedFiles(installDir)
}

func (a *App) writeSharedFiles(installDir string) error {
	onboarding, err := a.webClient.GetOnboardingConfig()
	if err != nil {
		return err
	}
	nodeWebURL := onboarding.NodeWebURL
	if nodeWebURL == "" {
		nodeWebURL = a.webClient.BaseURL
	}
	operatorYAML := setupfiles.BuildOperatorConfigYAML(nodeWebURL, onboarding.PingIntervalSeconds)
	_, err = setupfiles.EnsureSharedFiles(installDir, onboarding.NetworkConfigYaml, operatorYAML)
	return err
}

func (a *App) SetWalletAddress(wallet string) BrowserSignInResult {
	if wallet == "" {
		return BrowserSignInResult{Error: "Wallet address is required."}
	}
	a.mu.Lock()
	a.session = &Session{
		Wallet:   wallet,
		Verified: false,
		LoginAt:  time.Now().UTC().Format(time.RFC3339),
	}
	_ = a.saveSessionLocked()
	a.mu.Unlock()
	return BrowserSignInResult{OK: true, Wallet: wallet}
}

func (a *App) SignOut() {
	a.mu.Lock()
	a.session = nil
	_ = a.saveSessionLocked()
	a.mu.Unlock()
}

type WalletStatusView struct {
	OK      bool                   `json:"ok"`
	Error   string                 `json:"error,omitempty"`
	Status  *nodeweb.WalletStatus  `json:"status,omitempty"`
	Local   *install.LocalSetup    `json:"local,omitempty"`
}

func (a *App) GetWalletStatus() WalletStatusView {
	a.mu.Lock()
	wallet := ""
	if a.session != nil {
		wallet = a.session.Wallet
	}
	installDir := a.installDir
	a.mu.Unlock()

	if wallet == "" {
		return WalletStatusView{Error: "Connect your wallet first."}
	}

	status, err := a.webClient.GetWalletStatus(wallet)
	if err != nil {
		return WalletStatusView{Error: err.Error()}
	}

	local, _ := install.InspectLocalSetup(installDir)
	return WalletStatusView{OK: true, Status: status, Local: local}
}

type NodeActionResult struct {
	OK        bool   `json:"ok"`
	Cancelled bool   `json:"cancelled,omitempty"`
	Error     string `json:"error,omitempty"`
}

type UnstakeNodeResult struct {
	OK                bool   `json:"ok"`
	Error             string `json:"error,omitempty"`
	Phase             string `json:"phase,omitempty"`
	Message           string `json:"message,omitempty"`
	ReturnAvailableAt string `json:"returnAvailableAt,omitempty"`
}

type NodeRowView struct {
	NodeID         string   `json:"nodeId"`
	NodeName       string   `json:"nodeName"`
	PublicKey      string   `json:"publicKey,omitempty"`
	WebStatus      string   `json:"webStatus"`
	LocalReady     bool     `json:"localReady"`
	ProcessRunning bool     `json:"processRunning"`
	MissingItems   []string `json:"missingItems"`
	CanUnstake     bool     `json:"canUnstake"`
}

type OffboardStatusView struct {
	PendingUnstake      bool    `json:"pendingUnstake"`
	ReturnAvailableAt   string  `json:"returnAvailableAt,omitempty"`
	HoursUntilReturn    float64 `json:"hoursUntilReturn,omitempty"`
	RegisteredNodeCount int     `json:"registeredNodeCount"`
}

type DashboardView struct {
	OK                 bool          `json:"ok"`
	Error              string        `json:"error,omitempty"`
	Wallet             string        `json:"wallet,omitempty"`
	WalletVerified     bool          `json:"walletVerified"`
	InstallDir         string        `json:"installDir"`
	Installed          bool          `json:"installed"`
	RunningNodeName    string        `json:"runningNodeName,omitempty"`
	RunningNodeNames   []string      `json:"runningNodeNames"`
	RunningCount       int           `json:"runningCount"`
	MaxConcurrentNodes int           `json:"maxConcurrentNodes"`
	RegisterURL        string        `json:"registerUrl"`
	DashboardURL       string        `json:"dashboardUrl"`
	StakingURL         string        `json:"stakingUrl"`
	CanRegister        bool          `json:"canRegister"`
	SharedMissing      []string      `json:"sharedMissing"`
	Offboard           *OffboardStatusView `json:"offboard,omitempty"`
	Nodes              []NodeRowView `json:"nodes"`
}

func (a *App) GetDashboard() DashboardView {
	a.mu.Lock()
	wallet := ""
	walletVerified := false
	if a.session != nil {
		wallet = a.session.Wallet
		walletVerified = a.session.Verified
	}
	installDir := a.installDir
	installed := a.installState.Installed
	webBase := a.webClient.BaseURL
	a.mu.Unlock()

	runningNodeNames := a.nodeProc.RunningNodeNames(installDir)
	runningNodes := map[string]bool{}
	for _, name := range runningNodeNames {
		runningNodes[name] = true
	}
	runningNode := ""
	if len(runningNodeNames) > 0 {
		runningNode = runningNodeNames[0]
	}

	view := DashboardView{
		InstallDir: installDir,
		Installed:  installed,
		Wallet:     wallet,
		WalletVerified: walletVerified,
		RunningNodeName: runningNode,
		RunningNodeNames: runningNodeNames,
		RunningCount: len(runningNodeNames),
		MaxConcurrentNodes: nodeprocess.MaxConcurrentNodes,
		RegisterURL: webBase + "/dashboard",
		DashboardURL: webBase + "/dashboard",
		StakingURL:  webBase + "/staking",
		Nodes:       []NodeRowView{},
	}

	shared := install.InspectSharedSetup(installDir)
	view.SharedMissing = shared.MissingItems
	localNodes, _ := install.ListNodeLocalSetups(installDir, shared)
	localByName := map[string]install.NodeLocalSetup{}
	for _, node := range localNodes {
		localByName[node.NodeName] = node
	}

	if wallet == "" {
		for _, local := range localNodes {
			view.Nodes = append(view.Nodes, nodeRowFromLocal(local, runningNodes, "local_only", false))
		}
		if len(view.Nodes) == 0 {
			view.Error = "Sign in to manage your registered nodes."
		} else {
			view.OK = true
		}
		return view
	}

	status, err := a.webClient.GetWalletStatus(wallet)
	if err != nil {
		view.Error = err.Error()
		for _, local := range localNodes {
			view.Nodes = append(view.Nodes, nodeRowFromLocal(local, runningNodes, "local_only", false))
		}
		if len(view.Nodes) > 0 {
			view.OK = true
		}
		return view
	}

	view.RegisterURL = status.GUI.RegisterURL
	view.DashboardURL = status.GUI.DashboardURL
	view.StakingURL = status.GUI.StakingURL
	view.CanRegister = status.Eligibility.CanRegister

	offboardStatus, _ := a.webClient.GetOffboardStatus(wallet)
	pendingUnstake := offboardStatus != nil && offboardStatus.PendingUnstake != nil
	if offboardStatus != nil {
		view.Offboard = &OffboardStatusView{
			PendingUnstake:      pendingUnstake,
			RegisteredNodeCount: offboardStatus.RegisteredNodeCount,
		}
		if offboardStatus.ReturnAvailableAt != nil {
			view.Offboard.ReturnAvailableAt = *offboardStatus.ReturnAvailableAt
		}
		if offboardStatus.HoursUntilReturn != nil {
			view.Offboard.HoursUntilReturn = *offboardStatus.HoursUntilReturn
		}
	}

	activeIDs, _ := a.webClient.GetActiveNodeIDs()
	seen := map[string]bool{}

	for _, record := range status.Nodes {
		name := nodeweb.NodeRecordName(record)
		if name == "" {
			continue
		}
		seen[name] = true
		local := localByName[name]
		if local.NodeName == "" {
			if inspected, err := install.InspectNodeLocalSetup(installDir, name); err == nil && inspected != nil {
				local = *inspected
			}
		}
		webStatus := "registered"
		if activeIDs[record.NodeID] {
			webStatus = "active"
		}
		view.Nodes = append(view.Nodes, buildNodeRow(record.NodeID, name, local, runningNodes, webStatus, record.PublicKey, pendingUnstake))
	}

	for _, local := range localNodes {
		if seen[local.NodeName] {
			continue
		}
		view.Nodes = append(view.Nodes, nodeRowFromLocal(local, runningNodes, "local_only", pendingUnstake))
	}

	view.OK = true
	return view
}

func buildNodeRow(nodeID, nodeName string, local install.NodeLocalSetup, runningNodes map[string]bool, webStatus string, publicKey *string, pendingUnstake bool) NodeRowView {
	row := NodeRowView{
		NodeID:         nodeID,
		NodeName:       nodeName,
		WebStatus:      webStatus,
		LocalReady:     local.ReadyToStart,
		MissingItems:   local.MissingItems,
		ProcessRunning: runningNodes[nodeName],
		CanUnstake:     webStatus != "local_only" && nodeID != "" && !pendingUnstake,
	}
	if publicKey != nil {
		row.PublicKey = *publicKey
	} else if local.PublicKey != "" {
		row.PublicKey = local.PublicKey
	}
	return row
}

func nodeRowFromLocal(local install.NodeLocalSetup, runningNodes map[string]bool, webStatus string, pendingUnstake bool) NodeRowView {
	return NodeRowView{
		NodeID:         local.NodeID,
		NodeName:       local.NodeName,
		PublicKey:      local.PublicKey,
		WebStatus:      webStatus,
		LocalReady:     local.ReadyToStart,
		MissingItems:   local.MissingItems,
		ProcessRunning: runningNodes[local.NodeName],
		CanUnstake:     webStatus != "local_only" && local.NodeID != "" && !pendingUnstake,
	}
}

func (a *App) StartNode(nodeName string) NodeActionResult {
	nodeName = strings.TrimSpace(nodeName)
	if nodeName == "" {
		return NodeActionResult{Error: "Select a node to start."}
	}

	a.mu.Lock()
	installDir := a.installDir
	wallet := ""
	if a.session != nil {
		wallet = a.session.Wallet
	}
	a.mu.Unlock()

	if wallet == "" {
		return NodeActionResult{Error: "Sign in with your wallet first."}
	}

	if a.nodeProc.IsNodeRunning(installDir, nodeName) {
		return NodeActionResult{OK: true}
	}
	if nodeprocess.MaxConcurrentNodes > 0 && a.nodeProc.RunningCount(installDir) >= nodeprocess.MaxConcurrentNodes {
		return NodeActionResult{Error: fmt.Sprintf("Maximum %d nodes can run at once.", nodeprocess.MaxConcurrentNodes)}
	}

	// Validate locally without a network round-trip so Start is instant.
	shared := install.InspectSharedSetup(installDir)
	local, _ := install.InspectNodeLocalSetup(installDir, nodeName)
	if local == nil || local.NodeName == "" {
		return NodeActionResult{Error: "Node not found locally. Register it in this app first."}
	}
	if !shared.NodeBinaryPresent {
		return NodeActionResult{Error: "Node binary missing. Click Repair Binary to restore it."}
	}
	if !local.ReadyToStart {
		missing := strings.Join(local.MissingItems, ", ")
		if missing == "" {
			missing = "identity or config files"
		}
		return NodeActionResult{Error: "Cannot start: missing " + missing + ". Use Generate Config Files or re-register this node."}
	}

	if err := a.nodeProc.Start(installDir, nodeName); err != nil {
		return NodeActionResult{Error: err.Error()}
	}

	// cmd.Start returns as soon as the process spawns, so a node that dies on a
	// bad config would look like a successful start. Give it a moment and report
	// the captured output instead of silently showing a stopped node.
	time.Sleep(1500 * time.Millisecond)
	if a.nodeProc.IsNodeRunning(installDir, nodeName) {
		return NodeActionResult{OK: true}
	}

	logs := a.nodeProc.LogsForNode(nodeName)
	if len(logs) > 6 {
		logs = logs[len(logs)-6:]
	}
	detail := strings.TrimSpace(strings.Join(logs, "\n"))
	if detail == "" {
		detail = "no output was captured"
	}
	return NodeActionResult{Error: nodeName + " exited right after starting. Check the Logs tab.\n\n" + detail}
}

// StopNode terminates nodeName. An empty nodeName stops every node process for
// this install folder. The confirmation prompt is shown by the frontend.
func (a *App) StopNode(nodeName string) NodeActionResult {
	a.mu.Lock()
	installDir := a.installDir
	a.mu.Unlock()

	if err := a.nodeProc.Stop(installDir, strings.TrimSpace(nodeName)); err != nil {
		return NodeActionResult{Error: err.Error()}
	}
	return NodeActionResult{OK: true}
}

func (a *App) GetNodeLogs() []string {
	return a.nodeProc.Logs()
}

func (a *App) UnstakeNode(nodeName string) UnstakeNodeResult {
	nodeName = strings.TrimSpace(nodeName)
	if nodeName == "" {
		return UnstakeNodeResult{Error: "Select a node to unstake."}
	}

	wallet := a.sessionWallet()
	if wallet == "" {
		return UnstakeNodeResult{Error: "Sign in with your wallet first."}
	}

	a.mu.Lock()
	installDir := a.installDir
	a.mu.Unlock()

	view := a.GetDashboard()
	if !view.OK && view.Error != "" {
		return UnstakeNodeResult{Error: view.Error}
	}

	var target *NodeRowView
	for i := range view.Nodes {
		if view.Nodes[i].NodeName == nodeName {
			target = &view.Nodes[i]
			break
		}
	}
	if target == nil {
		return UnstakeNodeResult{Error: "Node not found."}
	}
	if target.WebStatus == "local_only" {
		return UnstakeNodeResult{Error: "Register this node before unstaking it."}
	}
	if target.NodeID == "" {
		return UnstakeNodeResult{Error: "Node ID is missing for this node."}
	}
	if view.Offboard != nil && view.Offboard.PendingUnstake {
		return UnstakeNodeResult{
			Error: "An unstake is already in progress for this wallet.",
			Phase: "already_pending",
			Message: view.Offboard.ReturnAvailableAt,
		}
	}

	if a.nodeProc.IsNodeRunning(installDir, nodeName) {
		if err := a.nodeProc.Stop(installDir, nodeName); err != nil {
			return UnstakeNodeResult{Error: fmt.Sprintf("Failed to stop node: %v", err)}
		}
		time.Sleep(2 * time.Second)
	}

	resp, err := a.webClient.OffboardNode(nodeweb.OffboardNodeRequest{
		Wallet:         wallet,
		NodeID:         target.NodeID,
		NodeName:       nodeName,
		ProcessStopped: true,
	})
	if err != nil {
		return UnstakeNodeResult{Error: err.Error()}
	}

	if err := install.RemoveNodeLocalSetup(installDir, nodeName); err != nil {
		return UnstakeNodeResult{Error: fmt.Sprintf("Offboard started but local cleanup failed: %v", err)}
	}
	_ = a.webClient.LogLocalIdentityRemoved(wallet, target.NodeID, nodeName)

	result := UnstakeNodeResult{
		OK:      true,
		Phase:   resp.Phase,
		Message: resp.Message,
	}
	if resp.ReturnAvailableAt != nil {
		result.ReturnAvailableAt = *resp.ReturnAvailableAt
	}
	return result
}

func (a *App) IsNodeRunning() bool {
	a.mu.Lock()
	installDir := a.installDir
	a.mu.Unlock()
	if a.nodeProc.IsRunning() {
		return true
	}
	return len(nodeprocess.DiscoverRunningNodeNames(installDir)) > 0
}

func (a *App) OpenInstallFolder() {
	a.mu.Lock()
	dir := a.installDir
	a.mu.Unlock()
	if dir == "" {
		return
	}
	runtime.BrowserOpenURL(a.ctx, "file:///"+filepath.ToSlash(dir))
}

func (a *App) GetRecommendedLinks() map[string]string {
	view := a.GetWalletStatus()
	if !view.OK || view.Status == nil {
		base := a.webClient.BaseURL
		return map[string]string{
			"stakingUrl":   base + "/staking",
			"dashboardUrl": base + "/dashboard",
			"registerUrl":  base + "/dashboard",
		}
	}
	return map[string]string{
		"stakingUrl":   view.Status.GUI.StakingURL,
		"dashboardUrl": view.Status.GUI.DashboardURL,
		"registerUrl":  view.Status.GUI.RegisterURL,
	}
}

func (a *App) sessionWallet() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.session == nil {
		return ""
	}
	return a.session.Wallet
}

func (a *App) RepairInstall() InstallResult {
	a.mu.Lock()
	if a.installState.Installed {
		a.licenseAccepted = true
	}
	a.mu.Unlock()
	return a.RunInstall()
}

func (a *App) GetActionMessage() string {
	view := a.GetWalletStatus()
	if !view.OK {
		return "Website login and desktop login are separate. Link the same wallet here first, then check staking and node registration status."
	}
	switch view.Status.GUI.RecommendedAction {
	case "stake_on_web":
		return fmt.Sprintf("Stake at least %.4f SOL on AICW Node Web before continuing.", view.Status.Eligibility.RequiredStakeSol)
	case "register_in_app":
		return "Your wallet meets the staking requirement. Click + Register Node in this app to create and register a node."
	case "setup_local":
		if view.Local != nil && view.Local.ReadyToStart {
			return "Your node is ready to start from this app."
		}
		return "Generate the missing config files in this app, or register a node here to create them automatically."
	default:
		return "Your node is ready to start."
	}
}

// HandleBeforeClose runs when the user clicks the window close (X) button.
// Returning true keeps the window open. The dialog must not run on the caller's
// thread, so the first close request always returns true and the actual quit is
// triggered from a goroutine once the user confirms.
func (a *App) HandleBeforeClose(ctx context.Context) bool {
	a.mu.Lock()
	if a.quitting {
		a.mu.Unlock()
		return false
	}
	if a.closePromptOpen {
		a.mu.Unlock()
		return true
	}
	a.closePromptOpen = true
	installDir := a.installDir
	a.mu.Unlock()

	runningNames := a.nodeProc.RunningNodeNames(installDir)
	message := "Quit AICW Node?"
	if len(runningNames) == 1 {
		message = fmt.Sprintf("Node %q is still running.\n\nStop the node and quit AICW Node?", runningNames[0])
	} else if len(runningNames) > 1 {
		message = fmt.Sprintf("%d nodes are still running (%s).\n\nStop them and quit AICW Node?", len(runningNames), strings.Join(runningNames, ", "))
	}

	go func() {
		result, err := runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
			Type:          runtime.QuestionDialog,
			Title:         "Quit AICW Node",
			Message:       message,
			Buttons:       []string{"Quit", "Cancel"},
			DefaultButton: "Quit",
			CancelButton:  "Cancel",
		})
		// A dialog failure must never trap the user in the app.
		confirmed := err != nil || strings.TrimSpace(result) != "Cancel"

		a.mu.Lock()
		a.closePromptOpen = false
		if confirmed {
			a.quitting = true
		}
		a.mu.Unlock()

		if !confirmed {
			return
		}
		_ = a.nodeProc.StopAll(installDir)
		runtime.Quit(ctx)
	}()

	return true
}
