package install

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type State struct {
	Installed   bool   `json:"installed"`
	InstallDir  string `json:"installDir"`
	InstalledAt string `json:"installedAt,omitempty"`
	Version     string `json:"version,omitempty"`
}

func LoadState(path string) (*State, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{}, nil
		}
		return nil, err
	}
	var state State
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func SaveState(path string, state *State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func FindBundledNodeBinary(exePath string) string {
	dir := filepath.Dir(exePath)
	name := NodeBinaryName()

	candidates := []string{
		filepath.Join(dir, name),
		filepath.Join(dir, "aicw-node"),
		filepath.Join(dir, "aicw-node.exe"),
		filepath.Join(dir, NodeReleaseAssetName("windows", "amd64")),
		filepath.Join(dir, NodeReleaseAssetName("linux", "amd64")),
		filepath.Join(dir, NodeReleaseAssetName("linux", "arm64")),
		filepath.Join(dir, NodeReleaseAssetName("darwin", "amd64")),
		filepath.Join(dir, NodeReleaseAssetName("darwin", "arm64")),
		filepath.Join(dir, "aicw-node-darwin-universal"),
		// macOS .app bundle (engine copied into Contents/MacOS during build)
		filepath.Join(dir, "..", "Resources", name),
		filepath.Join(dir, "..", "Resources", "aicw-node"),
		filepath.Join(dir, "..", name),
		filepath.Join(dir, "..", "dist", NodeReleaseAssetName("windows", "amd64")),
		filepath.Join(dir, "..", "dist", NodeReleaseAssetName("linux", "amd64")),
		filepath.Join(dir, "..", "dist", NodeReleaseAssetName("linux", "arm64")),
		filepath.Join(dir, "..", "dist", NodeReleaseAssetName("darwin", "amd64")),
		filepath.Join(dir, "..", "dist", NodeReleaseAssetName("darwin", "arm64")),
		filepath.Join(dir, "..", "dist", "aicw-node-darwin-universal"),
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

func Install(sourceBinary, targetDir string) error {
	if sourceBinary == "" {
		return fmt.Errorf("bundled %s was not found next to the installer", NodeBinaryName())
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(targetDir, "identity"), 0o755); err != nil {
		return err
	}

	targetBinary := filepath.Join(targetDir, NodeBinaryName())
	if err := copyFile(sourceBinary, targetBinary); err != nil {
		return err
	}
	return os.Chmod(targetBinary, 0o755)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

type LocalSetup struct {
	InstallDir         string `json:"installDir"`
	NodeBinaryPresent  bool   `json:"nodeBinaryPresent"`
	NetworkConfigFound bool   `json:"networkConfigFound"`
	PasswordFound      bool   `json:"passwordFound"`
	IdentityFound      bool   `json:"identityFound"`
	NodeName           string `json:"nodeName,omitempty"`
	NodeID             string `json:"nodeId,omitempty"`
	PublicKey          string `json:"publicKey,omitempty"`
	ReadyToStart       bool   `json:"readyToStart"`
	MissingItems       []string `json:"missingItems"`
}

type identityFile struct {
	NodeName  string `json:"node_name"`
	NodeID    string `json:"node_id"`
	PublicKey string `json:"public_key"`
}

type SharedSetup struct {
	InstallDir         string   `json:"installDir"`
	NodeBinaryPresent  bool     `json:"nodeBinaryPresent"`
	NetworkConfigFound bool     `json:"networkConfigFound"`
	PasswordFound      bool     `json:"passwordFound"`
	MissingItems       []string `json:"missingItems"`
}

type NodeLocalSetup struct {
	NodeName        string   `json:"nodeName"`
	NodeID          string   `json:"nodeId,omitempty"`
	PublicKey       string   `json:"publicKey,omitempty"`
	IdentityFound   bool     `json:"identityFound"`
	PrivateKeyFound bool     `json:"privateKeyFound"`
	ReadyToStart    bool     `json:"readyToStart"`
	MissingItems    []string `json:"missingItems"`
}

func InspectSharedSetup(installDir string) *SharedSetup {
	setup := &SharedSetup{
		InstallDir:   installDir,
		MissingItems: []string{},
	}
	if info, err := os.Stat(filepath.Join(installDir, NodeBinaryName())); err == nil && !info.IsDir() {
		setup.NodeBinaryPresent = true
	} else {
		setup.MissingItems = append(setup.MissingItems, NodeBinaryName())
	}
	if info, err := os.Stat(filepath.Join(installDir, "network-config.yaml")); err == nil && !info.IsDir() {
		setup.NetworkConfigFound = true
	} else {
		setup.MissingItems = append(setup.MissingItems, "network-config.yaml")
	}
	if info, err := os.Stat(filepath.Join(installDir, "password.txt")); err == nil && !info.IsDir() {
		setup.PasswordFound = true
	} else {
		setup.MissingItems = append(setup.MissingItems, "password.txt")
	}
	return setup
}

func ListNodeLocalSetups(installDir string, shared *SharedSetup) ([]NodeLocalSetup, error) {
	if shared == nil {
		shared = InspectSharedSetup(installDir)
	}
	identityDir := filepath.Join(installDir, "identity")
	entries, err := os.ReadDir(identityDir)
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	var nodes []NodeLocalSetup
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		raw, readErr := os.ReadFile(filepath.Join(identityDir, entry.Name()))
		if readErr != nil {
			continue
		}
		var identity identityFile
		if json.Unmarshal(raw, &identity) != nil || identity.NodeName == "" {
			continue
		}
		if seen[identity.NodeName] {
			continue
		}
		seen[identity.NodeName] = true
		nodes = append(nodes, buildNodeLocalSetup(installDir, shared, identity))
	}
	return nodes, nil
}

func InspectNodeLocalSetup(installDir, nodeName string) (*NodeLocalSetup, error) {
	shared := InspectSharedSetup(installDir)
	identityPath := filepath.Join(installDir, "identity", nodeName+"_identity.json")
	raw, err := os.ReadFile(identityPath)
	if err != nil {
		setup := buildNodeLocalSetup(installDir, shared, identityFile{NodeName: nodeName})
		return &setup, nil
	}
	var identity identityFile
	if json.Unmarshal(raw, &identity) != nil {
		setup := buildNodeLocalSetup(installDir, shared, identityFile{NodeName: nodeName})
		return &setup, nil
	}
	setup := buildNodeLocalSetup(installDir, shared, identity)
	return &setup, nil
}

func buildNodeLocalSetup(installDir string, shared *SharedSetup, identity identityFile) NodeLocalSetup {
	setup := NodeLocalSetup{
		NodeName:     identity.NodeName,
		NodeID:       identity.NodeID,
		PublicKey:    identity.PublicKey,
		MissingItems: append([]string{}, shared.MissingItems...),
	}
	identityPath := filepath.Join(installDir, "identity", identity.NodeName+"_identity.json")
	if info, err := os.Stat(identityPath); err == nil && !info.IsDir() {
		setup.IdentityFound = true
	} else {
		setup.MissingItems = append(setup.MissingItems, "identity/"+identity.NodeName+"_identity.json")
	}
	privateKeyPath := filepath.Join(installDir, "identity", identity.NodeName+"_private_key.txt")
	if info, err := os.Stat(privateKeyPath); err == nil && !info.IsDir() {
		setup.PrivateKeyFound = true
	} else {
		setup.MissingItems = append(setup.MissingItems, "identity/"+identity.NodeName+"_private_key.txt")
	}
	setup.ReadyToStart = shared.NodeBinaryPresent && shared.NetworkConfigFound && shared.PasswordFound && setup.IdentityFound && setup.PrivateKeyFound && setup.NodeName != ""
	return setup
}

func InspectLocalSetup(installDir string) (*LocalSetup, error) {
	shared := InspectSharedSetup(installDir)
	setup := &LocalSetup{
		InstallDir:         installDir,
		NodeBinaryPresent:  shared.NodeBinaryPresent,
		NetworkConfigFound: shared.NetworkConfigFound,
		PasswordFound:      shared.PasswordFound,
		MissingItems:       append([]string{}, shared.MissingItems...),
	}

	identityDir := filepath.Join(installDir, "identity")
	entries, err := os.ReadDir(identityDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
				continue
			}
			raw, readErr := os.ReadFile(filepath.Join(identityDir, entry.Name()))
			if readErr != nil {
				continue
			}
			var identity identityFile
			if json.Unmarshal(raw, &identity) != nil {
				continue
			}
			nodeSetup := buildNodeLocalSetup(installDir, shared, identity)
			setup.IdentityFound = nodeSetup.IdentityFound
			setup.NodeName = identity.NodeName
			setup.NodeID = identity.NodeID
			setup.PublicKey = identity.PublicKey
			setup.MissingItems = nodeSetup.MissingItems
			setup.ReadyToStart = nodeSetup.ReadyToStart
			break
		}
	}
	if !setup.IdentityFound {
		setup.MissingItems = append(setup.MissingItems, "identity/*.json")
	}
	return setup, nil
}

// RemoveNodeLocalSetup deletes local identity, node DB, and pid files for a node.
// The node process must be stopped before calling this.
func RemoveNodeLocalSetup(installDir, nodeName string) error {
	nodeName = strings.TrimSpace(nodeName)
	if nodeName == "" {
		return fmt.Errorf("node name is required")
	}
	if strings.TrimSpace(installDir) == "" {
		return fmt.Errorf("install directory is required")
	}

	identityDir := filepath.Join(installDir, "identity")
	for _, name := range []string{
		nodeName + "_identity.json",
		nodeName + "_private_key.txt",
		nodeName + "_private.key",
	} {
		_ = os.Remove(filepath.Join(identityDir, name))
	}

	dbDir := filepath.Join(installDir, nodeName)
	_ = os.RemoveAll(dbDir)

	pidPath := filepath.Join(installDir, "run", nodeName+".pid")
	_ = os.Remove(pidPath)

	return nil
}
