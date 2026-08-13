//go:build !windows

package nodeprocess

import (
	"os"
	"os/exec"
	"syscall"
)

func configureHiddenProcess(cmd *exec.Cmd) {}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

func terminateProcess(pid int) error {
	if pid <= 0 {
		return nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}
	return proc.Kill()
}

func findNodeBinaryPIDs(installDir string) []int {
	return nil
}
