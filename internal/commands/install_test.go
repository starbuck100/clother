package commands

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jolehuit/clother/internal/cli"
	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/platform"
	"github.com/jolehuit/clother/internal/providers"
	"github.com/jolehuit/clother/internal/testutil"
	"github.com/jolehuit/clother/internal/ui"
)

// isolate points every directory clother writes at a subdirectory of root.
// Without it a test on Windows would resolve %APPDATA% and %LOCALAPPDATA% and
// write into the real installation of whoever ran it.
func isolate(t *testing.T, root string, binDir string) {
	t.Helper()
	testutil.SetHome(t, filepath.Join(root, "home"))
	testutil.IsolateDirs(t, root)
	t.Setenv("CLOTHER_BIN", binDir)
}

func TestRunInstallPreservesSameBinClaude(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")

	isolate(t, root, binDir)
	t.Setenv("CLOTHER_SKIP_SELF_UPDATE", "1")

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	realClaude := filepath.Join(binDir, platform.ClaudeName())
	if err := os.WriteFile(realClaude, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+oldPath)

	paths, err := config.Detect("")
	if err != nil {
		t.Fatal(err)
	}
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
	output := &ui.Output{Stdout: io.Discard, Stderr: io.Discard, Format: ui.FormatHuman}

	code, err := runInstall(context.Background(), Context{
		Paths:   paths,
		Config:  cfg,
		Secrets: config.Secrets{},
		Catalog: catalog,
		Output:  output,
	})
	if err != nil {
		t.Fatalf("runInstall() error = %v", err)
	}
	if code != 0 {
		t.Fatalf("runInstall() code = %d, want 0", code)
	}

	preserved := filepath.Join(binDir, platform.RealClaudeName())
	if _, err := os.Stat(preserved); err != nil {
		t.Fatalf("expected preserved real claude, stat error: %v", err)
	}
	// The name `claude` now belongs to clother's shim, and the real Claude Code
	// is the one that was moved aside.
	shim := filepath.Join(binDir, platform.ClaudeName())
	if !platform.SameInstallation(shim, filepath.Join(binDir, platform.BinaryName())) {
		t.Fatalf("%s is not clother's shim", shim)
	}
	if platform.SameFile(shim, preserved) {
		t.Fatalf("%s still is the original Claude Code", shim)
	}
}

func TestRunInstallWithoutShimLeavesClaudeAlone(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")

	isolate(t, root, binDir)
	t.Setenv("CLOTHER_SKIP_SELF_UPDATE", "1")

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("#!/bin/sh\necho real claude\n")
	realClaude := filepath.Join(binDir, platform.ClaudeName())
	if err := os.WriteFile(realClaude, content, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	paths, err := config.Detect("")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}

	code, err := runInstall(context.Background(), Context{
		Paths:   paths,
		Config:  &config.File{Version: 1, ProviderOverrides: map[string]config.ProviderOverride{}, OpenRouterAliases: map[string]string{}, CustomProviders: map[string]config.CustomProvider{}},
		Secrets: config.Secrets{},
		Catalog: catalog,
		Output:  &ui.Output{Stdout: io.Discard, Stderr: io.Discard, Format: ui.FormatHuman},
		Options: cli.Options{NoShim: true},
	})
	if err != nil {
		t.Fatalf("runInstall() error = %v", err)
	}
	if code != 0 {
		t.Fatalf("runInstall() code = %d, want 0", code)
	}

	got, err := os.ReadFile(realClaude)
	if err != nil {
		t.Fatalf("--no-shim removed the user's claude: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("claude content = %q, want it untouched", string(got))
	}
	if _, err := os.Stat(filepath.Join(binDir, platform.RealClaudeName())); !os.IsNotExist(err) {
		t.Fatalf("--no-shim moved the user's claude aside, stat err=%v", err)
	}
	// The launchers are the whole point of the install and must still be there.
	if _, err := os.Stat(filepath.Join(binDir, platform.LauncherName("zai"))); err != nil {
		t.Fatalf("provider launchers were not created: %v", err)
	}
}

func TestRunInstallUpgradesToLatestRelease(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")

	isolate(t, root, binDir)

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	realClaude := filepath.Join(binDir, platform.ClaudeName())
	if err := os.WriteFile(realClaude, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+oldPath)

	releaseBinary := filepath.Join(root, "release-clother")
	if err := os.WriteFile(releaseBinary, []byte("#!/bin/sh\necho release-3.0.3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	originalDownloader := downloadLatestBinary
	downloadLatestBinary = func(_ context.Context, _ string) (string, string, func(), error) {
		return releaseBinary, "v3.0.3", nil, nil
	}
	defer func() {
		downloadLatestBinary = originalDownloader
	}()

	paths, err := config.Detect("")
	if err != nil {
		t.Fatal(err)
	}
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

	code, err := runInstall(context.Background(), Context{
		Paths:   paths,
		Config:  cfg,
		Secrets: config.Secrets{},
		Catalog: catalog,
		Output:  &ui.Output{Stdout: io.Discard, Stderr: io.Discard, Format: ui.FormatHuman},
	})
	if err != nil {
		t.Fatalf("runInstall() error = %v", err)
	}
	if code != 0 {
		t.Fatalf("runInstall() code = %d, want 0", code)
	}

	installed, err := os.ReadFile(filepath.Join(binDir, platform.BinaryName()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(installed), "release-3.0.3") {
		t.Fatalf("expected installed clother to come from latest release, got %q", string(installed))
	}
}

func TestRunInstallWarnsWhenBinDirIsNotOnPath(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	realClaudeDir := filepath.Join(root, "claude-bin")

	isolate(t, root, binDir)
	t.Setenv("CLOTHER_SKIP_SELF_UPDATE", "1")

	if err := os.MkdirAll(realClaudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realClaudeDir, platform.ClaudeName()), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", realClaudeDir+string(os.PathListSeparator)+oldPath)

	paths, err := config.Detect("")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	output := &ui.Output{Stdout: stdout, Stderr: stderr, Format: ui.FormatHuman}

	code, err := runInstall(context.Background(), Context{
		Paths:   paths,
		Config:  &config.File{Version: 1, ProviderOverrides: map[string]config.ProviderOverride{}, OpenRouterAliases: map[string]string{}, CustomProviders: map[string]config.CustomProvider{}},
		Secrets: config.Secrets{},
		Catalog: catalog,
		Output:  output,
	})
	if err != nil {
		t.Fatalf("runInstall() error = %v", err)
	}
	if code != 0 {
		t.Fatalf("runInstall() code = %d, want 0", code)
	}
	if !strings.Contains(stderr.String(), "is not on PATH") {
		t.Fatalf("expected PATH warning, got stderr %q", stderr.String())
	}
}
