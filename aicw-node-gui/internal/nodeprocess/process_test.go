package nodeprocess

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

func TestBadgerDBDir_Default(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "operator-config.yaml"), []byte("db_path: \".\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := badgerDBDir(dir, "t01")
	want := filepath.Join(dir, "t01")
	if got != want {
		t.Fatalf("badgerDBDir() = %q, want %q", got, want)
	}
}

func TestCleanupStaleState_DeadPID(t *testing.T) {
	dir := t.TempDir()
	run := filepath.Join(dir, "run")
	if err := os.MkdirAll(run, 0o755); err != nil {
		t.Fatal(err)
	}
	nodeName := "t01"
	pidPath := filepath.Join(run, nodeName+".pid")
	if err := os.WriteFile(pidPath, []byte("999999"), 0o644); err != nil {
		t.Fatal(err)
	}

	dbDir := filepath.Join(dir, nodeName)
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(dbDir, "LOCK")
	if err := os.WriteFile(lockPath, []byte("lock"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "operator-config.yaml"), []byte("db_path: \".\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewManager()
	m.cleanupStaleState(dir, nodeName)

	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("expected stale pid removed, stat err=%v", err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("expected stale LOCK removed, stat err=%v", err)
	}
}

func TestCleanupStaleState_AlivePID(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("alive pid preservation test uses Windows ping.exe renamed as aicw-node.exe")
	}

	dir := t.TempDir()
	run := filepath.Join(dir, "run")
	if err := os.MkdirAll(run, 0o755); err != nil {
		t.Fatal(err)
	}
	nodeName := "t01"

	pingSrc := filepath.Join(os.Getenv("SystemRoot"), "System32", "ping.exe")
	fakeNode := filepath.Join(dir, "aicw-node.exe")
	if err := copyFile(pingSrc, fakeNode); err != nil {
		t.Skipf("ping.exe unavailable: %v", err)
	}

	cmd := exec.Command(fakeNode, "127.0.0.1", "-n", "30")
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	pidPath := filepath.Join(run, nodeName+".pid")
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(cmd.Process.Pid)), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewManager()
	m.cleanupStaleState(dir, nodeName)

	if _, err := os.Stat(pidPath); err != nil {
		t.Fatalf("alive aicw-node pid file must remain: %v", err)
	}
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
	_, err = io.Copy(out, in)
	return err
}
