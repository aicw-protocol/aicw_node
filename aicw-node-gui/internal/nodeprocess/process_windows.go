//go:build windows

package nodeprocess

import (
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const createNoWindow = 0x08000000

const nodeBinaryName = "aicw-node.exe"

func configureHiddenProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}

// processAlive reports whether pid belongs to a live aicw-node process.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)

	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}
	const stillActive = 259
	if code != stillActive {
		return false
	}
	return strings.EqualFold(filepath.Base(processImagePath(handle)), nodeBinaryName)
}

func processImagePath(handle windows.Handle) string {
	buf := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(handle, 0, &buf[0], &size); err != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:size])
}

func terminateProcess(pid int) error {
	if pid <= 0 {
		return nil
	}
	handle, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return nil // already gone or not accessible
	}
	defer windows.CloseHandle(handle)
	return windows.TerminateProcess(handle, 1)
}

// findNodeBinaryPIDs enumerates live aicw-node.exe processes launched from installDir.
// It uses a process snapshot (no shell, no console window).
func findNodeBinaryPIDs(installDir string) []int {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snapshot)

	wantDir := ""
	if installDir != "" {
		wantDir = strings.ToLower(filepath.Clean(installDir))
	}

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil
	}

	var pids []int
	for {
		name := windows.UTF16ToString(entry.ExeFile[:])
		if strings.EqualFold(name, nodeBinaryName) {
			pid := int(entry.ProcessID)
			if wantDir == "" || processDirMatches(pid, wantDir) {
				pids = append(pids, pid)
			}
		}
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			break
		}
	}
	return pids
}

func processDirMatches(pid int, wantDirLower string) bool {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)

	path := processImagePath(handle)
	if path == "" {
		return false
	}
	return strings.ToLower(filepath.Clean(filepath.Dir(path))) == wantDirLower
}
