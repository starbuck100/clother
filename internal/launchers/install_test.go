package launchers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/platform"
	"github.com/jolehuit/clother/internal/providers"
)

// launcherNames are the launchers a full install must create, in the spelling
// of the platform under test — clother-zai on Unix, clother-zai.exe on Windows.
func launcherNames(profiles ...string) []string {
	names := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		names = append(names, platform.LauncherName(profile))
	}
	return names
}

func testPaths(root string) config.Paths {
	return config.Paths{
		ConfigDir:       filepath.Join(root, "config"),
		DataDir:         filepath.Join(root, "data"),
		CacheDir:        filepath.Join(root, "cache"),
		BinDir:          filepath.Join(root, "bin"),
		ManifestFile:    filepath.Join(root, "data", "launchers.json"),
		SessionPatchDir: filepath.Join(root, "data", "session-patches"),
	}
}

func writeStub(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// writeLargeStub writes a stub above platform's size floor, so that a copy of
// it is still recognisable as the same installation on Windows.
func writeLargeStub(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	blob := make([]byte, 2<<20)
	copy(blob, "#!/bin/sh\n")
	if err := os.WriteFile(path, blob, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestSyncCreatesBinaryAndLaunchers(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	execPath := filepath.Join(root, "clother-staging")
	writeStub(t, execPath)

	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.File{
		Version:           1,
		ProviderOverrides: map[string]config.ProviderOverride{},
		OpenRouterAliases: map[string]string{"kimi": "moonshotai/kimi-k2.5"},
		CustomProviders: map[string]config.CustomProvider{
			"myprovider": {
				Name:        "myprovider",
				DisplayName: "myprovider",
				BaseURL:     "https://example.com/anthropic",
				APIKeyEnv:   "MYPROVIDER_API_KEY",
			},
		},
	}
	paths := testPaths(root)

	if err := Sync(execPath, paths, catalog, cfg, SyncOptions{InstallClaudeShim: true}); err != nil {
		t.Fatal(err)
	}

	names := append(
		[]string{platform.BinaryName(), platform.ClaudeName()},
		launcherNames("zai", "native", "or-kimi", "myprovider", "or", "custom")...,
	)
	for _, name := range names {
		if _, err := os.Lstat(filepath.Join(paths.BinDir, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}

	// The binary is installed as a copy of the running executable, not as a
	// link back to it: it has to survive the staging file going away.
	installed := filepath.Join(paths.BinDir, platform.BinaryName())
	if platform.SameFile(installed, execPath) {
		t.Fatalf("%s must be a copy of %s, not a link to it", installed, execPath)
	}
	data, err := os.ReadFile(installed)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "#!/bin/sh\n" {
		t.Fatalf("%s content = %q, want the executable's own bytes", installed, string(data))
	}
}

func TestSyncClaudeShimResolvesToClother(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	execPath := filepath.Join(root, "clother-staging")
	writeStub(t, execPath)

	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}
	paths := testPaths(root)
	cfg := &config.File{
		Version:           1,
		ProviderOverrides: map[string]config.ProviderOverride{},
		OpenRouterAliases: map[string]string{},
		CustomProviders:   map[string]config.CustomProvider{},
	}

	if err := Sync(execPath, paths, catalog, cfg, SyncOptions{InstallClaudeShim: true}); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(paths.BinDir, platform.ClaudeName())
	if !platform.SameInstallation(shim, filepath.Join(paths.BinDir, platform.BinaryName())) {
		t.Fatalf("%s does not resolve to clother; the shim would start itself", shim)
	}

	manifest, err := LoadManifest(paths.ManifestFile)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ClaudeShim != platform.ClaudeName() {
		t.Fatalf("manifest ClaudeShim = %q, want %q", manifest.ClaudeShim, platform.ClaudeName())
	}
}

func TestSyncWithoutClaudeShimLeavesClaudeAlone(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	execPath := filepath.Join(root, "clother-staging")
	writeStub(t, execPath)

	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}
	paths := testPaths(root)
	cfg := &config.File{
		Version:           1,
		ProviderOverrides: map[string]config.ProviderOverride{},
		OpenRouterAliases: map[string]string{},
		CustomProviders:   map[string]config.CustomProvider{},
	}

	// A Claude Code the user installed themselves must survive untouched.
	userClaude := filepath.Join(paths.BinDir, platform.ClaudeName())
	writeStub(t, userClaude)

	if err := Sync(execPath, paths, catalog, cfg, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if platform.SameInstallation(userClaude, filepath.Join(paths.BinDir, platform.BinaryName())) {
		t.Fatal("claude was replaced by clother even though the shim was not requested")
	}
	manifest, err := LoadManifest(paths.ManifestFile)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ClaudeShim != "" {
		t.Fatalf("manifest ClaudeShim = %q, want empty", manifest.ClaudeShim)
	}
	if _, err := os.Stat(filepath.Join(paths.BinDir, platform.LauncherName("zai"))); err != nil {
		t.Fatalf("provider launchers must still be created: %v", err)
	}
}

func TestSyncWithoutClaudeShimRemovesItsOwnPreviousShim(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	// Large enough for the size tiebreak in platform.SameInstallation, which is
	// what recognises a *copied* shim as clother's own on Windows.
	execPath := filepath.Join(root, "clother-staging")
	writeLargeStub(t, execPath)

	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}
	paths := testPaths(root)
	cfg := &config.File{
		Version:           1,
		ProviderOverrides: map[string]config.ProviderOverride{},
		OpenRouterAliases: map[string]string{},
		CustomProviders:   map[string]config.CustomProvider{},
	}

	if err := Sync(execPath, paths, catalog, cfg, SyncOptions{InstallClaudeShim: true}); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(paths.BinDir, platform.ClaudeName())
	if !platform.SameInstallation(shim, filepath.Join(paths.BinDir, platform.BinaryName())) {
		t.Fatalf("%s does not resolve to clother; the shim would start itself", shim)
	}

	if err := Sync(execPath, paths, catalog, cfg, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(shim); !os.IsNotExist(err) {
		t.Fatalf("clother's own shim survived an install without the shim, stat err=%v", err)
	}
	manifest, err := LoadManifest(paths.ManifestFile)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ClaudeShim != "" {
		t.Fatalf("manifest ClaudeShim = %q after an install without the shim, want empty", manifest.ClaudeShim)
	}
}

func TestSyncSkipsCopyAndUsesAbsoluteLinks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	// Simulate the Homebrew-managed binary (not in BinDir)
	homebrewBin := filepath.Join(root, "homebrew", "bin", platform.BinaryName())
	writeStub(t, homebrewBin)

	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.File{
		Version:           1,
		ProviderOverrides: map[string]config.ProviderOverride{},
		OpenRouterAliases: map[string]string{},
		CustomProviders:   map[string]config.CustomProvider{},
	}
	paths := testPaths(root)

	if err := Sync(homebrewBin, paths, catalog, cfg, SyncOptions{SkipCopy: true, InstallClaudeShim: true}); err != nil {
		t.Fatal(err)
	}

	// clother binary must NOT be copied into BinDir
	if _, err := os.Lstat(filepath.Join(paths.BinDir, platform.BinaryName())); err == nil {
		t.Fatal("clother binary must not be copied into BinDir in Homebrew mode")
	}

	// Every launcher must lead back to the Homebrew binary. The link flavour
	// differs by platform — an absolute symlink on Unix, a hardlink on Windows,
	// where a symlink would need a privilege the installer does not have — so
	// the assertion is the one that matters either way: same file.
	names := append([]string{platform.ClaudeName()}, launcherNames("zai", "native", "or", "custom")...)
	for _, name := range names {
		link := filepath.Join(paths.BinDir, name)
		if !platform.SameFile(link, homebrewBin) {
			t.Fatalf("%s does not resolve to %s", link, homebrewBin)
		}
	}
}

func TestSyncHomebrewSkipsDynamicProviderSymlinks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	homebrewBin := filepath.Join(root, "homebrew", "bin", platform.BinaryName())
	writeStub(t, homebrewBin)

	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.File{
		Version:           1,
		ProviderOverrides: map[string]config.ProviderOverride{},
		OpenRouterAliases: map[string]string{"kimi": "moonshotai/kimi-k2.5"},
		CustomProviders: map[string]config.CustomProvider{
			"myprovider": {Name: "myprovider", DisplayName: "myprovider", BaseURL: "https://example.com", APIKeyEnv: "MYPROVIDER_API_KEY"},
		},
	}
	paths := testPaths(root)

	if err := Sync(homebrewBin, paths, catalog, cfg, SyncOptions{SkipCopy: true, InstallClaudeShim: true}); err != nil {
		t.Fatal(err)
	}

	// individual dynamic symlinks must NOT be created under Homebrew
	for _, name := range launcherNames("or-kimi", "myprovider") {
		if _, err := os.Lstat(filepath.Join(paths.BinDir, name)); err == nil {
			t.Fatalf("%s must not be created in Homebrew mode", name)
		}
	}

	// gateway symlinks must always be present
	for _, name := range launcherNames("or", "custom") {
		if _, err := os.Lstat(filepath.Join(paths.BinDir, name)); err != nil {
			t.Fatalf("gateway launcher %s must always be created: %v", name, err)
		}
	}
}
