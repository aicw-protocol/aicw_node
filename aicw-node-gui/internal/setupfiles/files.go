package setupfiles

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
)

const passwordChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func GeneratePassword(length int) (string, error) {
	if length < 16 {
		return "", fmt.Errorf("password length must be at least 16")
	}
	out := make([]byte, length)
	max := big.NewInt(int64(len(passwordChars)))
	for i := range out {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		out[i] = passwordChars[n.Int64()]
	}
	return string(out), nil
}

func BuildOperatorConfigYAML(nodeWebURL string, pingIntervalSeconds int) string {
	baseURL := strings.TrimRight(strings.TrimSpace(nodeWebURL), "/")
	urlLine := `  url: ""  # e.g. http://localhost:4003 or https://node.aicw.ai`
	if baseURL != "" {
		urlLine = fmt.Sprintf(`  url: "%s"`, baseURL)
	}
	if pingIntervalSeconds < 30 {
		pingIntervalSeconds = 90
	}

	return fmt.Sprintf(`# Operator-local overrides (merge with network-config.yaml from your network admin)
db_path: "."
backup_enabled: true
backup_period_seconds: 300
backup_dir: backups
max_concurrent_keygen: 2
max_concurrent_signing: 10

healthcheck:
  enabled: false
  address: "0.0.0.0:8082"

# Report liveness to the AICW node web dashboard (referral selection).
node_web:
  ping_enabled: true
%s
  ping_interval_seconds: %d
`, urlLine, pingIntervalSeconds)
}

type SharedWriteResult struct {
	NetworkConfigCreated bool
	PasswordCreated      bool
	OperatorConfigCreated bool
}

func EnsureSharedFiles(installDir, networkConfigYAML, operatorConfigYAML string) (*SharedWriteResult, error) {
	if strings.TrimSpace(installDir) == "" {
		return nil, fmt.Errorf("install directory is required")
	}
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return nil, err
	}

	result := &SharedWriteResult{}
	networkPath := filepath.Join(installDir, "network-config.yaml")
	if _, err := os.Stat(networkPath); os.IsNotExist(err) {
		if strings.TrimSpace(networkConfigYAML) == "" {
			return nil, fmt.Errorf("network config template is empty")
		}
		if err := os.WriteFile(networkPath, []byte(strings.TrimSpace(networkConfigYAML)+"\n"), 0o644); err != nil {
			return nil, err
		}
		result.NetworkConfigCreated = true
	}

	passwordPath := filepath.Join(installDir, "password.txt")
	if _, err := os.Stat(passwordPath); os.IsNotExist(err) {
		password, err := GeneratePassword(24)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(passwordPath, []byte(password+"\n"), 0o600); err != nil {
			return nil, err
		}
		result.PasswordCreated = true
	}

	operatorPath := filepath.Join(installDir, "operator-config.yaml")
	if _, err := os.Stat(operatorPath); os.IsNotExist(err) {
		if strings.TrimSpace(operatorConfigYAML) == "" {
			return nil, fmt.Errorf("operator config template is empty")
		}
		if err := os.WriteFile(operatorPath, []byte(operatorConfigYAML), 0o644); err != nil {
			return nil, err
		}
		result.OperatorConfigCreated = true
	}

	return result, nil
}
