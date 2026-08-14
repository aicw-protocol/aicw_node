#!/usr/bin/env bash
# Build AICW Node desktop GUI + bundled node engine for the current or target OS.
#
# Usage:
#   ./scripts/build-gui.sh                     # native OS/arch
#   GOOS=linux GOARCH=amd64 ./scripts/build-gui.sh
#   GOOS=darwin GOARCH=arm64 ./scripts/build-gui.sh
#   GOOS=darwin GOARCH=universal ./scripts/build-gui.sh   # macOS only
#
# Output (dist/):
#   Windows: aicw-node-setup-windows-amd64-installer.exe (NSIS, Programs and Features)
#   Linux:   aicw-node-setup-linux-amd64.zip
#   macOS:   aicw-node-setup-darwin-universal.app.zip

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GUI_DIR="$ROOT/aicw-node-gui"
DIST_DIR="$ROOT/dist"
mkdir -p "$DIST_DIR"

GOOS="${GOOS:-$(go env GOOS)}"
GOARCH="${GOARCH:-$(go env GOARCH)}"
TARGET_GOARCH="$GOARCH"

node_local_name="aicw-node"
setup_suffix="${GOOS}-${GOARCH}"
platform="${GOOS}/${GOARCH}"
windows_installer_name="aicw-node-setup-windows-amd64-installer.exe"

if [ "$GOOS" = "windows" ]; then
  node_local_name="aicw-node.exe"
  setup_suffix="${GOOS}-${GOARCH}.exe"
fi

if [ "$GOOS" = "darwin" ] && [ "$GOARCH" = "universal" ]; then
  platform="darwin/universal"
  setup_suffix="darwin-universal.app.zip"
fi

setup_dist_name="aicw-node-setup-${setup_suffix}"
if [ "$GOOS" = "darwin" ] && [ "$GOARCH" = "universal" ]; then
  setup_dist_name="aicw-node-setup-darwin-universal.app.zip"
fi

NODE_LOCAL="$GUI_DIR/$node_local_name"
SETUP_DIST="$DIST_DIR/$setup_dist_name"

if [ -n "${GITHUB_REF_NAME:-}" ] && [[ "$GITHUB_REF_NAME" == v* ]]; then
  PRODUCT_VERSION="${GITHUB_REF_NAME#v}"
  if command -v node >/dev/null 2>&1; then
    node - "$GUI_DIR/wails.json" "$PRODUCT_VERSION" <<'EOF'
const fs = require("fs");
const [file, version] = process.argv.slice(2);
const json = JSON.parse(fs.readFileSync(file, "utf8"));
json.info.productVersion = version;
fs.writeFileSync(file, JSON.stringify(json, null, 2) + "\n");
EOF
  fi
fi

echo "==> Building aicw-node (${platform})"
pushd "$ROOT" >/dev/null
if [ "$GOOS" = "darwin" ] && [ "$GOARCH" = "universal" ]; then
  node_amd64="$(mktemp)"
  node_arm64="$(mktemp)"
  CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o "$node_amd64" ./cmd/aicw-node
  CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 \
    go build -trimpath -ldflags="-s -w" -o "$node_arm64" ./cmd/aicw-node
  lipo -create -output "$NODE_LOCAL" "$node_amd64" "$node_arm64"
  rm -f "$node_amd64" "$node_arm64"
else
  CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" \
    go build -trimpath -ldflags="-s -w" -o "$NODE_LOCAL" ./cmd/aicw-node
fi
popd >/dev/null

echo "==> Wails bindings"
pushd "$GUI_DIR" >/dev/null
if command -v wails >/dev/null 2>&1; then
  wails generate module
else
  go run github.com/wailsapp/wails/v2/cmd/wails@v2.10.1 generate module
fi
go mod tidy
popd >/dev/null

echo "==> Building GUI (${platform})"
pushd "$GUI_DIR" >/dev/null
export CGO_ENABLED=1

target_goarch="$TARGET_GOARCH"
wails_build_args=(-platform "$platform" -clean -skipbindings)
if [ "$GOOS" = "linux" ]; then
  wails_build_args+=(-tags webkit2_41)
fi
if [ "$GOOS" = "windows" ] && [ "$TARGET_GOARCH" = "amd64" ]; then
  wails_build_args+=(-nsis)
fi

if [ "$GOOS" = "darwin" ] && [ "$target_goarch" = "universal" ]; then
  unset GOARCH
fi

if command -v wails >/dev/null 2>&1; then
  wails build "${wails_build_args[@]}"
  rm -rf "$DIST_DIR"/aicw-node-setup-*.app "$SETUP_DIST" 2>/dev/null || true
  if [ "$GOOS" = "darwin" ]; then
    app_path="$(find build/bin -maxdepth 1 -name '*.app' -print -quit)"
    if [ -z "$app_path" ]; then
      echo "Wails did not produce a .app bundle under build/bin" >&2
      exit 1
    fi
    cp "$NODE_LOCAL" "$app_path/Contents/MacOS/aicw-node"
    chmod +x "$app_path/Contents/MacOS/aicw-node" 2>/dev/null || true
    ditto -c -k --sequesterRsrc --keepParent "$app_path" "$SETUP_DIST"
  elif [ "$GOOS" = "windows" ] && [ "$TARGET_GOARCH" = "amd64" ]; then
    nsis_installer="build/bin/aicw-node-setup-amd64-installer.exe"
    if [ ! -f "$nsis_installer" ]; then
      echo "NSIS installer was not produced at $nsis_installer" >&2
      exit 1
    fi
    cp "$nsis_installer" "$DIST_DIR/$windows_installer_name"
  else
    built="$(find build/bin -maxdepth 1 -type f -name 'aicw-node-setup*' -print -quit)"
    if [ -z "$built" ]; then
      built="$(find build/bin -maxdepth 1 -type f -print -quit)"
    fi
    cp "$built" "$SETUP_DIST"
    chmod +x "$SETUP_DIST"
  fi
else
  ldflags="-s -w"
  go_tags="production"
  if [ "$GOOS" = "linux" ]; then
    go_tags="production,webkit2_41"
  fi
  if [ "$GOOS" = "darwin" ] && [ "$target_goarch" = "universal" ]; then
    GOARCH=arm64
  fi
  if [ "$GOOS" = "windows" ]; then
    ldflags="-H windowsgui -s -w"
  fi
  GOOS="$GOOS" GOARCH="${GOARCH:-$target_goarch}" go build -tags "$go_tags" -trimpath -ldflags="$ldflags" \
    -o "$SETUP_DIST" .
  chmod +x "$SETUP_DIST"
fi
popd >/dev/null

if [ "$GOOS" = "linux" ] && [ "$TARGET_GOARCH" = "amd64" ]; then
  bundled_engine="$DIST_DIR/$node_local_name"
  cp "$NODE_LOCAL" "$bundled_engine"
  chmod +x "$bundled_engine" 2>/dev/null || true
  (cd "$DIST_DIR" && zip -j -q "aicw-node-setup-linux-amd64.zip" \
    "aicw-node-setup-linux-amd64" "$node_local_name")
fi

if [ "$GOOS" = "$(go env GOOS)" ] && [ "$TARGET_GOARCH" = "$(go env GOARCH)" ]; then
  if [ "$GOOS" != "darwin" ] || [ "$TARGET_GOARCH" != "universal" ]; then
    if [ "$GOOS" = "windows" ]; then
      cp "$DIST_DIR/$windows_installer_name" "$DIST_DIR/aicw-node-setup.exe" 2>/dev/null || true
    else
      cp "$SETUP_DIST" "$DIST_DIR/aicw-node-setup" 2>/dev/null || true
    fi
  fi
fi

echo ""
echo "Done:"
if [ -f "$DIST_DIR/$windows_installer_name" ]; then
  echo "  $DIST_DIR/$windows_installer_name"
fi
echo "  $SETUP_DIST"
if [ -f "$DIST_DIR/aicw-node-setup-linux-amd64.zip" ]; then
  echo "  $DIST_DIR/aicw-node-setup-linux-amd64.zip"
fi
