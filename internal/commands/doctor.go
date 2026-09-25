package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jolehuit/clother/internal/budget"
	"github.com/jolehuit/clother/internal/fallback"
	"github.com/jolehuit/clother/internal/kilo"
	"github.com/jolehuit/clother/internal/openrouter"
	"github.com/jolehuit/clother/internal/platform"
	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/runtime"
	"github.com/jolehuit/clother/internal/ui"
	"github.com/jolehuit/clother/internal/usage"
)

type doctorProvider struct {
	quota    *usage.FreeQuota
	Provider string `json:"provider"`
	Key      bool   `json:"key_configured"`
	Catalog  bool   `json:"catalog_reachable"`
	Model    string `json:"model"`
	Context  int    `json:"context_tokens"`
	Output   int    `json:"max_output_tokens"`
	Free     bool   `json:"free"`
	Auth     string `json:"authentication"`
	Probe    string `json:"tool_probe"`
}

func runDoctor(ctx context.Context, c Context, args []string) (int, error) {
	provider, model := "", ""
	probe, jsonOutput := false, c.Output.Format == ui.FormatJSON
	for _, arg := range args {
		switch arg {
		case "probe":
			probe = true
		case "--json":
			jsonOutput = true
		default:
			if provider == "" {
				provider = arg
			} else if model == "" {
				model = arg
			} else {
				return 2, fmt.Errorf("usage: clother doctor [kilo|openrouter] [model] [probe] [--json]")
			}
		}
	}
	if provider != "" && provider != "kilo" && provider != "openrouter" {
		return 2, fmt.Errorf("doctor provider must be kilo or openrouter")
	}
	if probe && provider == "" {
		return 2, fmt.Errorf("select one provider for the bounded free tool probe")
	}
	_, nativeErr := runtime.FindRealClaude(c.Paths)
	_, launcherErr := os.Stat(filepath.Join(c.Paths.BinDir, platform.BinaryName()))
	_, lockErr := os.Stat(filepath.Join(c.Paths.DataDir, "budget", "v1", "lock"))
	budgetInfo, ledgerErr := (budget.Ledger{Dir: filepath.Join(c.Paths.DataDir, "budget", "v1"), Session: os.Getenv("CLOTHER_BUDGET_SESSION"), Config: budget.Defaults(c.Config.Budget)}).Summary()
	report := doctorReport{Native: nativeErr == nil, Launcher: launcherErr == nil, BudgetReadable: ledgerErr == nil, BudgetLock: lockErr == nil, Note: "Metadata-only diagnostics; no keys, prompts or raw provider errors. A catalog is not proof of inference. Probe sends two small synthetic free requests; quota may be consumed."}
	code := 0
	if nativeErr != nil || ledgerErr != nil {
		code = 1
	}
	for _, id := range []string{"kilo", "openrouter"} {
		if provider != "" && id != provider {
			continue
		}
		target, e := profiles.Resolve(id, c.Catalog, c.Config)
		if e != nil {
			return 1, e
		}
		key := c.Secrets[target.SecretKey]
		d := doctorProvider{Provider: id, Key: key != "", Model: target.Model, Auth: "not tested", Probe: "not run"}
		if model != "" {
			d.Model = model
		}
		var catalog openrouter.Catalog
		if id == "kilo" {
			catalog, e = kilo.Load(ctx, target.BaseURL, c.Paths.CacheDir, true)
		} else {
			catalog, e = openrouter.Load(ctx, target.BaseURL, c.Paths.CacheDir, true)
		}
		if e == nil {
			d.Catalog = true
			m, lookup := catalog.Find(d.Model)
			if lookup == nil && m.Validate() == nil {
				d.Context = m.ContextLimit()
				d.Output = m.TopProvider.MaxCompletionTokens
				d.Free = m.Free()
			} else {
				code = 1
			}
		} else {
			code = 1
		}
		if id == "openrouter" && key != "" {
			if account, e := usage.FetchOpenRouter(ctx, target.BaseURL, key); e == nil {
				d.Auth = "passed"
				d.quota = account.FreeDaily
				if account.FreeDaily != nil && account.FreeDaily.Remaining == 0 {
					d.Auth = "free quota exhausted"
				}
			} else {
				d.Auth = "failed"
				code = 1
			}
		} else if id == "openrouter" {
			d.Auth = "key missing"
		} else if key == "" {
			d.Auth = "anonymous; inferred only by probe"
		}
		if probe {
			if !d.Catalog || !d.Free || d.Context == 0 || (id == "openrouter" && (key == "" || d.Auth != "passed")) {
				d.Probe = "blocked: a compatible live free model and credentials are required"
				code = 1
			} else {
				tr := &usage.Transport{Store: usage.NewStore(c.Paths.DataDir), Provider: id, AccountScope: usage.Scope(id, target.BaseURL, key), IsFree: func(string) bool { return true }}
				var base, token string
				var close func()
				if id == "kilo" {
					base, token, close, e = kilo.Start(ctx, target.BaseURL, key, catalog, tr)
				} else {
					base, token, close, e = openrouter.StartSchemaProxy(ctx, target.BaseURL, key, tr)
				}
				if e != nil {
					d.Probe = "adapter failed"
					code = 1
				} else {
					e = fallback.ProbeTool(ctx, fallback.Route{Provider: id, Model: d.Model, Base: base, Key: token, Free: true})
					close()
					if e != nil {
						d.Probe = e.Error()
						code = 1
					} else {
						d.Probe = "passed: tool schema, call, local result and final OK"
					}
				}
			}
		}
		report.Providers = append(report.Providers, d)
	}
	if jsonOutput {
		enc := json.NewEncoder(c.Output.Stdout)
		enc.SetIndent("", "  ")
		return code, enc.Encode(report)
	}
	printDoctor(c.Output, report, budgetInfo, os.Getenv("CLOTHER_BUDGET_SESSION") != "")
	return code, nil
}

type doctorReport struct {
	Native         bool             `json:"native_claude_found"`
	Launcher       bool             `json:"clother_launcher_found"`
	BudgetReadable bool             `json:"budget_readable"`
	BudgetLock     bool             `json:"budget_lock_present"`
	Providers      []doctorProvider `json:"providers"`
	Note           string           `json:"note"`
}
