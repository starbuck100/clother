package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/launchers"
	"github.com/jolehuit/clother/internal/platform"
	"github.com/jolehuit/clother/internal/runtime"
	"github.com/jolehuit/clother/internal/update"
	"github.com/jolehuit/clother/internal/version"
)

var downloadLatestBinary = update.DownloadLatestIfNewer

func runInstall(ctx context.Context, c Context) (int, error) {
	isHomebrew := runtime.IsHomebrew()
	installShim := !c.Options.NoShim

	var execPath, installedVersion string
	var cleanup func()
	var err error

	if isHomebrew {
		// Homebrew manages the binary lifecycle; skip downloading and copying.
		// os.Executable() returns the stable /opt/homebrew/bin/clother symlink
		// path, so symlinks remain valid after `brew upgrade` without re-running
		// `clother install`.
		execPath, err = os.Executable()
		if err != nil {
			return 1, err
		}
		installedVersion = update.DisplayVersion(version.Value)
	} else {
		execPath, installedVersion, cleanup, err = resolveInstallBinary(ctx)
		if cleanup != nil {
			defer cleanup()
		}
		if err != nil {
			c.Output.Warn("could not fetch latest release; installing current binary instead: %v", err)
		}
	}

	realClaude, claudeErr := runtime.FindRealClaude(c.Paths)
	if claudeErr != nil && installShim {
		if platform.IsWindows {
			c.Output.Warn("Claude Code not found; the provider launchers will still be created, but the `claude` shim cannot be — install Claude Code (native installer or `npm install -g @anthropic-ai/claude-code`) and run `clother install` again")
		} else {
			c.Output.Warn("claude not found; provider symlinks will be created but the `claude` shim will be skipped — run `clother install` again after installing Claude Code")
		}
	}
	if installShim {
		if err := runtime.PreserveRealClaude(c.Paths, realClaude); err != nil {
			return 1, err
		}
	}
	if err := c.Paths.EnsureBaseDirs(); err != nil {
		return 1, err
	}
	config.NormalizeLegacySecrets(c.Secrets, c.Catalog)
	if err := config.SaveConfig(c.Paths.ConfigFile, c.Config); err != nil {
		return 1, err
	}
	if err := config.SaveSecrets(c.Paths.SecretsFile, c.Secrets); err != nil {
		return 1, err
	}
	if err := launchers.Sync(execPath, c.Paths, c.Catalog, c.Config, launchers.SyncOptions{
		SkipCopy:          isHomebrew,
		InstallClaudeShim: installShim,
	}); err != nil {
		return 1, err
	}
	for _, legacy := range []string{
		filepath.Join(c.Paths.DataDir, "clother-full.sh"),
		filepath.Join(c.Paths.DataDir, "banner"),
	} {
		_ = os.Remove(legacy)
	}
	if !installShim {
		// Nothing took the name `claude`, but an earlier install may have: put
		// back the Claude Code that install moved aside, and say so. The call
		// refuses to displace anything that is not clother's own shim, so a real
		// install the user made since is never overwritten.
		if restored, err := runtime.RestorePreservedClaude(c.Paths); err == nil && restored {
			c.Output.Line("restored the original %s (--no-shim was given)", platform.ClaudeName())
		}
		c.Output.Success("installed Clother %s to %s (without the `claude` shim)", installedVersion, c.Paths.BinDir)
		if !pathContainsDir(os.Getenv("PATH"), c.Paths.BinDir) {
			c.Output.Warn("%s", pathHint(c.Paths.BinDir))
		}
		return 0, nil
	}
	// The shim has just taken the name `claude` on PATH. If a real Claude Code
	// could be found before this ran and cannot be found now, launching the
	// shim would fail — so the shim is withdrawn again rather than left in
	// place, and the caller is told what happened instead of being handed a
	// broken install that looks successful.
	if claudeErr == nil {
		if _, err := runtime.FindRealClaude(c.Paths); err != nil {
			if shim, shimErr := os.Stat(filepath.Join(c.Paths.BinDir, platform.ClaudeName())); shimErr == nil && !shim.IsDir() {
				_ = os.Remove(filepath.Join(c.Paths.BinDir, platform.ClaudeName()))
			}
			if restored, restoreErr := runtime.RestorePreservedClaude(c.Paths); restoreErr == nil && restored {
				c.Output.Warn("put the original %s back", platform.ClaudeName())
			}
			return 1, fmt.Errorf("the `claude` shim could not be verified after installing, so it was withdrawn: %w", err)
		}
	}

	c.Output.Success("installed Clother %s to %s", installedVersion, c.Paths.BinDir)
	if !pathContainsDir(os.Getenv("PATH"), c.Paths.BinDir) {
		c.Output.Warn("%s", pathHint(c.Paths.BinDir))
	}
	return 0, nil
}

// pathHint tells the user how to get BinDir onto PATH in the idiom of their
// platform. setx is deliberately not suggested on Windows: it truncates PATH at
// 1024 characters and silently drops the rest.
func pathHint(dir string) string {
	if platform.IsWindows {
		return fmt.Sprintf("%s is not on PATH; add it to your user PATH under Settings > Environment Variables (or re-run the installer) and open a new terminal", dir)
	}
	return fmt.Sprintf("%s is not on PATH; add `export PATH=\"%s:$PATH\"` to your shell profile and restart your shell", dir, dir)
}

func resolveInstallBinary(ctx context.Context) (string, string, func(), error) {
	if path, latest, cleanup, err := downloadLatestBinary(ctx, version.Value); err == nil && path != "" {
		return path, latest, cleanup, nil
	} else if err != nil {
		current, currentErr := os.Executable()
		if currentErr != nil {
			return "", "", nil, currentErr
		}
		return current, update.DisplayVersion(version.Value), nil, err
	}

	current, err := os.Executable()
	if err != nil {
		return "", "", nil, err
	}
	return current, update.DisplayVersion(version.Value), nil, nil
}

func pathContainsDir(pathEnv, dir string) bool {
	target := normalizePathDir(dir)
	if target == "" {
		return false
	}
	for _, entry := range filepath.SplitList(pathEnv) {
		if normalizePathDir(entry) == target {
			return true
		}
	}
	return false
}

func normalizePathDir(dir string) string {
	if dir == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return filepath.Clean(dir)
}
