# AICW Node Desktop GUI

Cross-platform desktop app to **install, register, run, and monitor** the `aicw-node` engine.

Supported platforms: **Windows**, **Linux (amd64)**, **macOS (Universal .app)**.

## Operator flow

1. **Stake on the web** (if required) — [node.aicw.ai/staking](https://node.aicw.ai/staking)
2. **Install** the GUI for your OS (see [Releases](https://github.com/aicw-protocol/aicw_node/releases))
3. **Sign in with Browser** — same Solana wallet as the web
4. **+ Register Node** — identity created locally, wallet signs registration, config files written automatically
5. **Start** nodes (up to 5 at once) — logs in the **Logs** tab

After the OS installer or zip extract, the app **auto-detects** the bundled node engine and skips the old license/folder wizard. You land on the dashboard and only need wallet sign-in plus node registration.

The web dashboard is for **staking, status, offboard/unstake, and rewards**. Node registration happens in this app.

## Release downloads

Each release ships **one file per platform**:

| Platform | Download | What's inside |
|----------|----------|---------------|
| Windows | `aicw-node-setup-windows-amd64-installer.exe` | NSIS installer (Programs and Features uninstall) + GUI + `aicw-node.exe` |
| Linux | `aicw-node-setup-linux-amd64.zip` | GUI app + `aicw-node` engine |
| macOS | `aicw-node-setup-darwin-universal.app.zip` | `AICW Node.app` (engine bundled inside) |

Windows installs to `%LOCALAPPDATA%\Programs\AICW Node\` and registers under **Settings → Apps → Installed apps → AICW Node**.

Local dev builds also write `dist/aicw-node-setup-windows-amd64-installer.exe`.

## Install folders

| OS | Default install path |
|----|----------------------|
| Windows | `%LOCALAPPDATA%\Programs\AICW Node\` |
| Linux | `~/.config/AICW Node/` |
| macOS | `~/Library/Application Support/AICW Node/` |

## Build

### Windows

Requirements: Go 1.25+, WebView2, [NSIS](https://nsis.sourceforge.io/), CGO enabled.

```powershell
./scripts/build-gui.ps1
```

### Linux / macOS

Requirements: Go 1.25+, [Wails CLI](https://wails.io/docs/gettingstarted/installation), CGO enabled.

Linux deps (Debian/Ubuntu):

```bash
sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev
```

Build:

```bash
chmod +x scripts/build-gui.sh
./scripts/build-gui.sh                  # native
GOOS=linux GOARCH=amd64 ./scripts/build-gui.sh
GOOS=darwin GOARCH=universal ./scripts/build-gui.sh   # macOS .app.zip
```

## Local development (Windows example)

Run `aicw_node_web` on port 4003, then:

```powershell
$env:AICW_NODE_WEB_URL = "http://localhost:4003"
& "..\dist\aicw-node-setup.exe"
```

## Web APIs used by the GUI

| Endpoint | Purpose |
|----------|---------|
| `GET /api/gui/status?wallet=` | Eligibility + registered nodes |
| `GET /api/auth/challenge` | Login / registration challenges |
| `POST /api/auth/verify` | Verify login signature |
| `POST /api/nodes` | Register node (signed) |
| `POST /api/offboard/node` | Unstake / offboard flow |
| `GET /api/onboarding/config` | network-config template + URLs |
| `/auth/gui` | Browser wallet sign-in / register |

## UI

- **Nodes tab** — register, start/stop, unstake, file status
- **Logs tab** — subprocess stdout/stderr (filter by node)
- Up to **5 nodes** concurrently per GUI instance
