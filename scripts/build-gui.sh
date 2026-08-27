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
#   Windows: aicw-node-setup-vX.Y.Z-windows-amd64-installer.exe (NSIS)
#   Linux:   aicw-node-setup-vX.Y.Z-linux-amd64.zip
#   macOS:   aicw-node-setup-vX.Y.Z-darwin-universal.app.zip
#
# Installed app binary stays aicw-node-setup.exe (stable path after install).

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GUI_DIR="$ROOT/aicw-node-gui"
DIST_DIR="$ROOT/dist"
WAILS_JSON="$GUI_DIR/wails.json"
mkdir -p "$DIST_DIR"

GOOS="${GOOS:-$(go env GOOS)}"
GOARCH="${GOARCH:-$(go env GOARCH)}"
TARGET_GOARCH="$GOARCH"

node_local_name="aicw-node"
platform="${GOOS}/${GOARCH}"

if [ "$GOOS" = "windows" ]; then
  node_local_name="aicw-node.exe"
fi

if [ "$GOOS" = "darwin" ] && [ "$GOARCH" = "universal" ]; then
  platform="darwin/universal"
fi

NODE_LOCAL="$GUI_DIR/$node_local_name"

if [ -n "${GITHUB_REF_NAME:-}" ] && [[ "$GITHUB_REF_NAME" == v* ]]; then
  PRODUCT_VERSION="${GITHUB_REF_NAME#v}"
  if command -v node >/dev/null 2>&1; then
    node - "$WAILS_JSON" "$PRODUCT_VERSION" <<'EOF'
const fs = require("fs");
const [file, version] = process.argv.slice(2);
const json = JSON.parse(fs.readFileSync(file, "utf8"));
json.info.productVersion = version;
fs.writeFileSync(file, JSON.stringify(json, null, 2) + "\n");
EOF
  fi
elif command -v node >/dev/null 2>&1; then
  PRODUCT_VERSION="$(node - "$WAILS_JSON" <<'EOF'
const fs = require("fs");
const json = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
process.stdout.write(json.info.productVersion || "0.0.0");
EOF
)"
else
  PRODUCT_VERSION="0.0.0"
fi

VERSION_TAG="v${PRODUCT_VERSION}"
windows_installer_name="aicw-node-setup-${VERSION_TAG}-windows-amd64-installer.exe"
linux_zip_name="aicw-node-setup-${VERSION_TAG}-linux-amd64.zip"
darwin_zip_name="aicw-node-setup-${VERSION_TAG}-darwin-universal.app.zip"
linux_binary_name="aicw-node-setup-linux-amd64"

if [ "$GOOS" = "darwin" ] && [ "$GOARCH" = "universal" ]; then
  SETUP_DIST="$DIST_DIR/$darwin_zip_name"
elif [ "$GOOS" = "linux" ] && [ "$TARGET_GOARCH" = "amd64" ]; then
  SETUP_DIST="$DIST_DIR/$linux_binary_name"
else
  SETUP_DIST="$DIST_DIR/aicw-node-setup-${GOOS}-${TARGET_GOARCH}"
  if [ "$GOOS" = "windows" ]; then
    SETUP_DIST="${SETUP_DIST}.exe"
  fi
fi

GUI_LDFLAGS="-s -w"
if [ -n "${PRODUCT_VERSION:-}" ]; then
  GUI_LDFLAGS="-X main.guiVersion=${PRODUCT_VERSION} ${GUI_LDFLAGS}"
fi

echo "==> Product version ${VERSION_TAG}"
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
wails_build_args=(-platform "$platform" -clean -skipbindings -ldflags "$GUI_LDFLAGS")
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
  go_tags="production"
  if [ "$GOOS" = "linux" ]; then
    go_tags="production,webkit2_41"
  fi
  if [ "$GOOS" = "darwin" ] && [ "$target_goarch" = "universal" ]; then
    GOARCH=arm64
  fi
  ldflags="-H windowsgui ${GUI_LDFLAGS}"
  if [ "$GOOS" != "windows" ]; then
    ldflags="${GUI_LDFLAGS}"
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
  rm -f "$DIST_DIR/$linux_zip_name"
  (cd "$DIST_DIR" && zip -j -q "$linux_zip_name" "$linux_binary_name" "$node_local_name")
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
if [ -f "$SETUP_DIST" ] && [ "$GOOS" != "linux" ]; then
  echo "  $SETUP_DIST"
fi
if [ -f "$DIST_DIR/$linux_zip_name" ]; then
  echo "  $DIST_DIR/$linux_zip_name"
fi
