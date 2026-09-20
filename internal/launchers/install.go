package launchers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/platform"
	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/providers"
)

// GeneratedFile is a file this install wrote, with the hash of what it wrote.
// Removal re-verifies that hash, so a file the user has edited since is never
// deleted as though it were ours.
type GeneratedFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type Manifest struct {
	Launchers []string `json:"launchers"`
	// ClaudeShim records the `claude` entry this install created, so uninstall
	// can tell its own shim apart from a real Claude Code the user put there
	// afterwards. Manifests written before this field exists leave it empty,
	// and an empty value means "remove nothing" — the safe direction to err in.
	ClaudeShim string `json:"claude_shim,omitempty"`
	// Commands records the slash commands this install wrote into the user's
	// Claude configuration. An absent list, which is what a manifest written
	// before this field existed has, means the same as an empty one: remove
	// nothing.
	Commands []GeneratedFile `json:"commands,omitempty"`
}

// SyncOptions selects what an install writes.
type SyncOptions struct {
	// SkipCopy leaves the binary where it is and points every link at execPath
	// absolutely, instead of copying it into BinDir. Homebrew installs use it,
	// so a formula upgrade is picked up without re-running `clother install`.
	SkipCopy bool
	// InstallClaudeShim writes the `claude` shim. It is false only when the user
	// asked for their Claude Code to be left completely untouched.
	InstallClaudeShim bool
	// CommandDir is where the slash commands are written, and an empty value
	// writes none. The caller resolves it, because it depends on the user's
	// Claude configuration and on whether we are inside a session — neither of
	// which this package has any business looking up. It is unaffected by
	// InstallClaudeShim: the commands work in any Claude Code session, and the
	// shim is orthogonal to them.
	CommandDir string
}

// Sync installs the clother binary and provider launchers into paths.BinDir.
func Sync(execPath string, paths config.Paths, catalog providers.Catalog, cfg *config.File, opts SyncOptions) error {
	if err := paths.EnsureBaseDirs(); err != nil {
		return err
	}
	skipCopy := opts.SkipCopy

	binaryName := platform.BinaryName() // "clother", or "clother.exe" on Windows

	// The binary is written before any link is created, and that order is
	// load-bearing on Windows. A hardlink does not follow its original when the
	// original is replaced, so links made first would keep pointing at the old
	// file record and every launcher would silently run the previous version.
	// Writing first means the links below are always made against what is
	// actually on disk.
	if !skipCopy {
		destBinary := filepath.Join(paths.BinDir, binaryName)
		if err := copyExecutable(execPath, destBinary); err != nil {
			return err
		}
	}

	previous, _ := LoadManifest(paths.ManifestFile)
	desired := map[string]struct{}{}
	for _, target := range profiles.All(catalog, cfg) {
		// Under Homebrew the formula already installs static provider symlinks in
		// the Homebrew prefix, and clother-or / clother-custom cover dynamic
		// providers via gateway invocation. Skip individual dynamic symlinks to
		// keep ~/bin clean for Homebrew users.
		if skipCopy && isDynamicProfile(target.Profile, cfg) {
			continue
		}
		desired[launcherName(target.Profile)] = struct{}{}
	}
	// Always create gateway symlinks regardless of install method or whether
	// any dynamic providers are configured. The isDynamicProfile skip above
	// only applies to per-alias/per-provider symlinks, never to these gateways.
	desired[platform.LauncherName("or")] = struct{}{}
	desired[platform.LauncherName("custom")] = struct{}{}

	for _, old := range previous.Launchers {
		if _, ok := desired[old]; ok {
			continue
		}
		_ = os.Remove(filepath.Join(paths.BinDir, old))
	}

	var launchers []string
	for name := range desired {
		launchers = append(launchers, name)
	}
	sort.Strings(launchers)
	for _, name := range launchers {
		link := filepath.Join(paths.BinDir, name)
		// Removing first is what makes a re-install pick up a replaced binary,
		// and it also clears anything a previous version left under this name.
		if _, err := platform.RemoveExecutable(link); err != nil {
			return err
		}
		if err := platform.LinkLauncher(execPath, binaryName, link, skipCopy); err != nil {
			return err
		}
	}
	// The slash commands are written here because this is where the binary they
	// invoke has just been placed, and how to invoke it is baked into them.
	//
	// Opting out leaves alone whatever a previous install wrote, and keeps it in
	// the manifest so uninstall still knows about it: --no-commands means "stop
	// writing these", not "forget that they exist".
	commands := previous.Commands
	if opts.CommandDir != "" {
		synced, err := SyncCommands(opts.CommandDir, CommandFileOptions{Clother: ClotherInvocation(paths)})
		if err != nil {
			return err
		}
		commands = synced
	}

	claudeShim := filepath.Join(paths.BinDir, platform.ClaudeName())
	if !opts.InstallClaudeShim {
		// A previous install may have left our shim under this name. It is
		// removed only when the manifest says we put it there and the file still
		// is our own binary — the same identity test `clother uninstall` uses.
		// Under any other name is a real Claude Code, which this install was
		// asked not to touch.
		if previous.ClaudeShim != "" && platform.SameInstallation(claudeShim, filepath.Join(paths.BinDir, binaryName)) {
			_ = os.Remove(claudeShim)
		}
		return SaveManifest(paths.ManifestFile, Manifest{Launchers: launchers, Commands: commands})
	}
	_ = os.Remove(claudeShim)
	if err := platform.LinkShim(execPath, binaryName, claudeShim, skipCopy); err != nil {
		return err
	}
	return SaveManifest(paths.ManifestFile, Manifest{
		Launchers:  launchers,
		ClaudeShim: platform.ClaudeName(),
		Commands:   commands,
	})
}

func LoadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func SaveManifest(path string, manifest Manifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeAtomic(path, data, 0o644)
}

func launcherName(profile string) string {
	return platform.LauncherName(profile)
}

// isDynamicProfile reports whether a profile is user-defined (OpenRouter alias
// or custom provider) rather than a catalog-builtin static provider.
func isDynamicProfile(profile string, cfg *config.File) bool {
	if strings.HasPrefix(profile, "or-") {
		return true
	}
	if cfg == nil {
		return false
	}
	_, isCustom := cfg.CustomProviders[profile]
	return isCustom
}

func copyExecutable(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	// CopyFile skips the copy when src and dst are already the same file, which
	// is the usual case: `clother install` runs from the very file it would
	// otherwise overwrite. On Windows that file cannot be replaced at all while
	// it is running, so the short-circuit is the only thing that works there.
	return platform.CopyFile(src, dst)
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	return platform.AtomicWrite(path, data, mode)
}
