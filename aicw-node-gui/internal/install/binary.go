package install

import "runtime"

// NodeBinaryName returns the node engine filename inside the install folder.
func NodeBinaryName() string {
	if runtime.GOOS == "windows" {
		return "aicw-node.exe"
	}
	return "aicw-node"
}

// NodeReleaseAssetName matches GitHub release CLI binaries (see .github/workflows/release.yml).
func NodeReleaseAssetName(goos, goarch string) string {
	if goos == "windows" {
		return "aicw-node-" + goos + "-" + goarch + ".exe"
	}
	return "aicw-node-" + goos + "-" + goarch
}

// GUIReleaseAssetName is the desktop installer/launcher filename per platform.
func GUIReleaseAssetName(goos, goarch string) string {
	if goos == "windows" {
		return "aicw-node-setup-" + goos + "-" + goarch + ".exe"
	}
	if goos == "darwin" && goarch == "universal" {
		return "aicw-node-setup-darwin-universal.app.zip"
	}
	return "aicw-node-setup-" + goos + "-" + goarch
}
