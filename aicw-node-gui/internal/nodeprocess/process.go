package nodeprocess

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/aicw/aicw_node/aicw-node-gui/internal/install"
)

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

const MaxConcurrentNodes = 5

type nodeProcess struct {
	cmd *exec.Cmd
}

type Manager struct {
	mu sync.Mutex
	// processes tracks live processes only; entries disappear on exit.
	processes map[string]*nodeProcess
	// logs outlives processes so a node that exits immediately still reports why.
	logs    map[string][]string
	maxLogs int
}

func NewManager() *Manager {
	return &Manager{
		processes: map[string]*nodeProcess{},
		logs:      map[string][]string{},
		maxLogs:   400,
	}
}

func runDir(installDir string) string {
	return filepath.Join(installDir, "run")
}

func pidFilePath(installDir, nodeName string) string {
	return filepath.Join(runDir(installDir), nodeName+".pid")
}

func writePIDFile(installDir, nodeName string, pid int) {
	if installDir == "" || nodeName == "" {
		return
	}
	if err := os.MkdirAll(runDir(installDir), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(pidFilePath(installDir, nodeName), []byte(strconv.Itoa(pid)), 0o644)
}

func removePIDFile(installDir, nodeName string) {
	if installDir == "" || nodeName == "" {
		return
	}
	_ = os.Remove(pidFilePath(installDir, nodeName))
}

func livePIDsByNode(installDir string) map[string]int {
	out := map[string]int{}
	if installDir == "" {
		return out
	}
	entries, err := os.ReadDir(runDir(installDir))
	if err != nil {
		return out
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".pid") {
			continue
		}
		nodeName := strings.TrimSuffix(entry.Name(), ".pid")
		raw, err := os.ReadFile(pidFilePath(installDir, nodeName))
		if err != nil {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
		if err != nil || !processAlive(pid) {
			removePIDFile(installDir, nodeName)
			continue
		}
		out[nodeName] = pid
	}
	return out
}

func (m *Manager) runningNames(installDir string) map[string]bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.runningNamesLocked(installDir)
}

func (m *Manager) runningNamesLocked(installDir string) map[string]bool {
	out := map[string]bool{}
	for name := range m.processes {
		out[name] = true
	}
	for name := range livePIDsByNode(installDir) {
		out[name] = true
	}
	return out
}

// DiscoverRunningNodeNames returns node names with a live aicw-node process under
// installDir, including processes started by an earlier GUI session.
func DiscoverRunningNodeNames(installDir string) []string {
	// Used before Manager exists; scan pid files only.
	live := livePIDsByNode(installDir)
	names := make([]string, 0, len(live))
	for name := range live {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (m *Manager) RunningNodeNames(installDir string) []string {
	names := m.runningNames(installDir)
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (m *Manager) RunningCount(installDir string) int {
	return len(m.runningNames(installDir))
}

func (m *Manager) IsNodeRunning(installDir, nodeName string) bool {
	return m.runningNames(installDir)[nodeName]
}

func (m *Manager) RunningNodeName() string {
	// Backward-compatible helper: returns one running name if any.
	m.mu.Lock()
	defer m.mu.Unlock()
	for name := range m.processes {
		return name
	}
	return ""
}

func (m *Manager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.processes) > 0
}

func (m *Manager) ResolveRunningNodeName(installDir string) string {
	names := m.RunningNodeNames(installDir)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

func (m *Manager) Logs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.allLogsLocked()
}

func (m *Manager) LogsForNode(nodeName string) []string {
	nodeName = strings.TrimSpace(nodeName)
	m.mu.Lock()
	defer m.mu.Unlock()
	if nodeName == "" {
		return m.allLogsLocked()
	}
	lines := m.logs[nodeName]
	out := make([]string, len(lines))
	copy(out, lines)
	return out
}

func (m *Manager) allLogsLocked() []string {
	names := make([]string, 0, len(m.logs))
	for name := range m.logs {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []string
	for _, name := range names {
		for _, line := range m.logs[name] {
			out = append(out, fmt.Sprintf("[%s] %s", name, line))
		}
	}
	return out
}

func (m *Manager) appendLog(nodeName, line string) {
	if strings.TrimSpace(nodeName) == "" {
		nodeName = "gui"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	lines := append(m.logs[nodeName], line)
	if len(lines) > m.maxLogs {
		lines = lines[len(lines)-m.maxLogs:]
	}
	m.logs[nodeName] = lines
}

func (m *Manager) Start(installDir, nodeName string) error {
	nodeName = strings.TrimSpace(nodeName)
	if nodeName == "" {
		return fmt.Errorf("node name is required")
	}

	m.mu.Lock()
	if m.runningNamesLocked(installDir)[nodeName] {
		m.mu.Unlock()
		return nil
	}
	if len(m.runningNamesLocked(installDir)) >= MaxConcurrentNodes {
		m.mu.Unlock()
		return fmt.Errorf("maximum %d nodes can run at once", MaxConcurrentNodes)
	}
	if _, exists := m.processes[nodeName]; exists {
		m.mu.Unlock()
		return fmt.Errorf("node %q is already running", nodeName)
	}
	m.mu.Unlock()

	binary := filepath.Join(installDir, install.NodeBinaryName())
	args := []string{
		"start",
		"--name", nodeName,
		"--network-config", filepath.Join(installDir, "network-config.yaml"),
		"--password-file", filepath.Join(installDir, "password.txt"),
		"--identity-dir", filepath.Join(installDir, "identity"),
	}
	operatorConfig := filepath.Join(installDir, "operator-config.yaml")
	if info, err := os.Stat(operatorConfig); err == nil && !info.IsDir() {
		args = append(args, "--config", operatorConfig)
	}

	cmd := exec.Command(binary, args...)
	cmd.Dir = installDir
	cmd.Env = append(os.Environ(), "NO_COLOR=1")
	configureHiddenProcess(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	m.mu.Lock()
	m.processes[nodeName] = &nodeProcess{cmd: cmd}
	m.mu.Unlock()

	writePIDFile(installDir, nodeName, cmd.Process.Pid)
	m.appendLog(nodeName, fmt.Sprintf("[gui] started %s %v", binary, args))

	go m.consume(nodeName, stdout)
	go m.consume(nodeName, stderr)
	go func() {
		err := cmd.Wait()
		m.mu.Lock()
		delete(m.processes, nodeName)
		m.mu.Unlock()
		removePIDFile(installDir, nodeName)
		if err != nil {
			m.appendLog(nodeName, fmt.Sprintf("[gui] node exited: %v", err))
		} else {
			m.appendLog(nodeName, "[gui] node exited cleanly")
		}
	}()

	return nil
}

func (m *Manager) consume(nodeName string, reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := strings.TrimSpace(stripANSI(scanner.Text()))
		if line == "" {
			continue
		}
		m.appendLog(nodeName, line)
	}
}

func stripANSI(s string) string {
	return ansiEscape.ReplaceAllString(s, "")
}

func (m *Manager) Stop(installDir, nodeName string) error {
	nodeName = strings.TrimSpace(nodeName)

	m.mu.Lock()
	tracked := map[string]int{}
	for name, proc := range m.processes {
		if nodeName == "" || nodeName == name {
			if proc.cmd != nil && proc.cmd.Process != nil {
				tracked[name] = proc.cmd.Process.Pid
			}
		}
	}
	m.mu.Unlock()

	if nodeName == "" {
		m.appendLog("", "[gui] stopping all nodes…")
	} else {
		m.appendLog(nodeName, fmt.Sprintf("[gui] stopping %s…", nodeName))
	}

	targets := map[int]string{}
	for name, pid := range tracked {
		targets[pid] = name
	}
	for name, pid := range livePIDsByNode(installDir) {
		if nodeName == "" || nodeName == name {
			if _, ok := targets[pid]; !ok {
				targets[pid] = name
			}
		}
	}
	if nodeName == "" || len(targets) == 0 {
		for _, pid := range findNodeBinaryPIDs(installDir) {
			if _, ok := targets[pid]; !ok {
				targets[pid] = ""
			}
		}
	}

	var lastErr error
	for pid, name := range targets {
		if err := terminateProcess(pid); err != nil && lastErr == nil {
			lastErr = err
		}
		if name != "" {
			removePIDFile(installDir, name)
			m.mu.Lock()
			delete(m.processes, name)
			m.mu.Unlock()
		}
	}

	return lastErr
}

func (m *Manager) StopAll(installDir string) error {
	return m.Stop(installDir, "")
}
