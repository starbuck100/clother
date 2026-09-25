package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jolehuit/clother/internal/platform"
	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/providers"
)

func PrepareClaudeConfigOverlay(target profiles.Target, args []string, env []string) ([]string, func(), error) {
	if target.Family == providers.FamilyClaudeStrict {
		return env, func() {}, nil
	}

	envMap := envSliceToMap(env)
	overrideModel := ModelOverride(args)
	if overrideModel != "" {
		envMap["ANTHROPIC_MODEL"] = overrideModel
		for _, key := range []string{
			"ANTHROPIC_DEFAULT_FABLE_MODEL",
			"ANTHROPIC_DEFAULT_HAIKU_MODEL",
			"ANTHROPIC_DEFAULT_SONNET_MODEL",
			"ANTHROPIC_DEFAULT_OPUS_MODEL",
			"ANTHROPIC_SMALL_FAST_MODEL",
			"CLAUDE_CODE_SUBAGENT_MODEL",
		} {
			envMap[key] = overrideModel
		}
	}

	claudeEnv := anthropicEnv(envMap)
	for _, key := range []string{"CLOTHER_ROUTE_STATUS", "CLOTHER_BUDGET_SESSION", "CLOTHER_CONTROL_URL", "CLOTHER_CONTROL_TOKEN", "CLOTHER_STATUSLINE"} {
		if value, ok := envMap[key]; ok {
			claudeEnv[key] = value
		}
	}
	if target.Family == providers.FamilyKilo {
		claudeEnv["ENABLE_TOOL_SEARCH"] = "false"
	}
	if target.Family == providers.FamilyOpenRouter || target.Family == providers.FamilyKilo {
		for _, key := range openRouterLimitKeys {
			if value, ok := envMap[key]; ok {
				claudeEnv[key] = value
			}
		}
	}
	if len(claudeEnv) == 0 {
		return flattenEnv(envMap), func() {}, nil
	}

	sessionModel := effectiveSessionModel(target, claudeEnv)
	if sessionModel == "" {
		return flattenEnv(envMap), func() {}, nil
	}

	sourceDir := envMap["CLAUDE_CONFIG_DIR"]
	if sourceDir == "" {
		sourceDir = filepath.Join(userHomeDir(), ".claude")
	}

	overlayDir, err := createOverlayDir(sourceDir)
	if err != nil {
		return nil, nil, err
	}
	links, err := mirrorClaude(sourceDir, overlayDir)
	if err != nil {
		cleanupOverlay(overlayDir, links)
		return nil, nil, err
	}
	cleanup := func() {
		cleanupOverlay(overlayDir, links)
	}

	if err := writePatchedClaudeSettings(sourceDir, overlayDir, sessionModel, claudeEnv); err != nil {
		cleanup()
		return nil, nil, err
	}

	envMap["CLAUDE_CONFIG_DIR"] = overlayDir
	return flattenEnv(envMap), cleanup, nil
}

func envSliceToMap(env []string) map[string]string {
	out := make(map[string]string, len(env))
	for _, pair := range env {
		key, value, ok := splitEnv(pair)
		if ok {
			out[key] = value
		}
	}
	return out
}

func anthropicEnv(envMap map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range envMap {
		if strings.HasPrefix(key, "ANTHROPIC_") || strings.HasPrefix(key, "CLAUDE_CODE_SUBAGENT_MODEL") {
			out[key] = value
		}
	}
	return out
}

func effectiveSessionModel(target profiles.Target, envMap map[string]string) string {
	for _, key := range []string{
		"ANTHROPIC_MODEL",
		"ANTHROPIC_DEFAULT_OPUS_MODEL",
		"ANTHROPIC_DEFAULT_SONNET_MODEL",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL",
		"ANTHROPIC_SMALL_FAST_MODEL",
		"CLAUDE_CODE_SUBAGENT_MODEL",
	} {
		if model := strings.TrimSpace(envMap[key]); model != "" {
			return model
		}
	}
	if model := strings.TrimSpace(target.Model); model != "" {
		return model
	}
	for _, key := range []string{"opus", "sonnet", "haiku", "small"} {
		if model := strings.TrimSpace(target.ModelTiers[key]); model != "" {
			return model
		}
	}
	return ""
}

// overlayLink records where one mirrored entry came from, so cleanup can tell
// an entry that is still a view of the original apart from one whose original
// was replaced underneath it.
type overlayLink struct {
	overlay string
	source  string
}

// mirrorClaude links every entry of the config directory, plus the state file
// that lives beside it, into the overlay, and reports what it linked.
//
// The returned links are not bookkeeping for its own sake: cleanup has to know
// the exact source of each entry, because guessing it would let a file be moved
// to the wrong place. See cleanupOverlay in overlay_windows.go.
func mirrorClaude(sourceDir, overlayDir string) ([]overlayLink, error) {
	var links []overlayLink

	entries, err := os.ReadDir(sourceDir)
	if err != nil && !os.IsNotExist(err) {
		return links, err
	}
	for _, entry := range entries {
		// settings.json is rewritten with the patched copy; .claude.json is the
		// state file, mirrored separately below. Skipping it here avoids a link
		// collision when the config dir also contains its own .claude.json
		// alongside the home-level one.
		if entry.Name() == "settings.json" || entry.Name() == ".claude.json" {
			continue
		}
		src := filepath.Join(sourceDir, entry.Name())
		dst := filepath.Join(overlayDir, entry.Name())
		if err := linkOverlayEntry(src, dst); err != nil {
			return links, fmt.Errorf("link %s: %w", entry.Name(), err)
		}
		links = append(links, overlayLink{overlay: dst, source: src})
	}

	statePath := filepath.Join(filepath.Dir(sourceDir), ".claude.json")
	if _, err := os.Stat(statePath); err != nil {
		if os.IsNotExist(err) {
			return links, nil
		}
		return links, err
	}
	dst := filepath.Join(overlayDir, ".claude.json")
	if err := linkOverlayEntry(statePath, dst); err != nil {
		return links, fmt.Errorf("link .claude.json: %w", err)
	}
	return append(links, overlayLink{overlay: dst, source: statePath}), nil
}

// linkOverlayEntry mirrors one entry into the overlay â€” a directory link for a
// directory, a file link for a file, never a copy.
//
// This is load-bearing. Claude Code writes new sessions into
// CLAUDE_CONFIG_DIR/projects, so a *copied* projects directory would collect
// the entire session history for the duration of the run and then have it
// deleted together with the overlay.
func linkOverlayEntry(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return platform.LinkDir(src, dst)
	}
	return platform.LinkOrCopy(src, dst)
}

func writePatchedClaudeSettings(sourceDir, overlayDir, sessionModel string, envMap map[string]string) error {
	settings := map[string]any{}
	sourceSettings := filepath.Join(sourceDir, "settings.json")
	data, err := os.ReadFile(sourceSettings)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("decode %s: %w", sourceSettings, err)
		}
	}

	settings["model"] = sessionModel
	if envMap["CLOTHER_ROUTE_STATUS"] != "" && envMap["CLOTHER_STATUSLINE"] != "0" {
		if exe, e := os.Executable(); e == nil {
			// Temporary session overlay; original user settings remain untouched.
			executable := filepath.ToSlash(exe)
			if strings.HasPrefix(strings.ToLower(filepath.Base(exe)), "clother-") {
				candidate := filepath.Join(filepath.Dir(exe), platform.BinaryName())
				if _, e := os.Stat(candidate); e == nil {
					executable = filepath.ToSlash(candidate)
				}
			}
			quoted := "'" + strings.ReplaceAll(executable, "'", "'\"'\"'") + "'"
			settings["statusLine"] = map[string]any{"type": "command", "command": quoted + " __session statusline"}
		}
	}
	settingsEnv := map[string]any{}
	if existing, ok := settings["env"].(map[string]any); ok {
		for key, value := range existing {
			settingsEnv[key] = value
		}
		for _, key := range anthropicEnvKeys(settingsEnv) {
			delete(settingsEnv, key)
		}
	}
	for key, value := range envMap {
		settingsEnv[key] = value
	}
	// Do not let a nested session inherit its parent router through settings.
	for _, key := range []string{"CLOTHER_CONTROL_URL", "CLOTHER_CONTROL_TOKEN", "CLOTHER_ROUTE_STATUS", "CLOTHER_BUDGET_SESSION"} {
		if _, ok := envMap[key]; !ok {
			delete(settingsEnv, key)
		}
	}
	settings["env"] = settingsEnv

	encoded, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return os.WriteFile(filepath.Join(overlayDir, "settings.json"), encoded, 0o644)
}

func anthropicEnvKeys(env map[string]any) []string {
	keys := make([]string, 0, len(env))
	for key := range env {
		if strings.HasPrefix(key, "ANTHROPIC_") || key == "CLAUDE_CODE_SUBAGENT_MODEL" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}
