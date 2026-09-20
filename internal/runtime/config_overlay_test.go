package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jolehuit/clother/internal/platform"
	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/providers"
	"github.com/jolehuit/clother/internal/testutil"
)

func TestPrepareClaudeConfigOverlayMirrorsConfigAndPinsModel(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(filepath.Join(claudeDir, "teams"), 0o755); err != nil {
		t.Fatal(err)
	}
	originalSettings := []byte("{\n  \"model\": \"opus[1m]\",\n  \"env\": {\n    \"KEEP_ME\": \"1\",\n    \"ANTHROPIC_MODEL\": \"claude-opus-4-6\"\n  }\n}\n")
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), originalSettings, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "teams", "marker.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte("{\"theme\":\"light\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	target := profiles.Target{
		Family: providers.FamilyAnthropicCompatibleNonClaude,
		Model:  "qwen3.5-plus",
	}
	env, cleanup, err := PrepareClaudeConfigOverlay(target, []string{"--model", "glm-5"}, []string{
		"PATH=/usr/bin",
		"ANTHROPIC_BASE_URL=https://coding-intl.dashscope.aliyuncs.com/apps/anthropic",
		"ANTHROPIC_AUTH_TOKEN=secret-token",
		"ANTHROPIC_MODEL=qwen3.5-plus",
		"ANTHROPIC_DEFAULT_OPUS_MODEL=qwen3.5-plus",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)

	overlayDir := envSliceToMap(env)["CLAUDE_CONFIG_DIR"]
	if overlayDir == "" {
		t.Fatal("expected CLAUDE_CONFIG_DIR override")
	}

	settingsData, err := os.ReadFile(filepath.Join(overlayDir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(settingsData, &settings); err != nil {
		t.Fatal(err)
	}
	if settings["model"] != "glm-5" {
		t.Fatalf("patched model = %v, want glm-5", settings["model"])
	}
	settingsEnv, ok := settings["env"].(map[string]any)
	if !ok {
		t.Fatal("expected env object in patched settings")
	}
	if settingsEnv["KEEP_ME"] != "1" {
		t.Fatalf("patched env lost KEEP_ME: %+v", settingsEnv)
	}
	if settingsEnv["ANTHROPIC_MODEL"] != "glm-5" {
		t.Fatalf("patched ANTHROPIC_MODEL = %v, want glm-5", settingsEnv["ANTHROPIC_MODEL"])
	}
	if settingsEnv["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "glm-5" {
		t.Fatalf("patched ANTHROPIC_DEFAULT_OPUS_MODEL = %v, want glm-5", settingsEnv["ANTHROPIC_DEFAULT_OPUS_MODEL"])
	}
	if settingsEnv["CLAUDE_CODE_SUBAGENT_MODEL"] != "glm-5" {
		t.Fatalf("patched CLAUDE_CODE_SUBAGENT_MODEL = %v, want glm-5", settingsEnv["CLAUDE_CODE_SUBAGENT_MODEL"])
	}
	if settingsEnv["ANTHROPIC_DEFAULT_FABLE_MODEL"] != "glm-5" {
		t.Fatal("Fable model did not follow the model override")
	}
	// Mirrored, not copied. On Unix that is a symlink, on Windows a junction for
	// the directory and a hardlink for the file; what has to hold either way is
	// that the overlay entry *is* the original.
	markerPath := filepath.Join(overlayDir, "teams")
	if !platform.SameFile(markerPath, filepath.Join(claudeDir, "teams")) {
		t.Fatalf("%s is not the original directory", markerPath)
	}
	statePath := filepath.Join(overlayDir, ".claude.json")
	if !platform.SameFile(statePath, filepath.Join(home, ".claude.json")) {
		t.Fatalf("%s is not the home-level state file", statePath)
	}

	// A write through the mirrored directory must reach the original, or the
	// session history Claude Code records during a run would be deleted together
	// with the overlay.
	if err := os.WriteFile(filepath.Join(markerPath, "written.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(claudeDir, "teams", "written.txt")); err != nil {
		t.Fatalf("writing through the overlay did not reach the config directory: %v", err)
	}

	cleanup()
	if _, err := os.Stat(overlayDir); !os.IsNotExist(err) {
		t.Fatalf("expected cleanup to remove overlay, stat err=%v", err)
	}
}

func TestPrepareClaudeConfigOverlaySkipsNativeClaude(t *testing.T) {
	env, cleanup, err := PrepareClaudeConfigOverlay(profiles.Target{Family: providers.FamilyClaudeStrict}, nil, []string{"PATH=/usr/bin"})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if got := envSliceToMap(env)["CLAUDE_CONFIG_DIR"]; got != "" {
		t.Fatalf("CLAUDE_CONFIG_DIR = %q, want empty", got)
	}
}

func TestPrepareClaudeConfigOverlayHandlesStateFileInsideConfigDir(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Some installs leave a .claude.json inside the config dir in addition to
	// the canonical home-level one. Both previously mapped to the same overlay
	// path and aborted the launch with "file exists".
	if err := os.WriteFile(filepath.Join(claudeDir, ".claude.json"), []byte("{\"stale\":true}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	homeState := filepath.Join(home, ".claude.json")
	if err := os.WriteFile(homeState, []byte("{\"home\":true}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	target := profiles.Target{Family: providers.FamilyAnthropicCompatibleNonClaude, Model: "glm-5.2"}
	env, cleanup, err := PrepareClaudeConfigOverlay(target, nil, []string{
		"PATH=/usr/bin",
		"ANTHROPIC_BASE_URL=https://api.z.ai/api/anthropic",
		"ANTHROPIC_AUTH_TOKEN=secret-token",
		"ANTHROPIC_DEFAULT_SONNET_MODEL=glm-5.2",
	})
	if err != nil {
		t.Fatalf("overlay failed with .claude.json inside config dir: %v", err)
	}
	t.Cleanup(cleanup)

	overlayDir := envSliceToMap(env)["CLAUDE_CONFIG_DIR"]
	if overlayDir == "" {
		t.Fatal("expected CLAUDE_CONFIG_DIR override")
	}
	// The canonical home-level state file must be mirrored, not the in-dir copy.
	statePath := filepath.Join(overlayDir, ".claude.json")
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("overlay .claude.json missing: %v", err)
	}
	if !platform.SameFile(statePath, homeState) {
		t.Fatalf("overlay state is not the home-level state file %s", homeState)
	}
	if platform.SameFile(statePath, filepath.Join(claudeDir, ".claude.json")) {
		t.Fatalf("overlay state points at the in-dir copy instead of %s", homeState)
	}
}
