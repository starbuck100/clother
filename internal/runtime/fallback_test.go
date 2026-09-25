package runtime

import (
	"context"
	"fmt"
	"github.com/jolehuit/clother/internal/budget"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/providers"
)

func TestSessionFallbackPaidOptInAndLiveQuota(t *testing.T) {
	t.Setenv("CLOTHER_BENCHMARKS", "0")
	t.Setenv("CLOTHER_FALLBACK_PLANNER", "0")
	t.Setenv("CLOTHER_PAID_FALLBACK", "")
	t.Setenv("CLOTHER_AUTO_FALLBACK", "")
	for _, mode := range []string{"free-only", "paid", "unknown-price", "zero-budget"} {
		paid := mode != "free-only"
		t.Run(mode, func(t *testing.T) {
			var paidCalls, quotaCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/kilo/models", "/or/v1/models":
					fmt.Fprint(w, `{"data":[{"id":"test/free","context_length":100000,"pricing":{"prompt":"0","completion":"0"},"supported_parameters":["tools"],"architecture":{"input_modalities":["text"],"output_modalities":["text"]}}]}`)
				case "/or/v1/key":
					quotaCalls.Add(1)
					fmt.Fprint(w, `{"data":{"free_model_daily_requests":{"used":50,"limit":50,"remaining":0}}}`)
				case "/initial/v1/messages":
					w.WriteHeader(429)
					fmt.Fprint(w, `{"error":{"message":"Rate limit exceeded for free models"}}`)
				case "/ds/v1/messages":
					paidCalls.Add(1)
					data, _ := io.ReadAll(r.Body)
					if !strings.Contains(string(data), `"model":"deepseek-flash"`) || r.Header.Get("Authorization") != "Bearer configured-deepseek" {
						t.Error("wrong model/key")
					}
					fmt.Fprint(w, `{"type":"message","content":[{"type":"text","text":"OK"}],"usage":{"input_tokens":1,"output_tokens":1}}`)
				default:
					t.Errorf("unexpected %s", r.URL.Path)
					w.WriteHeader(400)
				}
			}))
			defer server.Close()
			root := t.TempDir()
			paths := config.Paths{ConfigFile: filepath.Join(root, "config.json"), SecretsFile: filepath.Join(root, "secrets.env"), CacheDir: filepath.Join(root, "cache"), DataDir: root}
			cfg := &config.File{Version: 1, FallbackPaid: paid, ProviderOverrides: map[string]config.ProviderOverride{"kilo": {Model: "test/free", BaseURL: server.URL + "/kilo"}, "openrouter": {Model: "test/free", BaseURL: server.URL + "/or"}, "deepseek": {BaseURL: server.URL + "/ds"}}}
			cfg.Budget = &budget.Config{Daily: 1, Session: 1, Prices: map[string]budget.Price{"deepseek/deepseek-flash": {Input: 0.3, Output: 1.2, Expires: time.Now().Add(time.Hour), Source: "explicit test endpoint price"}}}
			if mode == "unknown-price" {
				cfg.Budget.Prices = nil
			}
			if mode == "zero-budget" {
				cfg.Budget.Daily = 0
			}
			if e := config.SaveConfig(paths.ConfigFile, cfg); e != nil {
				t.Fatal(e)
			}
			if e := config.SaveSecrets(paths.SecretsFile, config.Secrets{"OPENROUTER_API_KEY": "configured-or", "DEEPSEEK_API_KEY": "configured-deepseek"}); e != nil {
				t.Fatal(e)
			}
			cat, _ := providers.Load()
			target, _ := profiles.Resolve("kilo", cat, cfg)
			env, close, e := PrepareFallback(context.Background(), paths, target, nil, []string{"ANTHROPIC_BASE_URL=" + server.URL + "/initial", "ANTHROPIC_AUTH_TOKEN=session", "ANTHROPIC_MODEL=test/free"})
			if e != nil {
				t.Fatal(e)
			}
			defer close()
			values := envSliceToMap(env)
			req, _ := http.NewRequest("POST", values["ANTHROPIC_BASE_URL"]+"/v1/messages", strings.NewReader(`{"model":"test/free","max_tokens":128,"messages":[{"role":"user","content":"Hi"}]}`))
			req.Header.Set("Authorization", "Bearer "+values["ANTHROPIC_AUTH_TOKEN"])
			resp, e := http.DefaultClient.Do(req)
			if e != nil {
				t.Fatal(e)
			}
			defer resp.Body.Close()
			data, _ := io.ReadAll(resp.Body)
			if mode == "unknown-price" || mode == "zero-budget" {
				if resp.StatusCode != 402 || paidCalls.Load() != 0 {
					t.Fatalf("unbudgeted request: %d calls %d", resp.StatusCode, paidCalls.Load())
				}
			} else if paid {
				if resp.StatusCode != 200 || paidCalls.Load() != 1 || !strings.Contains(string(data), "may incur cost") {
					t.Fatalf("%d %s", resp.StatusCode, data)
				}
			} else if resp.StatusCode != 429 || paidCalls.Load() != 0 {
				t.Fatal("paid key used without opt-in")
			}
			if quotaCalls.Load() != 1 {
				t.Fatalf("quota check count %d", quotaCalls.Load())
			}
		})
	}
}

func TestInformationalLaunchSkipsFallbackInference(t *testing.T) {
	for _, arg := range []string{"--version", "-v", "--help", "-h"} {
		env := []string{"unchanged=yes"}
		result, close, err := PrepareFallback(context.Background(), config.Paths{ConfigFile: "not-a-file"}, profiles.Target{Family: providers.FamilyKilo}, []string{arg}, env)
		if err != nil || len(result) != 1 || result[0] != env[0] {
			t.Fatalf("%s did not bypass inference: %v", arg, err)
		}
		close()
	}
}
