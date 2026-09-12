package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/platform"
)

// isolatePath makes PATH consist of exactly the given directories, so a lookup
// cannot reach a real Claude Code installed on the machine running the test.
func isolatePath(t *testing.T, dirs ...string) {
	t.Helper()
	t.Setenv("PATH", strings.Join(dirs, string(os.PathListSeparator)))
	// A recorded path from an outer shim would short-circuit the PATH scan this
	// test is exercising.
	t.Setenv(shimRealEnv, "")
	t.Setenv(shimDepthEnv, "")
}

func writeClaudeStub(t *testing.T, dir string, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, platform.ClaudeName())
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFindRealClaudeCanUseSameBinDir(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "clother-bin")
	realDir := filepath.Join(root, "real-bin")

	// The first entry on PATH wins, even when it is clother's own directory:
	// a Claude Code the user keeps there is theirs, not the shim.
	want := writeClaudeStub(t, binDir, "real-claude-binary")
	writeClaudeStub(t, realDir, "other-claude-binary")
	isolatePath(t, binDir, realDir)

	got, err := FindRealClaude(config.Paths{BinDir: filepath.Join(root, "unused")})
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("FindRealClaude() = %q, want %q", got, want)
	}
}

func TestFindRealClaudeSkipsSelfAndFallsBack(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// The installed shim, in the shape this platform installs it: a symlink on
	// Unix, a hardlink or copy on Windows.
	if err := platform.LinkOrCopy(self, filepath.Join(binDir, platform.ClaudeName())); err != nil {
		t.Fatal(err)
	}
	realFallback := filepath.Join(binDir, platform.RealClaudeName())
	if err := os.WriteFile(realFallback, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	isolatePath(t, binDir)

	got, err := FindRealClaude(config.Paths{BinDir: binDir})
	if err != nil {
		t.Fatal(err)
	}
	if got != realFallback {
		t.Fatalf("FindRealClaude() = %q, want %q", got, realFallback)
	}
}

func TestFindRealClaudeTrustsARecordedPath(t *testing.T) {
	root := t.TempDir()
	recorded := writeClaudeStub(t, filepath.Join(root, "elsewhere"), "real-claude-binary")
	isolatePath(t, filepath.Join(root, "empty"))

	t.Setenv(shimRealEnv, recorded)
	got, err := FindRealClaude(config.Paths{BinDir: filepath.Join(root, "bin")})
	if err != nil {
		t.Fatal(err)
	}
	if got != recorded {
		t.Fatalf("FindRealClaude() = %q, want the recorded %q", got, recorded)
	}
}

func TestFindRealClaudeIgnoresARecordedClotherPath(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(binDir, platform.ClaudeName())
	if err := platform.LinkOrCopy(self, shim); err != nil {
		t.Fatal(err)
	}

	// A recorded path that is clother itself must be discarded, not trusted:
	// otherwise a stale value in the environment would start the loop the
	// identity checks exist to prevent.
	isolatePath(t, binDir)
	t.Setenv(shimRealEnv, shim)

	if _, err := FindRealClaude(config.Paths{BinDir: binDir}); err == nil {
		t.Fatal("FindRealClaude() accepted clother's own shim as the real Claude Code")
	}
}

func TestRunClaudeShimRefusesToRecurse(t *testing.T) {
	for _, depth := range []string{"3", "4", "99"} {
		t.Setenv(shimDepthEnv, depth)
		_, err := RunClaudeShim(context.Background(), config.Paths{BinDir: t.TempDir()}, []string{"--version"})
		if err == nil {
			t.Fatalf("RunClaudeShim() with %s=%s returned no error; a recursive shim would never terminate", shimDepthEnv, depth)
		}
	}
}

func TestShimDepthIgnoresGarbage(t *testing.T) {
	for _, value := range []string{"", "-1", "abc", "  "} {
		t.Setenv(shimDepthEnv, value)
		if got := shimDepth(); got != 0 {
			t.Fatalf("shimDepth() with %q = %d, want 0", value, got)
		}
	}
}

func TestShimEnvDeepensTheChain(t *testing.T) {
	t.Setenv(shimDepthEnv, "1")
	env := shimEnv([]string{"PATH=/usr/bin"}, "/usr/bin/claude")

	if got := envSliceToMap(env)[shimDepthEnv]; got != "2" {
		t.Fatalf("%s = %q, want 2", shimDepthEnv, got)
	}
	if got := envSliceToMap(env)[shimRealEnv]; got != "/usr/bin/claude" {
		t.Fatalf("%s = %q, want the resolved path", shimRealEnv, got)
	}
	if got := envSliceToMap(env)["PATH"]; got != "/usr/bin" {
		t.Fatalf("PATH = %q, want it preserved", got)
	}

	// A stale value already in the slice is replaced, not appended to: duplicate
	// keys resolve by an unspecified order in os/exec on Windows, so a leftover
	// depth could win and let the chain run one hop longer than it should.
	env = shimEnv([]string{"PATH=/usr/bin", shimDepthEnv + "=99"}, "/usr/bin/claude")
	count := 0
	for _, pair := range env {
		if strings.HasPrefix(strings.ToUpper(pair), strings.ToUpper(shimDepthEnv+"=")) {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("env holds %d entries for %s, want exactly 1: %v", count, shimDepthEnv, env)
	}
	if got := envSliceToMap(env)[shimDepthEnv]; got != "2" {
		t.Fatalf("%s = %q, want the stale 99 replaced by 2", shimDepthEnv, got)
	}
}

func TestSetEnvVarReplacesCaseInsensitively(t *testing.T) {
	env := setEnvVar([]string{"path=/usr/bin"}, "PATH", "/bin")
	if len(env) != 1 {
		t.Fatalf("env = %v, want the existing entry replaced rather than duplicated", env)
	}
	if env[0] != "PATH=/bin" {
		t.Fatalf("env[0] = %q, want PATH=/bin", env[0])
	}
}

func TestPreserveRealClaudeMovesClaudeToClaudeReal(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	claudePath := filepath.Join(binDir, platform.ClaudeName())
	content := []byte("real-claude-binary")
	if err := os.WriteFile(claudePath, content, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := PreserveRealClaude(config.Paths{BinDir: binDir}, claudePath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(claudePath); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be moved, stat err=%v", claudePath, err)
	}
	preserved := filepath.Join(binDir, platform.RealClaudeName())
	got, err := os.ReadFile(preserved)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("preserved content mismatch: got %q", string(got))
	}

	// Uninstall's half of the contract: the original goes back to its own name.
	restored, err := RestorePreservedClaude(config.Paths{BinDir: binDir})
	if err != nil {
		t.Fatal(err)
	}
	if !restored {
		t.Fatal("RestorePreservedClaude() reported nothing to restore")
	}
	got, err = os.ReadFile(claudePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("restored content mismatch: got %q", string(got))
	}
}

func TestRestorePreservedClaudeLeavesAUsersOwnInstallAlone(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, platform.RealClaudeName()), []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Something that is not clother now holds the name `claude`. It wins.
	mine := []byte("a claude code installed after uninstalling clother")
	if err := os.WriteFile(filepath.Join(binDir, platform.ClaudeName()), mine, 0o755); err != nil {
		t.Fatal(err)
	}

	restored, err := RestorePreservedClaude(config.Paths{BinDir: binDir})
	if err != nil {
		t.Fatal(err)
	}
	if restored {
		t.Fatal("RestorePreservedClaude() overwrote a Claude Code the user installed themselves")
	}
	got, err := os.ReadFile(filepath.Join(binDir, platform.ClaudeName()))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(mine) {
		t.Fatalf("claude content = %q, want the user's own binary", string(got))
	}
}
