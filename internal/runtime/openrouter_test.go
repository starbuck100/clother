package runtime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/jolehuit/clother/internal/openrouter"
	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/providers"
)

func TestOpenRouterLimits(t *testing.T) {
	for _, tc := range []struct {
		name      string
		context   int
		output    int
		reasoning bool
		user      map[string]string
		want      int
		think     int
	}{
		{"small output ceiling", 65536, 2048, true, nil, 2048, 1024},
		{"shared context", 8192, 32000, true, nil, 2048, 1024},
		{"unknown ceiling", 200000, 0, true, nil, 4096, 2048},
		{"unsupported thinking", 65536, 8192, false, map[string]string{"MAX_THINKING_TOKENS": "99999"}, 8192, 0},
		{"oversized user limits", 65536, 8192, true, map[string]string{"CLAUDE_CODE_MAX_OUTPUT_TOKENS": "99999", "MAX_THINKING_TOKENS": "99999"}, 8192, 4096},
		{"smaller user output", 65536, 8192, true, map[string]string{"CLAUDE_CODE_MAX_OUTPUT_TOKENS": "512"}, 512, 0},
		{"smaller user context", 65536, 8192, true, map[string]string{"CLAUDE_CODE_MAX_CONTEXT_TOKENS": "4096"}, 1024, 0},
		{"thinking disabled", 65536, 8192, true, map[string]string{"MAX_THINKING_TOKENS": "0"}, 8192, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := openrouter.Model{ContextLength: tc.context}
			model.TopProvider.MaxCompletionTokens = tc.output
			if tc.reasoning {
				model.SupportedParameters = []string{"reasoning"}
			}
			env := tc.user
			if env == nil {
				env = map[string]string{}
			}
			applyOpenRouterLimits(model, env)
			if env["CLAUDE_CODE_MAX_OUTPUT_TOKENS"] != strconv.Itoa(tc.want) || env["MAX_THINKING_TOKENS"] != strconv.Itoa(tc.think) {
				t.Fatalf("wrong limits: %v", env)
			}
		})
	}
}

func TestGatewayOverrideLimitsReachSettingsOverlay(t *testing.T) {
	for _, family := range []providers.Family{providers.FamilyOpenRouter, providers.FamilyKilo} {
		t.Run(string(family), func(t *testing.T) {
			cacheName := string(family) + "-models.json"

			root := t.TempDir()
			source := filepath.Join(root, "claude")
			if err := os.MkdirAll(source, 0o700); err != nil {
				t.Fatal(err)
			}
			original := `{"env":{"CLAUDE_CODE_MAX_OUTPUT_TOKENS":"99999","MAX_THINKING_TOKENS":"99999"}}`
			if err := os.WriteFile(filepath.Join(source, "settings.json"), []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			var model openrouter.Model
			if err := json.Unmarshal([]byte(`{"id":"vendor/override","context_length":32768,"top_provider":{"max_completion_tokens":2048},"architecture":{"input_modalities":["text"],"output_modalities":["text"]},"supported_parameters":["tools"]}`), &model); err != nil {
				t.Fatal(err)
			}
			catalog := openrouter.Catalog{BaseURL: "https://example.invalid/api", FetchedAt: time.Now(), Models: []openrouter.Model{model}}
			data, _ := json.Marshal(catalog)
			if err := os.WriteFile(filepath.Join(root, cacheName), data, 0o600); err != nil {
				t.Fatal(err)
			}
			target := profiles.Target{Family: family, BaseURL: catalog.BaseURL, Model: "absent/default"}
			args := []string{"--model", "vendor/override"}
			env, err := PrepareOpenRouterEnv(context.Background(), root, target, args, []string{"CLAUDE_CONFIG_DIR=" + source})
			if err != nil {
				t.Fatal(err)
			}
			env, cleanup, err := PrepareClaudeConfigOverlay(target, args, env)
			if err != nil {
				t.Fatal(err)
			}
			defer cleanup()
			data, err = os.ReadFile(filepath.Join(envSliceToMap(env)["CLAUDE_CONFIG_DIR"], "settings.json"))
			if err != nil {
				t.Fatal(err)
			}
			var settings struct {
				Env map[string]string `json:"env"`
			}
			if err := json.Unmarshal(data, &settings); err != nil {
				t.Fatal(err)
			}
			if settings.Env["CLAUDE_CODE_MAX_OUTPUT_TOKENS"] != "2048" || settings.Env["MAX_THINKING_TOKENS"] != "0" || settings.Env["ANTHROPIC_MODEL"] != "vendor/override" {
				t.Fatalf("settings bypass limits: %v", settings.Env)
			}
			data, _ = os.ReadFile(filepath.Join(source, "settings.json"))
			if string(data) != original {
				t.Fatal("original settings modified")
			}
			if _, err := PrepareOpenRouterEnv(context.Background(), root, target, nil, nil); err == nil {
				t.Fatal("unknown default model accepted")
			}

		})
	}
}
