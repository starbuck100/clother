package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jolehuit/clother/internal/benchmarks"
	"github.com/jolehuit/clother/internal/fallback"
	"github.com/jolehuit/clother/internal/kilo"
	"github.com/jolehuit/clother/internal/openrouter"
	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/ui"
	"github.com/jolehuit/clother/internal/usage"
)

type providerUsage struct {
	Provider            string                   `json:"provider"`
	Credential          bool                     `json:"credential_configured"`
	Model               string                   `json:"configured_model"`
	Today               usage.Totals             `json:"local_today_utc"`
	Hour                usage.Totals             `json:"local_last_hour"`
	Month               usage.Totals             `json:"local_last_30_days"`
	FreeHour            usage.Totals             `json:"local_free_last_hour"`
	Models              map[string]usage.Totals  `json:"local_models_today_utc"`
	Blocks              []usage.Event            `json:"active_observations"`
	Account             *usage.OpenRouterAccount `json:"openrouter_account,omitempty"`
	CheckError          string                   `json:"check_error,omitempty"`
	ExactRemainingKnown bool                     `json:"exact_free_remaining_known"`
	Note                string                   `json:"note"`
	Events              []usage.Event            `json:"-"`
	Catalog             openrouter.Catalog       `json:"-"`
	CatalogError        string                   `json:"catalog_error,omitempty"`
}

type usageReport struct {
	Benchmark          *benchmarks.Report `json:"benchmark_source,omitempty"`
	ActiveRoute        *fallback.Route    `json:"active_session_route,omitempty"`
	At                 time.Time          `json:"checked_at"`
	Providers          []providerUsage    `json:"providers"`
	CurrentProvider    string             `json:"current_provider,omitempty"`
	CurrentModel       string             `json:"current_model,omitempty"`
	RequiredContext    int                `json:"required_context_estimate,omitempty"`
	NeedImages         bool               `json:"requires_images,omitempty"`
	Candidates         []candidate        `json:"candidates,omitempty"`
	RecommendationNote string             `json:"recommendation_note,omitempty"`
}

func loadUsage(ctx context.Context, c Context, now time.Time, next bool) (usageReport, error) {
	report := usageReport{At: now}
	if path := os.Getenv("CLOTHER_ROUTE_STATUS"); path != "" {
		if data, e := os.ReadFile(path); e == nil {
			var route fallback.Route
			if json.Unmarshal(data, &route) == nil {
				report.ActiveRoute = &route
			}
		}
	}
	events, err := usage.NewStore(c.Paths.DataDir).Read(now)
	if err != nil {
		return report, err
	}
	today := usage.NextUTCDay(now).Add(-24 * time.Hour)
	ids := []string{"openrouter", "kilo"}
	seen := map[string]bool{"openrouter": true, "kilo": true}
	for _, event := range events {
		if !seen[event.Provider] {
			if _, e := profiles.Resolve(event.Provider, c.Catalog, c.Config); e == nil {
				ids = append(ids, event.Provider)
				seen[event.Provider] = true
			}
		}
	}
	for _, id := range ids {
		target, err := profiles.Resolve(id, c.Catalog, c.Config)
		if err != nil {
			return report, err
		}
		key := c.Secrets[target.SecretKey]
		entry := providerUsage{Provider: id, Credential: key != "", Model: target.Model, Models: map[string]usage.Totals{}}
		scope := usage.Scope(id, target.BaseURL, key)
		for _, event := range events {
			if event.Provider == id && event.Scope == scope {
				entry.Events = append(entry.Events, event)
			}
		}
		entry.Today = usage.Sum(entry.Events, today)
		entry.Hour = usage.Sum(entry.Events, now.Add(-time.Hour))
		entry.Month = usage.Sum(entry.Events, now.Add(-30*24*time.Hour))
		var free []usage.Event
		models := map[string][]usage.Event{}
		for _, event := range entry.Events {
			if event.Free {
				free = append(free, event)
			}
			models[event.Model] = append(models[event.Model], event)
		}
		entry.FreeHour = usage.Sum(free, now.Add(-time.Hour))
		for model, items := range models {
			entry.Models[model] = usage.Sum(items, today)
		}
		entry.Blocks = usage.ActiveBlocks(entry.Events, now)
		if id == "openrouter" {
			entry.Note = "API daily free counter is account-wide; local tokens cover only this Clother installation and credential. Credit balance is separate from free requests."
			account, err := usage.FetchOpenRouter(ctx, target.BaseURL, key)
			if err != nil {
				entry.CheckError = err.Error()
			} else {
				entry.Account = &account
				entry.ExactRemainingKnown = account.FreeDaily != nil
				// A fresh successful account check supersedes an older daily/auth block.
				var retained []usage.Event
				for _, event := range entry.Blocks {
					if event.Limit.Kind == "authentication" {
						continue
					}
					if event.Limit.Kind == "free_daily" && account.FreeDaily != nil && account.FreeDaily.Remaining > 0 {
						continue
					}
					retained = append(retained, event)
				}
				entry.Blocks = retained

			}
		} else if id == "kilo" {
			entry.Note = "Kilo documents 200 free requests/hour/IP, shared across models and keys. Exact remaining/reset is not exposed by a documented counter endpoint. Local attempts do not include other apps/devices and are not remaining quota."
		} else {
			entry.Note = "Configured keyed fallback: local measured usage only; provider balance/reset unknown."
		}
		if next && (id == "kilo" || id == "openrouter") {
			if id == "kilo" {
				entry.Catalog, err = kilo.Load(ctx, target.BaseURL, c.Paths.CacheDir, true)
			} else {
				entry.Catalog, err = openrouter.Load(ctx, target.BaseURL, c.Paths.CacheDir, true)
			}
			if err != nil {
				entry.CatalogError = err.Error()
			}
		}
		report.Providers = append(report.Providers, entry)
	}
	return report, nil
}

func runUsage(ctx context.Context, c Context, args []string) (int, error) {
	next := len(args) > 0 && args[0] == "next"
	if next {
		args = args[1:]
	}
	if (!next && len(args) > 1) || len(args) > 2 {
		return 2, fmt.Errorf("usage: clother usage [openrouter|kilo] or clother usage next [provider] [model]")
	}
	provider := ""
	if len(args) > 0 {
		provider = args[0]
	}
	if provider != "" && provider != "openrouter" && provider != "kilo" {
		return 2, fmt.Errorf("usage supports openrouter and kilo")
	}
	if len(args) == 2 && !profiles.IsModelTag(args[1]) {
		return 2, fmt.Errorf("invalid model ID")
	}
	report, err := loadUsage(ctx, c, time.Now().UTC(), next)
	if err != nil {
		return 1, err
	}
	if next {
		current, model := provider, ""
		if current == "" {
			current = sessionProfile()
		}
		if current == "" {
			current = activeSelection(c).Profile
			model = activeSelection(c).Model
		}
		if current == sessionProfile() && os.Getenv("ANTHROPIC_MODEL") != "" {
			model = os.Getenv("ANTHROPIC_MODEL")
		}
		if len(args) == 2 {
			model = args[1]
		}
		if strings.HasPrefix(current, "or-") {
			target, err := profiles.Resolve(current, c.Catalog, c.Config)
			if err == nil {
				current = "openrouter"
				model = target.Model
			}
		}
		if current != "openrouter" && current != "kilo" {
			return 2, fmt.Errorf("name the current free provider: clother usage next kilo <model> or clother usage next openrouter <model>")
		}
		for _, p := range report.Providers {
			if p.Provider == current && model == "" {
				model = p.Model
			}
		}
		report.CurrentProvider = current
		report.CurrentModel = model
		recommend(&report)
		if os.Getenv("CLOTHER_BENCHMARKS") != "0" {
			bench := benchmarks.Load(ctx, c.Paths.CacheDir)
			for i := range report.Candidates {
				report.Candidates[i].Benchmarks = bench.Match(report.Candidates[i].Model)
			}
			bench.Rows = nil
			report.Benchmark = &bench
		}
	} else if provider != "" {
		for _, p := range report.Providers {
			if p.Provider == provider {
				report.Providers = []providerUsage{p}
				break
			}
		}
	}
	if c.Output.Format == ui.FormatJSON {
		return 0, json.NewEncoder(c.Output.Stdout).Encode(report)
	}
	fmt.Fprintln(c.Output.Stdout, "Clother usage (tracked from v3.5.0 onward; previous sessions are not reconstructed)")
	if report.ActiveRoute != nil {
		fmt.Fprintf(c.Output.Stdout, "Active session backend: %s / %s (free: %t)\n", report.ActiveRoute.Provider, report.ActiveRoute.Model, report.ActiveRoute.Free)
	}
	for _, p := range report.Providers {
		fmt.Fprintf(c.Output.Stdout, "\n%s | %s\n", p.Provider, p.Model)
		fmt.Fprintf(c.Output.Stdout, "  Today UTC: %d requests, %d successful, %d failed/incomplete\n", p.Today.Attempts, p.Today.Succeeded, p.Today.Failed)
		fmt.Fprintf(c.Output.Stdout, "  Reported tokens: %d input / %d output | cache read %d / write %d (%d/%d requests measured)\n", p.Today.Input, p.Today.Output, p.Today.CacheRead, p.Today.CacheWrite, p.Today.Measured, p.Today.Attempts)
		fmt.Fprintf(c.Output.Stdout, "  Last hour: %d requests | last 30 days: %d requests\n", p.Hour.Attempts, p.Month.Attempts)
		names := make([]string, 0, len(p.Models))
		for name, total := range p.Models {
			if total.Attempts > 0 {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		for _, name := range names {
			total := p.Models[name]
			fmt.Fprintf(c.Output.Stdout, "    %s: %d requests, %d input / %d output tokens\n", name, total.Attempts, total.Input, total.Output)
		}
		if p.Today.CostMeasured > 0 {
			fmt.Fprintf(c.Output.Stdout, "  Reported cost: $%.6f (%d/%d requests; missing costs excluded)\n", p.Today.KnownCost, p.Today.CostMeasured, p.Today.Attempts)
		}
		if p.Account != nil && p.Account.FreeDaily != nil {
			q := p.Account.FreeDaily
			state := "available"
			if q.Remaining == 0 {
				state = "EXHAUSTED (reported daily policy)"
			}
			fmt.Fprintf(c.Output.Stdout, "  Free daily: %d/%d used, %d remaining - %s\n  UTC-day reset: %s\n", q.Used, q.Limit, q.Remaining, state, p.Account.FreeResetAt.Local().Format(time.RFC3339))
		} else {
			fmt.Fprintln(c.Output.Stdout, "  Free remaining: unknown")
		}
		if p.Provider == "kilo" {
			fmt.Fprintf(c.Output.Stdout, "  Locally observed free requests in last hour: %d (not a provider remaining counter)\n", p.FreeHour.Attempts)
		}
		if p.CheckError != "" {
			fmt.Fprintf(c.Output.Stdout, "  Live check: %s\n", p.CheckError)
		}
		for _, event := range p.Blocks {
			reset := "unknown"
			if event.Limit.ResetAt != nil {
				reset = event.Limit.ResetAt.Local().Format(time.RFC3339) + " (" + event.Limit.ResetSource + ")"
			}
			fmt.Fprintf(c.Output.Stdout, "  Observed %s: %s | scope %s | model %s | retry/reset %s\n", event.At.Local().Format(time.RFC3339), event.Limit.Kind, event.Limit.Scope, event.Model, reset)
		}
		fmt.Fprintf(c.Output.Stdout, "  %s\n", p.Note)
		if p.CatalogError != "" {
			fmt.Fprintf(c.Output.Stdout, "  Catalog: %s\n", p.CatalogError)
		}
	}
	if next {
		fmt.Fprintf(c.Output.Stdout, "\nNext free model for %s / %s\n%s\n", report.CurrentProvider, report.CurrentModel, report.RecommendationNote)
		if len(report.Candidates) == 0 {
			fmt.Fprintln(c.Output.Stdout, "  No eligible free candidate. Wait for a reported reset or resolve the listed provider errors.")
		}
		for i, candidate := range report.Candidates {
			fmt.Fprintf(c.Output.Stdout, "  %d. %s / %s | context %d | %s\n     clother %s --model %s --yolo\n", i+1, candidate.Provider, candidate.Model, candidate.Context, strings.Join(candidate.Reasons, "; "), candidate.Provider, candidate.Model)
			for _, row := range candidate.Benchmarks {
				data, _ := json.Marshal(row.Categories)
				fmt.Fprintf(c.Output.Stdout, "     LiveBench %s: %s\n", row.Model, data)
			}
		}
		if b := report.Benchmark; b != nil {
			fmt.Fprintf(c.Output.Stdout, "Benchmark: %s | dataset %s | source modified %s | fetched %s | stale: %t | %s\n", b.Source, b.Dataset, b.Modified, b.Fetched.Format(time.RFC3339), b.Stale, b.Error)
		}
	}
	return 0, nil
}
