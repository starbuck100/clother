package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jolehuit/clother/internal/launchers"
	"github.com/jolehuit/clother/internal/platform"
	"github.com/jolehuit/clother/internal/runtime"
)

func runUninstall(_ context.Context, c Context) (int, error) {
	if !c.Options.Yes {
		ok, err := c.Prompt.Confirm("Remove all Clother files?", false)
		if err != nil {
			return 1, err
		}
		if !ok {
			return 0, nil
		}
	}

	// The real Claude Code is put back under its own name before anything else
	// happens. Upstream removed the shim and left the preserved binary behind,
	// which stranded an installation under a name nothing looks for; restoring
	// it is what makes uninstall reversible.
	if restored, err := runtime.RestorePreservedClaude(c.Paths); err != nil {
		c.Output.Warn("could not restore the real claude binary: %v", err)
	} else if restored {
		c.Output.Success("restored %s", platform.ClaudeName())
	}

	manifest, _ := launchers.LoadManifest(c.Paths.ManifestFile)

	// The shim is removed only when the manifest says this install created it,
	// and only while it still is a clother shim. Either check alone would be
	// enough on a clean machine; together they mean a `claude` the user has
	// since replaced is never deleted by an uninstall.
	if manifest.ClaudeShim != "" {
		shim := filepath.Join(c.Paths.BinDir, manifest.ClaudeShim)
		binary := filepath.Join(c.Paths.BinDir, platform.BinaryName())
		if platform.SameInstallation(shim, binary) {
			_ = os.Remove(shim)
		}
	}

	for _, name := range manifest.Launchers {
		_ = os.Remove(filepath.Join(c.Paths.BinDir, name))
	}

	// The binary is removed last, and after the shim, because it is what the
	// shim is compared against to prove ownership. On Windows it is also the
	// file that is currently running.
	if removed, err := platform.RemoveExecutable(filepath.Join(c.Paths.BinDir, platform.BinaryName())); err != nil {
		c.Output.Warn("could not remove %s: %v", platform.BinaryName(), err)
	} else if !removed {
		c.Output.Warn("%s is still in place; remove it once this process has exited", platform.BinaryName())
	}

	_ = os.RemoveAll(c.Paths.ConfigDir)
	_ = os.RemoveAll(c.Paths.DataDir)
	_ = os.RemoveAll(c.Paths.CacheDir)
	fmt.Fprintln(c.Output.Stdout, "Clother uninstalled")
	return 0, nil
}
