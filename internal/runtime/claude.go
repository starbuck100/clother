package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/platform"
	"github.com/jolehuit/clother/internal/session"
	"github.com/jolehuit/clother/internal/update"
	"github.com/jolehuit/clother/internal/version"
)

const (
	// shimDepthEnv counts how many clother shims deep the current process is.
	shimDepthEnv = "CLOTHER_SHIM_DEPTH"
	// shimRealEnv records the real Claude Code a previous shim already found,
	// so a nested `claude` subprocess does not have to scan PATH again.
	shimRealEnv = "CLOTHER_REAL_CLAUDE"
	// maxShimDepth is where the chain is declared a loop. Real nesting —
	// claude spawning claude — does not exceed two.
	maxShimDepth = 3
)

func RunClaudeShim(ctx context.Context, paths config.Paths, args []string) (int, error) {
	// Last line of defence. The file-identity checks in FindRealClaude cover
	// every layout we know of, but this one does not depend on them being
	// right: it terminates the chain whatever the filesystem looks like, so a
	// mis-detected shim can never become a fork bomb.
	if depth := shimDepth(); depth >= maxShimDepth {
		return 1, fmt.Errorf(
			"clother's `claude` shim has been launched recursively (%s=%d); refusing to continue. Run `clother install` to repair the launchers in %s",
			shimDepthEnv, depth, paths.BinDir)
	}

	args = NormalizeClaudeArgs(args)
	if isTTY(os.Stderr) && !IsHomebrew() {
		if message, err := update.MaybeMessage(paths, version.Value, time.Now()); err == nil && message != "" {
			fmt.Fprintln(os.Stderr, message)
		}
	}
	claudePath, err := FindRealClaude(paths)
	if err != nil {
		return 1, err
	}
	if err := session.RestoreStale(paths); err != nil {
		return 1, err
	}
	env := shimEnv(os.Environ(), claudePath)
	if code, handled, err := runWithTemporaryPatch(ctx, claudePath, paths, args, env, ""); handled {
		return code, err
	}
	return runClaudeCommand(ctx, claudePath, args, env, "")
}

// FindRealClaude locates the genuine Claude Code binary, skipping clother's own
// shim wherever it appears on PATH.
//
// The skip is the dangerous part of this package. Clother installs a `claude`
// entry that leads back to the clother binary, and if that entry were mistaken
// for a real installation the shim would exec itself forever. On Unix a symlink
// resolves back to clother and the skip is easy; on Windows the shim is a copy
// (see launchers.Sync for why), which shares no file identity with clother at
// all — hence platform.SameInstallation, which also compares sizes there.
func FindRealClaude(paths config.Paths) (string, error) {
	// A shim further up the chain already resolved the real binary. Trusting it
	// skips the PATH scan, and cannot loop: the process that recorded it would
	// have refused to start this one if the chain were already too deep.
	if recorded := strings.TrimSpace(os.Getenv(shimRealEnv)); recorded != "" {
		if info, err := os.Stat(recorded); err == nil && !info.IsDir() && !isClotherSelf(recorded, paths) {
			return recorded, nil
		}
	}

	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}
		// On Windows a bare `claude` is not executable — the loader needs the
		// extension — so every PATHEXT spelling is tried. On Unix this is the
		// single name it always was.
		for _, name := range platform.ExecutableNames("claude") {
			candidate := filepath.Join(dir, name)
			info, err := os.Stat(candidate)
			if err != nil || info.IsDir() {
				continue
			}
			if isClotherSelf(candidate, paths) {
				continue
			}
			return candidate, nil
		}
	}

	fallback := filepath.Join(paths.BinDir, platform.RealClaudeName())
	if info, err := os.Stat(fallback); err == nil && !info.IsDir() && !isClotherSelf(fallback, paths) {
		return fallback, nil
	}
	return "", fmt.Errorf("could not locate real claude; ensure `claude` is in PATH or `%s` exists", fallback)
}

// PreserveRealClaude moves a real Claude Code out of the way before clother's
// shim takes its name, so the shim can still find the original afterwards.
//
// Only a real install sitting at exactly the shim's own path is touched, and
// only when it shares the shim's extension: on Windows an npm `claude.cmd` is
// left alone, because clother's `claude.exe` already shadows it (`.EXE`
// precedes `.CMD` in PATHEXT) and renaming it would only break the npm
// installation without gaining anything.
func PreserveRealClaude(paths config.Paths, realClaudePath string) error {
	if realClaudePath == "" {
		return nil
	}
	defaultClaude := filepath.Join(paths.BinDir, platform.ClaudeName())
	if !platform.SameFile(realClaudePath, defaultClaude) {
		return nil
	}

	preserved := filepath.Join(paths.BinDir, platform.RealClaudeName())
	if platform.SameFile(defaultClaude, preserved) {
		return nil
	}

	if _, err := os.Stat(preserved); err == nil {
		if err := os.Remove(preserved); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Rename(defaultClaude, preserved)
}

// IsPreservedClaude reports whether a real Claude Code was moved aside by an
// earlier install and is still sitting in the bin directory, waiting to be put
// back by uninstall.
func IsPreservedClaude(paths config.Paths) bool {
	_, err := os.Stat(filepath.Join(paths.BinDir, platform.RealClaudeName()))
	return err == nil
}

// RestorePreservedClaude undoes PreserveRealClaude: the preserved binary goes
// back to the name `claude` now occupies. It is a no-op unless the shim is
// actually there, so it can never overwrite a real install the user made after
// uninstalling clother by hand.
func RestorePreservedClaude(paths config.Paths) (bool, error) {
	preserved := filepath.Join(paths.BinDir, platform.RealClaudeName())
	if _, err := os.Stat(preserved); err != nil {
		return false, nil
	}
	defaultClaude := filepath.Join(paths.BinDir, platform.ClaudeName())
	if _, err := os.Stat(defaultClaude); err == nil {
		// Something already holds the name. Only clother's own shim may be
		// displaced: a Claude Code the user installed since is theirs and wins.
		if !isClotherSelf(defaultClaude, paths) {
			return false, nil
		}
		if err := os.Remove(defaultClaude); err != nil {
			return false, err
		}
	}
	if err := os.Rename(preserved, defaultClaude); err != nil {
		return false, err
	}
	return true, nil
}

// isClotherSelf reports whether candidate is clother itself rather than a real
// Claude Code installation — as a symlink, a hardlink, or a byte-identical copy.
func isClotherSelf(candidate string, paths config.Paths) bool {
	if candidate == "" {
		return false
	}
	self, _ := os.Executable()
	for _, own := range []string{self, filepath.Join(paths.BinDir, platform.BinaryName())} {
		if own == "" {
			continue
		}
		if platform.SameInstallation(candidate, own) {
			return true
		}
	}
	return false
}

// shimEnv derives the environment for the Claude Code process: one level deeper
// in the chain, and told which binary to use so it does not look again.
func shimEnv(env []string, claudePath string) []string {
	depth := strconv.Itoa(shimDepth() + 1)
	env = setEnvVar(env, shimDepthEnv, depth)
	return setEnvVar(env, shimRealEnv, claudePath)
}

func shimDepth() int {
	depth, err := strconv.Atoi(strings.TrimSpace(os.Getenv(shimDepthEnv)))
	if err != nil || depth < 0 {
		return 0
	}
	return depth
}

// setEnvVar replaces key in a KEY=VALUE environment slice, or appends it.
// Replacing rather than appending keeps the slice free of the duplicate keys
// that os/exec resolves by an unspecified order on Windows.
func setEnvVar(env []string, key, value string) []string {
	prefix := key + "="
	for i, pair := range env {
		if strings.HasPrefix(strings.ToUpper(pair), strings.ToUpper(prefix)) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}
