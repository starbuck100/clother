package commands

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jolehuit/clother/internal/platform"
	"github.com/jolehuit/clother/internal/runtime"
)

func runUpdate(ctx context.Context, c Context) (int, error) {
	if runtime.IsHomebrew() {
		brew, err := exec.LookPath("brew")
		if err != nil {
			return 1, err
		}
		cmd := exec.CommandContext(ctx, brew, "upgrade", "clother")
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			if exit, ok := err.(*exec.ExitError); ok {
				return exit.ExitCode(), nil
			}
			return 1, err
		}
		return 0, nil
	}
	code, err := runInstall(ctx, c)
	if err != nil || code != 0 {
		return code, err
	}
	// Generate launchers/commands with the newly installed binary's templates.
	// The old updater's embedded templates may be missing new slash commands.
	args := []string{"install", "--yes", "--bin-dir", c.Paths.BinDir}
	if c.Options.NoShim {
		args = append(args, "--no-shim")
	}
	if c.Options.NoCommands {
		args = append(args, "--no-commands")
	}
	cmd := exec.CommandContext(ctx, filepath.Join(c.Paths.BinDir, platform.BinaryName()), args...)
	cmd.Env = append(os.Environ(), "CLOTHER_SKIP_SELF_UPDATE=1", "CLOTHER_CONFIG_DIR="+c.Paths.ConfigDir, "CLOTHER_DATA_DIR="+c.Paths.DataDir, "CLOTHER_CACHE_DIR="+c.Paths.CacheDir)
	cmd.Stdin = os.Stdin
	cmd.Stdout = c.Output.Stdout
	cmd.Stderr = c.Output.Stderr
	if err := cmd.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode(), nil
		}
		return 1, err
	}
	return 0, nil
}
