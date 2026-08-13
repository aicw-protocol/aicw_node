package config

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	AppFolderName   = "AICW Node"
	DefaultWebBase  = "https://node.aicw.ai"
	StateFileName   = "app-state.json"
	SessionFileName = "session.json"
)

func WebBaseURL() string {
	if value := strings.TrimSpace(os.Getenv("AICW_NODE_WEB_URL")); value != "" {
		return strings.TrimRight(value, "/")
	}
	return DefaultWebBase
}

func DefaultInstallDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	return filepath.Join(base, AppFolderName)
}

func DefaultLocalAppDataInstallDir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return DefaultInstallDir()
	}
	return filepath.Join(base, "Programs", AppFolderName)
}
