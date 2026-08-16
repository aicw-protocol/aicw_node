#!/usr/bin/env bash
# Install reshare-orchestrator on the AICW VPS (Bridge host).
# Run ON THE VPS after copying artifacts (see scripts/bundle-orchestrator.ps1).
set -euo pipefail

INSTALL_ROOT="${INSTALL_ROOT:-/opt/aicw}"
ORCH_DIR="$INSTALL_ROOT/orchestrator"
SECRETS_DIR="$INSTALL_ROOT/secrets"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUNDLE_DIR="${BUNDLE_DIR:-$SCRIPT_DIR/../deployments/orchestrator-bundle}"

if [[ ! -f "$BUNDLE_DIR/reshare-orchestrator" ]]; then
  echo "Missing $BUNDLE_DIR/reshare-orchestrator — run scripts/bundle-orchestrator.ps1 first." >&2
  exit 1
fi
if [[ ! -f "$BUNDLE_DIR/reshare_initiator.key" ]]; then
  echo "Missing $BUNDLE_DIR/reshare_initiator.key — copy from .secrets/ (never commit)." >&2
  exit 1
fi

sudo mkdir -p "$ORCH_DIR" "$SECRETS_DIR"
sudo install -m 755 "$BUNDLE_DIR/reshare-orchestrator" "$ORCH_DIR/reshare-orchestrator"
sudo install -m 644 "$BUNDLE_DIR/network-config.yaml" "$ORCH_DIR/network-config.yaml"
sudo install -m 644 "$BUNDLE_DIR/orchestrator-config.yaml" "$ORCH_DIR/orchestrator-config.yaml"
sudo install -m 600 "$BUNDLE_DIR/reshare_initiator.key" "$SECRETS_DIR/reshare_initiator.key"

sudo install -m 644 "$BUNDLE_DIR/reshare-orchestrator.service" /etc/systemd/system/reshare-orchestrator.service
sudo systemctl daemon-reload
sudo systemctl enable reshare-orchestrator
sudo systemctl restart reshare-orchestrator

echo "=== reshare-orchestrator status ==="
sudo systemctl status reshare-orchestrator --no-pager || true
echo ""
echo "Logs: journalctl -u reshare-orchestrator -f"
