const state = {
  step: "license",
  tab: "nodes",
  installDir: "",
  dashboard: null,
  logs: [],
  pollTimer: null,
  expandedNodes: {},
  showRegisterModal: false,
  registerNodeName: "",
  registerBusy: false,
  registerPhase: "",
  registerRecoveryTimer: null,
  registerStatusTimer: null,
  stopConfirmNode: null,
  stopBusy: false,
  unstakeConfirmNode: null,
  unstakeBusy: false,
  logNodeFilter: "all",
  persistentEventsBound: false,
};

const LICENSE_TEXT = `AICW Node Operator Software

By installing and running this software, you agree to operate an MPC node in
accordance with the AICW network rules and keep your identity and password
files secure on this computer.

This software is provided as-is while the network is under active development.`;

function appApi() {
  return window.go?.main?.App;
}

async function call(method, ...args) {
  const api = appApi();
  if (!api || typeof api[method] !== "function") {
    throw new Error(`Backend unavailable: ${method}`);
  }
  return api[method](...args);
}

function escapeHtml(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

function formatWalletLabel(address) {
  if (!address) return "Not signed in";
  if (address.length <= 12) return address;
  return `${address.slice(0, 4)}…${address.slice(-4)}`;
}

function setFooter(html) {
  document.getElementById("footerActions").innerHTML = html;
}

function setHeader(html) {
  document.getElementById("headerActions").innerHTML = html;
}

function setTabBar(visible) {
  const tabBar = document.getElementById("tabBar");
  if (!visible) {
    tabBar.classList.add("hidden");
    return;
  }
  tabBar.classList.remove("hidden");
  tabBar.innerHTML = `
    <button class="tab-btn ${state.tab === "nodes" ? "active" : ""}" data-tab="nodes">Nodes</button>
    <button class="tab-btn ${state.tab === "logs" ? "active" : ""}" data-tab="logs">Logs</button>
  `;
}

function registerPhaseLabel(phase) {
  switch (phase) {
    case "waiting_wallet":
      return "Complete the wallet sign-in in your browser, then return to this app.";
    case "registering":
      return "Registering on the AICW network…";
    case "configuring":
      return "Saving identity and config files on this computer…";
    default:
      return "Working…";
  }
}

function clearRegisterRecoveryPoll() {
  if (state.registerRecoveryTimer) {
    clearInterval(state.registerRecoveryTimer);
    state.registerRecoveryTimer = null;
  }
}

function clearRegisterStatusPoll() {
  if (state.registerStatusTimer) {
    clearInterval(state.registerStatusTimer);
    state.registerStatusTimer = null;
  }
}

function normalizeRegisterResult(result) {
  if (!result) return result;
  return {
    ok: Boolean(result.ok ?? result.OK),
    pending: Boolean(result.pending ?? result.Pending),
    error: result.error ?? result.Error ?? "",
    nodeId: result.nodeId ?? result.NodeID ?? "",
    nodeName: result.nodeName ?? result.NodeName ?? "",
    publicKey: result.publicKey ?? result.PublicKey ?? "",
    authUrl: result.authUrl ?? result.AuthURL ?? "",
  };
}

function updateRegisterPhaseText(phase) {
  const el = document.getElementById("registerPhaseText");
  if (el) el.textContent = registerPhaseLabel(phase);
}

function finishRegisterFlow(rawResult) {
  const result = normalizeRegisterResult(rawResult);
  clearRegisterRecoveryPoll();
  clearRegisterStatusPoll();
  state.registerBusy = false;
  state.registerPhase = "";
  state.showRegisterModal = false;
  if (!result?.ok) {
    const message = result?.error || "Node registration failed";
    alert(message);
    state.registerNodeName = "";
    renderDashboardShell();
    void refreshDashboard();
    return;
  }
  state.registerNodeName = "";
  if (result.nodeName) {
    state.expandedNodes[result.nodeName] = true;
  }
  state.tab = "nodes";
  renderDashboardShell();
  void refreshDashboard();
}

function startRegisterRecoveryPoll(nodeName) {
  clearRegisterRecoveryPoll();
  // Intentionally empty: polling GetDashboard during registration can freeze the WebView.
  void nodeName;
}

function startRegisterStatusPoll() {
  clearRegisterStatusPoll();
  state.registerStatusTimer = setInterval(async () => {
    if (!state.registerBusy) {
      clearRegisterStatusPoll();
      return;
    }
    try {
      const status = await call("GetRegisterStatus");
      if (status?.phase && status.phase !== state.registerPhase) {
        state.registerPhase = status.phase;
        updateRegisterPhaseText(status.phase);
      }
      if (status?.result) {
        finishRegisterFlow(status.result);
      }
    } catch {
      // Ignore transient status errors while registration finishes.
    }
  }, 1000);
}

function bindRegisterEvents() {
  const runtime = window.runtime;
  if (!runtime?.EventsOn) return;
  runtime.EventsOn("register:phase", (phase) => {
    if (!state.registerBusy) return;
    state.registerPhase = phase;
    updateRegisterPhaseText(phase);
  });
  runtime.EventsOn("register:finished", (result) => {
    if (!state.registerBusy) return;
    finishRegisterFlow(result);
  });
}

function webStatusLabel(status) {
  switch (status) {
    case "active":
      return "Network active";
    case "registered":
      return "Registered";
    case "local_only":
      return "Local only";
    default:
      return status;
  }
}

function webStatusBadgeClass(status) {
  switch (status) {
    case "active":
      return "badge-active";
    case "registered":
      return "badge-registered";
    default:
      return "badge-local";
  }
}

function renderLicense() {
  setHeader("");
  setTabBar(false);
  document.getElementById("screenRoot").innerHTML = `
    <section class="panel">
      <h1>Install AICW Node</h1>
      <p>Review the license, then choose an install folder.</p>
      <div class="license-box">${LICENSE_TEXT}</div>
    </section>`;
  setFooter(`
    <button id="btnCancel">Cancel</button>
    <button id="btnAgree" class="primary">Continue</button>
  `);
  document.getElementById("btnCancel").onclick = () => window.close();
  document.getElementById("btnAgree").onclick = async () => {
    await call("AcceptLicense");
    state.step = "path";
    render();
  };
}

function renderPath() {
  setHeader("");
  setTabBar(false);
  document.getElementById("screenRoot").innerHTML = `
    <section class="panel">
      <h1>Install Folder</h1>
      <p>Node binary and local identity files are stored here.</p>
      <div class="path-row">
        <input id="installDirInput" value="${escapeHtml(state.installDir)}" />
      </div>
    </section>`;
  setFooter(`
    <button id="btnBack">Back</button>
    <button id="btnInstall" class="primary">Install</button>
  `);
  document.getElementById("btnBack").onclick = () => {
    state.step = "license";
    render();
  };
  document.getElementById("btnInstall").onclick = async () => {
    const dir = document.getElementById("installDirInput").value.trim();
    await call("SetInstallDir", dir);
    await call("SetInstallScope", "current_user");
    state.installDir = dir;
    state.step = "installing";
    render();
    const result = await call("RunInstall");
    if (!result.ok) {
      alert(result.error || "Installation failed");
      state.step = "path";
      render();
      return;
    }
    state.step = "dashboard";
    render();
  };
}

function renderInstalling() {
  setHeader("");
  setTabBar(false);
  document.getElementById("screenRoot").innerHTML = `
    <section class="panel">
      <h1>Installing…</h1>
      <div class="progress"><span></span></div>
    </section>`;
  setFooter("");
}

function startBlockReason(node, dashboard) {
  if (node.processRunning) return "Already running";
  if (node.webStatus === "local_only") return "This node is not registered on the network yet.";
  if (!node.localReady) {
    const missing = (node.missingItems || []).join(", ");
    if (missing.includes("identity/") || missing.includes("private_key")) {
      return "Local identity/private key is missing. Unstake this node, then register a new name from this app.";
    }
    return missing ? `Missing files: ${missing}` : "Local config files are missing.";
  }
  const max = dashboard?.maxConcurrentNodes || 5;
  if ((dashboard?.runningCount || 0) >= max) return `Already running the maximum of ${max} nodes.`;
  return "";
}

function renderNodeItem(node, dashboard) {
  const expanded = state.expandedNodes[node.nodeName] ?? !node.localReady;
  const badges = [];
  if (node.processRunning) badges.push('<span class="badge badge-running">Running</span>');
  badges.push(`<span class="badge ${webStatusBadgeClass(node.webStatus)}">${webStatusLabel(node.webStatus)}</span>`);
  badges.push(
    node.localReady
      ? '<span class="badge badge-ready">Local ready</span>'
      : '<span class="badge badge-missing">Files missing</span>',
  );

  const missing =
    node.missingItems?.length > 0
      ? `<ul class="missing-list">${node.missingItems.map((item) => `<li>${escapeHtml(item)}</li>`).join("")}</ul>`
      : "";

  const blockReason = startBlockReason(node, dashboard);
  const canStart = !blockReason;
  const canStop = node.processRunning;
  const canUnstake = Boolean(node.canUnstake) && !state.unstakeBusy;

  return `
    <article class="node-item" data-node="${escapeHtml(node.nodeName)}">
      <div class="node-summary" data-toggle="${escapeHtml(node.nodeName)}">
        <span class="node-chevron">${expanded ? "▾" : "▸"}</span>
        <span class="node-title">${escapeHtml(node.nodeName)}</span>
        <div class="node-badges">${badges.join("")}</div>
      </div>
      <div class="toolbar node-actions">
        <button class="primary btn-start-node" data-node="${escapeHtml(node.nodeName)}" ${canStart ? "" : "disabled"} title="${escapeHtml(blockReason || "Start this node")}">Start</button>
        <button class="btn-stop-node" data-node="${escapeHtml(node.nodeName)}" ${canStop ? "" : "disabled"}>Stop</button>
        <button class="btn-unstake-node" data-node="${escapeHtml(node.nodeName)}" ${canUnstake ? "" : "disabled"}>Unstake</button>
      </div>
      <div class="node-details ${expanded ? "open" : ""}">
        <div class="node-meta">
          ${node.nodeId ? `<div><strong>Node ID</strong><br/><code>${escapeHtml(node.nodeId)}</code></div>` : ""}
          ${node.publicKey ? `<div><strong>Public key</strong><br/><code>${escapeHtml(node.publicKey)}</code></div>` : ""}
        </div>
        ${missing}
        ${blockReason && !node.processRunning ? `<p class="muted">${escapeHtml(blockReason)}</p>` : ""}
      </div>
    </article>`;
}

function renderStatusStrip(dashboard) {
  if (!dashboard.wallet) return "";
  const max = dashboard.maxConcurrentNodes || 5;
  const count = dashboard.runningCount || 0;
  let strip = "";
  if (dashboard.offboard?.pendingUnstake) {
    const when = dashboard.offboard.returnAvailableAt
      ? new Date(dashboard.offboard.returnAvailableAt).toLocaleString()
      : "after the 72-hour waiting period";
    strip += `<div class="status-strip status-unstake">Unstake in progress — staked SOL returns around <strong>${escapeHtml(when)}</strong>.</div>`;
  }
  if (count > 0) {
    const names = (dashboard.runningNodeNames || []).map(escapeHtml).join(", ");
    strip += `<div class="status-strip status-running">Running <strong>${count}/${max}</strong>${names ? `: ${names}` : ""} — logs are in the Logs tab.</div>`;
  } else if (!strip) {
    strip = `<div class="status-strip">No node process is running (up to ${max} at once).</div>`;
  }
  return strip;
}

function renderRegisterModal(dashboard) {
  if (!state.showRegisterModal) return "";
  const disabled = state.registerBusy || !dashboard.wallet || !dashboard.walletVerified;
  const eligibilityNote =
    dashboard.canRegister === false
      ? `<p class="muted">Staking requirement not met. Open the web staking page if needed.</p>`
      : `<p class="muted">Your node identity is created locally. Private keys never leave this computer.</p>`;

  const phaseMessage =
    state.registerBusy && state.registerPhase
      ? `<p class="register-phase muted" id="registerPhaseText">${escapeHtml(registerPhaseLabel(state.registerPhase))}</p>`
      : `<p class="register-phase muted" id="registerPhaseText"></p>`;

  return `
    <div class="modal-backdrop" id="registerModalBackdrop">
      <div class="modal">
        <h2>Register Node</h2>
        <p>Choose a node name (2–64 chars: letters, numbers, ., _, -).</p>
        ${eligibilityNote}
        <label class="field-label" for="registerNodeNameInput">Node name</label>
        <input id="registerNodeNameInput" value="${escapeHtml(state.registerNodeName)}" placeholder="e.g. my_node_01" ${disabled ? "disabled" : ""} />
        ${phaseMessage}
        <div class="toolbar modal-actions">
          <button id="btnCancelRegister">Cancel</button>
          <button id="btnSubmitRegister" class="primary" ${disabled ? "disabled" : ""}>
            ${state.registerBusy ? "Registering…" : "Register"}
          </button>
        </div>
      </div>
    </div>`;
}

function renderUnstakeConfirmModal() {
  if (!state.unstakeConfirmNode) return "";
  return `
    <div class="modal-backdrop" id="unstakeModalBackdrop">
      <div class="modal">
        <h2>Unstake Node</h2>
        <p>Stop <strong>${escapeHtml(state.unstakeConfirmNode)}</strong>, remove it from the network, and begin unstaking?</p>
        <ul class="muted unstake-steps">
          <li>The node process will be stopped.</li>
          <li>Network registration and local identity files will be removed.</li>
          <li>If this is your last node, staked SOL returns to your wallet after <strong>72 hours</strong>.</li>
        </ul>
        <div class="toolbar modal-actions">
          <button id="btnCancelUnstake" ${state.unstakeBusy ? "disabled" : ""}>Cancel</button>
          <button id="btnConfirmUnstake" class="danger" ${state.unstakeBusy ? "disabled" : ""}>
            ${state.unstakeBusy ? "Processing…" : "Unstake"}
          </button>
        </div>
      </div>
    </div>`;
}

function renderStopConfirmModal() {
  if (!state.stopConfirmNode) return "";
  return `
    <div class="modal-backdrop" id="stopModalBackdrop">
      <div class="modal">
        <h2>Stop Node</h2>
        <p>Stop <strong>${escapeHtml(state.stopConfirmNode)}</strong>?</p>
        <p class="muted">The node stops sending pings and leaves the signing network until you start it again.</p>
        <div class="toolbar modal-actions">
          <button id="btnCancelStop" ${state.stopBusy ? "disabled" : ""}>Cancel</button>
          <button id="btnConfirmStop" class="primary" ${state.stopBusy ? "disabled" : ""}>
            ${state.stopBusy ? "Stopping…" : "Stop"}
          </button>
        </div>
      </div>
    </div>`;
}

function renderNodesTab(dashboard) {
  const nodes = dashboard.nodes || [];
  const sharedBanner =
    dashboard.sharedMissing?.length > 0
      ? `<div class="banner">
          Shared install files missing: ${dashboard.sharedMissing.map(escapeHtml).join(", ")}
          Use <strong>Generate Config Files</strong> to create them.
        </div>`
      : "";

  const signInBlock = dashboard.wallet
    ? ""
    : `<div class="signin-box">
        <p>Sign in with the same Solana wallet you use for staking on the web.</p>
        <div class="toolbar">
          <button id="btnBrowserSignIn" class="primary">Sign in with Browser</button>
        </div>
      </div>`;

  const nodeList =
    nodes.length > 0
      ? `<div class="node-list">${nodes.map((node) => renderNodeItem(node, dashboard)).join("")}</div>`
      : `<div class="empty-state">
          <p>No nodes yet.</p>
          <p>Register a node here to create identity and config files automatically.</p>
        </div>`;

  return `
    <section class="panel panel-wide">
      <div class="panel-header">
        <div>
          <h1>Nodes</h1>
          <p class="section-desc muted">Register, start, and monitor your MPC nodes.</p>
        </div>
      </div>
      ${renderStatusStrip(dashboard)}
      ${sharedBanner}
      ${signInBlock}
      <div class="toolbar-split">
        <div class="toolbar">
          <button id="btnRegisterNode" class="primary">+ Register Node</button>
        </div>
        <div class="toolbar toolbar-secondary">
          <button id="btnEnsureShared" class="ghost">Generate Config Files</button>
          <button id="btnRefresh" class="ghost">Refresh</button>
          <button id="btnOpenFolder" class="ghost">Install Folder</button>
          <button id="btnRepair" class="ghost">Repair Binary</button>
        </div>
      </div>
      ${nodeList}
    </section>
    ${renderRegisterModal(dashboard)}
    ${renderStopConfirmModal()}
    ${renderUnstakeConfirmModal()}`;
}

function filterLogs(logs, filter) {
  if (!filter || filter === "all") return logs;
  const prefix = `[${filter}]`;
  return logs.filter((line) => line.startsWith(prefix));
}

function renderLogsTab(logs, dashboard) {
  const running = dashboard?.runningNodeNames || [];
  const options = ['<option value="all">All nodes</option>']
    .concat(running.map((name) => `<option value="${escapeHtml(name)}" ${state.logNodeFilter === name ? "selected" : ""}>${escapeHtml(name)}</option>`))
    .join("");
  const filtered = filterLogs(logs, state.logNodeFilter);
  return `
    <section class="panel panel-wide">
      <h1>Logs</h1>
      <p class="muted">Output from locally running node processes.</p>
      ${
        running.length > 0
          ? `<label class="field-label" for="logNodeFilter">Show logs for</label>
             <select id="logNodeFilter">${options}</select>`
          : ""
      }
      <div class="log-box" id="logBox">${filtered.map((line) => escapeHtml(line)).join("<br/>") || "No logs yet."}</div>
    </section>`;
}

function syncRegisterNodeNameFromDom() {
  if (!state.showRegisterModal) return;
  const input = document.getElementById("registerNodeNameInput");
  if (input) state.registerNodeName = input.value;
}

function bindDashboardEvents() {
  const logBox = document.getElementById("logBox");
  if (logBox) logBox.scrollTop = logBox.scrollHeight;
}

function renderDashboardHeader(dashboard) {
  if (!dashboard.wallet) {
    setHeader(`<button id="btnHeaderSignIn" class="primary">Sign in</button>`);
    return;
  }

  setHeader(`
    <span class="wallet-chip" title="${escapeHtml(dashboard.wallet)}">${escapeHtml(formatWalletLabel(dashboard.wallet))}</span>
    <button id="btnHeaderStaking" class="ghost">Staking</button>
    <button id="btnHeaderDashboard" class="ghost">Dashboard</button>
    <button id="btnHeaderSignOut" class="ghost">Sign out</button>
  `);
}

async function handleBrowserSignIn() {
  try {
    const result = await call("SignInWithBrowser");
    if (!result.ok) alert(result.error || "Browser sign-in failed");
    await refreshDashboard({ force: true });
  } catch (error) {
    alert(String(error));
  }
}

async function handleStartNode(nodeName) {
  try {
    const result = await call("StartNode", nodeName);
    if (!result.ok) {
      alert(result.error || "Failed to start node");
      await refreshDashboard({ force: true });
      return;
    }
    state.tab = "logs";
    await refreshDashboard({ force: true });
  } catch (error) {
    alert("Start failed: " + String(error));
    await refreshDashboard({ force: true });
  }
}

async function handleHeaderSignOut(dashboard) {
  const stop = await call("StopNode", "");
  if (!stop.ok) {
    alert(stop.error || "Failed to stop node");
    return;
  }
  await call("SignOut");
  await refreshDashboard({ force: true });
}

function bindPersistentEvents() {
  if (state.persistentEventsBound) return;
  state.persistentEventsBound = true;

  const app = document.getElementById("app");
  app.addEventListener(
    "mousedown",
    (event) => {
      if (state.step !== "dashboard") return;
      const btn = event.target.closest("button");
      if (!btn || btn.disabled) return;

      const dashboard = state.dashboard || { nodes: [] };

      if (btn.classList.contains("tab-btn")) {
        event.preventDefault();
        state.tab = btn.dataset.tab;
        renderDashboardShell({ force: true });
        return;
      }
      if (btn.id === "btnBrowserSignIn" || btn.id === "btnHeaderSignIn") {
        event.preventDefault();
        void handleBrowserSignIn();
        return;
      }
      if (btn.classList.contains("btn-start-node")) {
        event.preventDefault();
        void handleStartNode(btn.dataset.node);
        return;
      }
      if (btn.classList.contains("btn-stop-node")) {
        event.preventDefault();
        state.stopConfirmNode = btn.dataset.node;
        renderDashboardShell({ force: true });
        return;
      }
      if (btn.classList.contains("btn-unstake-node")) {
        event.preventDefault();
        state.unstakeConfirmNode = btn.dataset.node;
        renderDashboardShell({ force: true });
        return;
      }
      if (btn.id === "btnRefresh") {
        event.preventDefault();
        void refreshDashboard({ force: true });
        return;
      }
      if (btn.id === "btnOpenFolder") {
        event.preventDefault();
        void call("OpenInstallFolder");
        return;
      }
      if (btn.id === "btnRepair") {
        event.preventDefault();
        void (async () => {
          const result = await call("RepairInstall");
          if (!result.ok) alert(result.error || "Repair failed");
          await refreshDashboard({ force: true });
        })();
        return;
      }
      if (btn.id === "btnEnsureShared") {
        event.preventDefault();
        void (async () => {
          const result = await call("EnsureSharedSetup");
          if (!result.ok) alert(result.error || "Failed to generate config files");
          await refreshDashboard({ force: true });
        })();
        return;
      }
      if (btn.id === "btnRegisterNode") {
        event.preventDefault();
        if (!dashboard.wallet) {
          alert("Sign in with your wallet first.");
          return;
        }
        if (!dashboard.walletVerified) {
          alert("Use Sign in with Browser so your wallet is verified before registering a node.");
          return;
        }
        if (dashboard.canRegister === false) {
          alert("You are not eligible to register a node yet. Check staking on the web dashboard.");
          return;
        }
        state.showRegisterModal = true;
        state.registerNodeName = "";
        renderDashboardShell({ force: true });
        return;
      }
      if (btn.id === "btnCancelRegister") {
        event.preventDefault();
        state.showRegisterModal = false;
        if (!state.registerBusy) state.registerNodeName = "";
        renderDashboardShell({ force: true });
        return;
      }
      if (btn.id === "btnSubmitRegister") {
        event.preventDefault();
        void submitRegisterNode();
        return;
      }
      if (btn.id === "btnCancelStop") {
        event.preventDefault();
        if (state.stopBusy) return;
        state.stopConfirmNode = null;
        renderDashboardShell({ force: true });
        return;
      }
      if (btn.id === "btnConfirmStop") {
        event.preventDefault();
        void confirmStopNode();
        return;
      }
      if (btn.id === "btnCancelUnstake") {
        event.preventDefault();
        if (state.unstakeBusy) return;
        state.unstakeConfirmNode = null;
        renderDashboardShell({ force: true });
        return;
      }
      if (btn.id === "btnConfirmUnstake") {
        event.preventDefault();
        void confirmUnstakeNode();
        return;
      }
      if (btn.id === "btnHeaderStaking") {
        event.preventDefault();
        call("OpenExternalURL", dashboard.stakingUrl);
        return;
      }
      if (btn.id === "btnHeaderDashboard") {
        event.preventDefault();
        call("OpenExternalURL", dashboard.dashboardUrl);
        return;
      }
      if (btn.id === "btnHeaderSignOut") {
        event.preventDefault();
        void handleHeaderSignOut(dashboard);
      }
    },
    true,
  );

  app.addEventListener("click", (event) => {
    if (state.step !== "dashboard") return;
    if (event.target.id === "registerModalBackdrop") {
      state.showRegisterModal = false;
      renderDashboardShell({ force: true });
      return;
    }
    const toggle = event.target.closest("[data-toggle]");
    if (!toggle || event.target.closest("button")) return;
    const name = toggle.dataset.toggle;
    state.expandedNodes[name] = !(state.expandedNodes[name] ?? true);
    renderDashboardShell({ force: true });
  });

  app.addEventListener("change", (event) => {
    if (state.step !== "dashboard") return;
    if (event.target.id === "logNodeFilter") {
      state.logNodeFilter = event.target.value;
      renderDashboardShell({ force: true });
    }
  });

  app.addEventListener("input", (event) => {
    if (event.target.id === "registerNodeNameInput") {
      state.registerNodeName = event.target.value;
    }
  });
}

async function submitRegisterNode() {
  syncRegisterNodeNameFromDom();
  const nodeName = state.registerNodeName.trim();
  if (!nodeName) {
    alert("Enter a node name.");
    return;
  }
  if (state.registerBusy) return;
  state.registerBusy = true;
  state.registerPhase = "waiting_wallet";
  state.registerNodeName = nodeName;
  renderDashboardShell({ force: true });
  startRegisterRecoveryPoll(nodeName);
  startRegisterStatusPoll();
  try {
    const result = await call("RegisterNode", nodeName);
    if (!result?.pending) finishRegisterFlow(result);
  } catch (error) {
    clearRegisterRecoveryPoll();
    clearRegisterStatusPoll();
    state.registerBusy = false;
    state.registerPhase = "";
    alert(String(error));
    renderDashboardShell({ force: true });
  }
}

async function confirmStopNode() {
  if (state.stopBusy) return;
  const nodeName = state.stopConfirmNode;
  state.stopBusy = true;
  renderDashboardShell({ force: true });
  const result = await call("StopNode", nodeName);
  state.stopBusy = false;
  state.stopConfirmNode = null;
  if (!result.ok) alert(result.error || "Failed to stop node");
  await refreshDashboard({ force: true });
}

async function confirmUnstakeNode() {
  if (state.unstakeBusy) return;
  const nodeName = state.unstakeConfirmNode;
  state.unstakeBusy = true;
  renderDashboardShell({ force: true });
  const result = await call("UnstakeNode", nodeName);
  state.unstakeBusy = false;
  state.unstakeConfirmNode = null;
  if (!result.ok) {
    alert(result.error || "Unstake failed");
    await refreshDashboard({ force: true });
    return;
  }
  alert(result.message || "Unstake started.");
  await refreshDashboard({ force: true });
}

async function refreshDashboard(options) {
  syncRegisterNodeNameFromDom();
  const [dashboard, logs] = await Promise.all([call("GetDashboard"), call("GetNodeLogs")]);
  state.dashboard = dashboard;
  state.logs = logs;
  renderDashboardShell(options);
}

let lastRenderSignature = "";
let pointerIsDown = false;
let renderQueuedWhilePointerDown = false;

function renderSignature(dashboard) {
  const parts = [state.tab, state.logNodeFilter, dashboard];
  if (state.tab === "logs") {
    parts.push(state.logs?.length ?? 0, state.logs?.[state.logs.length - 1] ?? "");
  }
  return JSON.stringify(parts);
}

function shouldDeferRender(options) {
  return pointerIsDown && !options?.force;
}

// Replacing innerHTML between mousedown and mouseup destroys the button the
// browser is tracking, so the click event never fires. Defer every redraw
// while the pointer is down, and attach actions on #app with mousedown capture.
function renderDashboardShell(options = {}) {
  if (shouldDeferRender(options)) {
    renderQueuedWhilePointerDown = true;
    return;
  }

  const dashboard = state.dashboard || { nodes: [] };
  const background = Boolean(options.background);

  if (background) {
    const signature = renderSignature(dashboard);
    if (signature === lastRenderSignature) return;
  }
  lastRenderSignature = renderSignature(dashboard);

  setTabBar(true);
  renderDashboardHeader(dashboard);
  document.getElementById("screenRoot").innerHTML =
    state.tab === "logs" ? renderLogsTab(state.logs, dashboard) : renderNodesTab(dashboard);
  bindDashboardEvents();
  setFooter(`<span class="muted">${escapeHtml(dashboard.installDir || "")}</span>`);
}

window.addEventListener("mousedown", () => {
  pointerIsDown = true;
});
window.addEventListener("mouseup", () => {
  pointerIsDown = false;
  if (!renderQueuedWhilePointerDown) return;
  renderQueuedWhilePointerDown = false;
  // Defer past the click dispatch that follows this mouseup.
  setTimeout(() => renderDashboardShell({ background: true }), 0);
});

function renderDashboard() {
  refreshDashboard();
  if (state.pollTimer) clearInterval(state.pollTimer);
  state.pollTimer = setInterval(() => {
    if (state.showRegisterModal || state.registerBusy) return;
    if (state.stopConfirmNode || state.stopBusy) return;
    if (state.unstakeConfirmNode || state.unstakeBusy) return;
    refreshDashboard({ background: true });
  }, 4000);
}

function render() {
  if (state.step === "license") return renderLicense();
  if (state.step === "path") return renderPath();
  if (state.step === "installing") return renderInstalling();
  if (state.step === "dashboard") return renderDashboard();
}

async function boot() {
  try {
    bindRegisterEvents();
    bindPersistentEvents();
    const bootstrap = await call("GetBootstrap");
    state.installDir = bootstrap.installDir || bootstrap.defaultInstallDir;
    document.getElementById("versionLabel").textContent = `v${bootstrap.version}`;
    document.getElementById("footerMeta").textContent = bootstrap.webBaseUrl;
    state.step = bootstrap.installed ? "dashboard" : "license";
    render();
  } catch (error) {
    setTabBar(false);
    document.getElementById("screenRoot").innerHTML = `
      <section class="panel">
        <h1>Backend unavailable</h1>
        <p>${escapeHtml(String(error))}</p>
      </section>`;
  }
}

window.addEventListener("DOMContentLoaded", boot);
