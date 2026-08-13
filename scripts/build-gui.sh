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
#   aicw-node-<platform>
#   aicw-node-setup-<platform>   (.app.zip on darwin universal)

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

if [ "$GOOS" = "windows" ]; then
  node_local_name="aicw-node.exe"
  setup_suffix="${GOOS}-${GOARCH}.exe"
fi

if [ "$GOOS" = "darwin" ] && [ "$GOARCH" = "universal" ]; then
  platform="darwin/universal"
  setup_suffix="darwin-universal.app.zip"
fi

node_dist_name="aicw-node-${setup_suffix}"
setup_dist_name="aicw-node-setup-${setup_suffix}"
if [ "$GOOS" = "darwin" ] && [ "$GOARCH" = "universal" ]; then
  node_dist_name="aicw-node-darwin-universal"
fi

NODE_LOCAL="$GUI_DIR/$node_local_name"
NODE_DIST="$DIST_DIR/$node_dist_name"
SETUP_DIST="$DIST_DIR/$setup_dist_name"

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
  # Ubuntu 24.04+ ships webkit2gtk 4.1; Wails defaults to 4.0 pkg-config.
  wails_build_args+=(-tags webkit2_41)
fi

# Wails understands darwin/universal via -platform; GOARCH=universal breaks the Go toolchain.
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
  GOOS="$GOOS" GOARCH="${GOARCH:-$target_goarch}" go build -tags "$go_tags" -trimpath -ldflags="$ldflags" \
    -o "$SETUP_DIST" .
  chmod +x "$SETUP_DIST"
fi
popd >/dev/null

cp "$NODE_LOCAL" "$NODE_DIST"
chmod +x "$NODE_DIST" 2>/dev/null || true

# Ship engine next to GUI installer (same folder as release downloads / zip contents).
bundled_engine="$DIST_DIR/$node_local_name"
cp "$NODE_LOCAL" "$bundled_engine"
chmod +x "$bundled_engine" 2>/dev/null || true

# Convenience copies for native dev builds
if [ "$GOOS" = "$(go env GOOS)" ] && [ "$TARGET_GOARCH" = "$(go env GOARCH)" ]; then
  cp "$NODE_DIST" "$DIST_DIR/aicw-node" 2>/dev/null || cp "$NODE_DIST" "$DIST_DIR/aicw-node.exe" 2>/dev/null || true
  if [ "$GOOS" != "darwin" ] || [ "$TARGET_GOARCH" != "universal" ]; then
    cp "$SETUP_DIST" "$DIST_DIR/aicw-node-setup" 2>/dev/null || cp "$SETUP_DIST" "$DIST_DIR/aicw-node-setup.exe" 2>/dev/null || true
  fi
fi

echo ""
echo "Done:"
echo "  $NODE_DIST"
echo "  $SETUP_DIST"
