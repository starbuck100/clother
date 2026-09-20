//go:build windows

package launchers

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/platform"
	"github.com/jolehuit/clother/internal/providers"
)

func TestLauncherHoldOpen(t *testing.T) {
	if os.Getenv("CLOTHER_TEST_HOLD_OPEN") != "1" {
		return
	}
	fmt.Println("ready")
	var b [1]byte
	_, _ = os.Stdin.Read(b[:])
	os.Exit(0)
}

func TestSyncReplacesRunningWindowsLauncher(t *testing.T) {
	root := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}
	paths := testPaths(root)
	cfg := &config.File{}
	if err := Sync(self, paths, catalog, cfg, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(paths.BinDir, platform.LauncherName("openrouter"))
	cmd := exec.Command(launcher, "-test.run=^TestLauncherHoldOpen$")
	cmd.Env = append(os.Environ(), "CLOTHER_TEST_HOLD_OPEN=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	}()
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != "ready" {
		t.Fatal("launcher helper did not start")
	}
	replacement := filepath.Join(root, "new.exe")
	writeLargeStub(t, replacement)
	if err := Sync(replacement, paths, catalog, cfg, SyncOptions{}); err != nil {
		t.Fatalf("update while launcher is running: %v", err)
	}
	if !platform.SameFile(launcher, filepath.Join(paths.BinDir, platform.BinaryName())) {
		t.Fatal("launcher still points to the old binary")
	}
}
