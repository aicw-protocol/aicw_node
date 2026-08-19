#!/usr/bin/env bash
# VPS에서 실행 (root@158.247.251.191). 로컬에서 bundle-orchestrator.ps1 후 scp로 업로드했다고 가정.
set -euo pipefail
BUNDLE_DIR="${BUNDLE_DIR:-/tmp/orchestrator-bundle}"
INSTALL_ROOT="${INSTALL_ROOT:-/opt/aicw}"
ORCH_DIR="$INSTALL_ROOT/orchestrator"
SECRETS_DIR="$INSTALL_ROOT/secrets"

test -f "$BUNDLE_DIR/reshare-orchestrator"
test -f "$BUNDLE_DIR/reshare_initiator.key"

mkdir -p "$ORCH_DIR" "$SECRETS_DIR"
install -m 755 "$BUNDLE_DIR/reshare-orchestrator" "$ORCH_DIR/reshare-orchestrator"
install -m 644 "$BUNDLE_DIR/network-config.yaml" "$ORCH_DIR/network-config.yaml"
install -m 644 "$BUNDLE_DIR/orchestrator-config.yaml" "$ORCH_DIR/orchestrator-config.yaml"
install -m 600 "$BUNDLE_DIR/reshare_initiator.key" "$SECRETS_DIR/reshare_initiator.key"
install -m 644 "$BUNDLE_DIR/reshare-orchestrator.service" /etc/systemd/system/reshare-orchestrator.service
systemctl daemon-reload
systemctl enable reshare-orchestrator
systemctl restart reshare-orchestrator
systemctl status reshare-orchestrator --no-pager || true
echo "G-8 check: journalctl -u reshare-orchestrator -n 30 | grep 'Reconcile scan summary'"
