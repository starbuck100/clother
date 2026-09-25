package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/openrouter"
	"github.com/jolehuit/clother/internal/ui"
	"github.com/jolehuit/clother/internal/usage"
)

func freeCatalog(t *testing.T) openrouter.Catalog {
	t.Helper()
	var catalog openrouter.Catalog
	err := json.Unmarshal([]byte(`{"data":[{"id":"vendor/a:free","context_length":32000,"top_provider":{"max_completion_tokens":4096},"architecture":{"input_modalities":["text"],"output_modalities":["text"]},"supported_parameters":["tools"],"pricing":{"prompt":0,"completion":0}},{"id":"vendor/b:free","context_length":16000,"architecture":{"input_modalities":["text"],"output_modalities":["text"]},"supported_parameters":["tools"],"pricing":{"prompt":0,"completion":0}},{"id":"vendor/paid","context_length":64000,"architecture":{"input_modalities":["text"],"output_modalities":["text"]},"supported_parameters":["tools"],"pricing":{"prompt":1,"completion":1}}]}`), &catalog)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func TestFreeRecommendationsRespectProviderAndModelLimits(t *testing.T) {
	now := time.Now().UTC()
	catalog := freeCatalog(t)
	base := usageReport{At: now, CurrentProvider: "kilo", CurrentModel: "vendor/a:free"}
	for _, tc := range []struct {
		scope        string
		wantProvider string
	}{{"provider", "openrouter"}, {"model", "kilo"}, {"unknown", "kilo"}} {
		report := base
		report.Providers = []providerUsage{
			{Provider: "kilo", Catalog: catalog, Blocks: []usage.Event{{At: now, Model: "vendor/a:free", Limit: &usage.Limit{Kind: "rate_limit", Scope: tc.scope}}}},
			{Provider: "openrouter", Credential: true, Catalog: catalog, Account: &usage.OpenRouterAccount{FreeDaily: &usage.FreeQuota{Used: 1, Limit: 50, Remaining: 49}}},
		}
		recommend(&report)
		if len(report.Candidates) == 0 {
			t.Fatal("no fallback")
		}
		hasKilo := false
		for _, c := range report.Candidates {
			if strings.Contains(c.Model, "paid") {
				t.Fatal("paid fallback")
			}
			if c.Provider == "kilo" {
				hasKilo = true
			}
		}
		if hasKilo != (tc.wantProvider == "kilo") {
			t.Fatalf("scope %s incorrectly blocks gateway: %+v", tc.scope, report.Candidates)
		}
		if report.Candidates[0].Provider != "openrouter" || report.Candidates[0].Model != "vendor/a:free" {
			t.Fatal("same model on another gateway should be preferred")
		}
	}
	report := base
	report.Providers = []providerUsage{{Provider: "kilo", Catalog: catalog, Blocks: []usage.Event{{Limit: &usage.Limit{Kind: "free_hour", Scope: "provider"}}}}, {Provider: "openrouter", Credential: true, Catalog: catalog, Account: &usage.OpenRouterAccount{FreeDaily: &usage.FreeQuota{Used: 50, Limit: 50, Remaining: 0}}}}
	recommend(&report)
	if len(report.Candidates) != 0 {
		t.Fatal("exhausted providers still recommended")
	}
}

func TestNextModelFitsMeasuredHistory(t *testing.T) {
	now := time.Now().UTC()
	input := 15000
	r := usageReport{At: now, CurrentProvider: "kilo", CurrentModel: "vendor/a:free", Providers: []providerUsage{
		{Provider: "kilo", Catalog: freeCatalog(t), Events: []usage.Event{{At: now, Model: "vendor/a:free", Input: &input, Outcome: "ok"}}},
		{Provider: "openrouter", Credential: true, Catalog: freeCatalog(t)},
	}}
	recommend(&r)
	if r.RequiredContext != 22096 {
		t.Fatalf("wrong context reserve: %d", r.RequiredContext)
	}
	for _, c := range r.Candidates {
		if c.Context < r.RequiredContext {
			t.Fatal("too-small context recommended")
		}
	}
	r.Candidates = nil
	r.Providers[0].Events[0].Images = true
	recommend(&r)
	if len(r.Candidates) > 0 {
		t.Fatal("text-only fallback would lose images")
	}
}

func TestUsageCommandLiveQuotaAndBothCatalogs(t *testing.T) {
	t.Setenv("CLOTHER_BENCHMARKS", "0")
	c, out := testSessionContext(t)
	c.Output.Format = ui.FormatJSON
	c.Secrets["OPENROUTER_API_KEY"] = "DO_NOT_PRINT_KEY"
	hits := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits[r.URL.Path]++
		if r.URL.Path == "/or/v1/key" {
			if r.Header.Get("Authorization") != "Bearer DO_NOT_PRINT_KEY" {
				t.Error("missing key")
			}
			fmt.Fprint(w, `{"data":{"free_model_daily_requests":{"used":50,"limit":50,"remaining":0},"usage":0}}`)
			return
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("credential sent to public catalog")
		}
		_ = json.NewEncoder(w).Encode(freeCatalog(t))
	}))
	defer server.Close()
	c.Config.ProviderOverrides["openrouter"] = config.ProviderOverride{BaseURL: server.URL + "/or", Model: "vendor/a:free"}
	c.Config.ProviderOverrides["kilo"] = config.ProviderOverride{BaseURL: server.URL + "/kilo", Model: "vendor/a:free"}
	code, err := runUsage(context.Background(), c, []string{"next", "openrouter", "vendor/a:free"})
	if code != 0 || err != nil {
		t.Fatalf("%d %v", code, err)
	}
	var result usageReport
	if json.Unmarshal(out.Bytes(), &result) != nil {
		t.Fatal(out.String())
	}
	if len(result.Providers) != 2 || len(result.Candidates) == 0 || result.Candidates[0].Provider != "kilo" {
		t.Fatal("provider-wide fallback incorrect")
	}
	if hits["/or/v1/key"] != 1 || hits["/or/v1/models"] != 1 || hits["/kilo/models"] != 1 {
		t.Fatalf("both gateways not checked: %v", hits)
	}
	if strings.Contains(out.String(), "DO_NOT_PRINT_KEY") {
		t.Fatal("key leaked")
	}
	configNotWritten(t, c)
	path := filepath.Join(t.TempDir(), "route.json")
	if err := os.WriteFile(path, []byte(`{"provider":"kilo","model":"vendor/b:free","free":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLOTHER_ROUTE_STATUS", path)
	t.Setenv("CLOTHER_PROFILE", "openrouter")
	t.Setenv("ANTHROPIC_MODEL", "vendor/a:free")
	out.Reset()
	if code, err := runUsage(context.Background(), c, []string{"next"}); code != 0 || err != nil {
		t.Fatalf("%d %v", code, err)
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.CurrentProvider != "kilo" || result.CurrentModel != "vendor/b:free" {
		t.Fatal("used stale launch environment after switch")
	}
}
