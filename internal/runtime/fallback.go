package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jolehuit/clother/internal/benchmarks"
	"github.com/jolehuit/clother/internal/budget"
	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/fallback"
	"github.com/jolehuit/clother/internal/kilo"
	"github.com/jolehuit/clother/internal/launchers"
	"github.com/jolehuit/clother/internal/openrouter"
	"github.com/jolehuit/clother/internal/platform"
	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/providers"
	"github.com/jolehuit/clother/internal/usage"
)

type routeAccount struct{ base, key, scope string }

func PrepareFallback(ctx context.Context, paths config.Paths, target profiles.Target, args, env []string) ([]string, func(), error) {
	noop := func() {}
	for _, arg := range args {
		if arg == "--version" || arg == "-v" || arg == "--help" || arg == "-h" {
			return env, noop, nil
		}
	}
	if (target.Family != providers.FamilyKilo && target.Family != providers.FamilyOpenRouter) || os.Getenv("CLOTHER_AUTO_FALLBACK") == "0" {
		return env, noop, nil
	}
	cfg, err := config.LoadConfig(paths.ConfigFile)
	if err != nil {
		return nil, noop, err
	}
	secrets, err := config.LoadSecrets(paths.SecretsFile)
	if err != nil {
		return nil, noop, err
	}
	builtins, err := providers.Load()
	if err != nil {
		return nil, noop, err
	}
	cfg.ApplyLegacySecrets(secrets, builtins)
	ledger := budget.Ledger{Dir: filepath.Join(paths.DataDir, "budget", "v1"), Session: budget.ID(), Config: budget.Defaults(cfg.Budget)}
	events, _ := usage.NewStore(paths.DataDir).Read(time.Now())
	values := envSliceToMap(env)
	model := ModelOverride(args)
	if model == "" {
		model = effectiveSessionModel(target, values)
	}
	initialProvider := string(target.Family)
	catalogs := map[string]openrouter.Catalog{}
	targets := map[string]profiles.Target{}
	accounts := map[string]routeAccount{}
	var cleaners []func()
	cleanup := func() {
		for i := len(cleaners) - 1; i >= 0; i-- {
			cleaners[i]()
		}
	}
	warn := func(s string) { fmt.Fprintln(os.Stderr, "Clother:", s) }
	for _, id := range []string{initialProvider, otherFree(initialProvider)} {
		t, e := profiles.Resolve(id, builtins, cfg)
		if e != nil {
			continue
		}
		if id == initialProvider {
			t = target
		}
		var cat openrouter.Catalog
		if id == "kilo" {
			cat, e = kilo.Load(ctx, t.BaseURL, paths.CacheDir, false)
		} else {
			cat, e = openrouter.Load(ctx, t.BaseURL, paths.CacheDir, false)
		}
		if e != nil {
			warn(id + " fallback catalog unavailable")
			continue
		}
		if id == initialProvider {
			initial, e := cat.Find(model)
			if e != nil || !initial.Free() {
				return env, cleanup, nil
			}
		}
		catalogs[id] = cat
		targets[id] = t
		key := secrets[t.SecretKey]
		accounts[id] = routeAccount{t.BaseURL, key, usage.Scope(id, t.BaseURL, key)}
	}
	original, e := catalogs[initialProvider].Find(model)
	if e != nil || !original.Free() {
		return env, cleanup, nil
	}
	evidence := benchmarks.Report{}
	if os.Getenv("CLOTHER_BENCHMARKS") != "0" {
		evidence = benchmarks.Load(ctx, paths.CacheDir)
	}
	routeFor := func(id string, m openrouter.Model, base, key string) fallback.Route {
		out := m.TopProvider.MaxCompletionTokens
		if out <= 0 {
			out = 4096
		}
		var matches []benchmarks.Row
		if !evidence.Stale {
			matches = evidence.Match(m.ID)
		}
		var mayTrain *bool
		if id == "kilo" {
			v := m.MayTrainOnPrompts
			mayTrain = &v
		}
		return fallback.Route{Local: usage.Measure(events, id, accounts[id].scope, m.ID, time.Now()), Provider: id, Model: m.ID, Base: base, Key: key, Context: m.ContextLimit(), Output: out, Free: m.Free(), Images: slices.Contains(m.Architecture.InputModalities, "image"), Reasoning: m.Supports("reasoning"), Benchmark: matches, MayTrain: mayTrain}
	}
	routes := []fallback.Route{routeFor(initialProvider, original, values["ANTHROPIC_BASE_URL"], values["ANTHROPIC_AUTH_TOKEN"])}
	for _, id := range []string{initialProvider, otherFree(initialProvider)} {
		cat, exists := catalogs[id]
		if !exists {
			continue
		}
		base, key := values["ANTHROPIC_BASE_URL"], values["ANTHROPIC_AUTH_TOKEN"]
		if id != initialProvider {
			account := accounts[id]
			if id == "openrouter" && account.key == "" {
				continue
			}
			transport := &usage.Transport{Store: usage.NewStore(paths.DataDir), Provider: id, AccountScope: account.scope, IsFree: func(model string) bool { m, e := cat.Find(model); return e == nil && m.Free() }, Warn: warn}
			var close func()
			if id == "kilo" {
				base, key, close, err = kilo.Start(ctx, account.base, account.key, cat, transport)
			} else {
				base, key, close, err = openrouter.StartSchemaProxy(ctx, account.base, account.key, transport)
			}
			if err != nil {
				warn(id + " fallback adapter unavailable")
				continue
			}
			cleaners = append(cleaners, close)
		}
		for _, m := range cat.FreeModels() {
			if id == initialProvider && m.ID == model {
				continue
			}
			routes = append(routes, routeFor(id, m, base, key))
		}
	}
	category := os.Getenv("CLOTHER_FALLBACK_CATEGORY")
	if category == "" {
		category = "Agentic Coding"
	}
	// Benchmarks are a tie-breaker only with an exact model/effort match. Unknown
	// stealth identities are never assigned another model's measurements.
	sort.SliceStable(routes[1:], func(i, j int) bool {
		a, b := routes[i+1], routes[j+1]
		score := func(r fallback.Route) float64 {
			v := r.Local.Score()
			if strings.TrimSuffix(r.Model, ":free") == strings.TrimSuffix(model, ":free") {
				v += 200
			}
			if r.Context >= original.ContextLimit() {
				v += 20
			}
			if !evidence.Stale && len(r.Benchmark) > 0 {
				v += r.Benchmark[0].Categories[category]
			}
			return v
		}
		return score(a) > score(b)
	})
	if cfg.FallbackPaid || os.Getenv("CLOTHER_PAID_FALLBACK") == "1" {
		paid := profiles.All(builtins, cfg)
		sort.SliceStable(paid, func(i, j int) bool { return paid[i].Profile == "deepseek" && paid[j].Profile != "deepseek" })
		for _, t := range paid {
			key := secrets[t.SecretKey]
			if key == "" || t.Model == "" || t.AuthMode != providers.AuthSecret || t.Family == providers.FamilyOpenRouter || t.Family == providers.FamilyKilo || strings.HasPrefix(t.Profile, "or-") {
				continue
			}
			// Other built-in/custom Anthropic-compatible keyed profiles retain their
			// configured model. Unknown capacities get a conservative 32k text bound.
			contextLimit, output, images := 32768, 4096, false
			if t.Profile == "deepseek" {
				t.Model = "deepseek-flash"
				contextLimit = 1000000
				output = 8192
				images = true
			}
			scope := usage.Scope(t.Profile, t.BaseURL, key)
			transport := &usage.Transport{Store: usage.NewStore(paths.DataDir), Provider: t.Profile, AccountScope: scope, IsFree: func(string) bool { return false }, Warn: warn}
			var priceMu sync.Mutex
			var currentPrice budget.Price
			transport.Before = func(raw []byte, event *usage.Event) (func(usage.Event), error) {
				priceMu.Lock()
				defer priceMu.Unlock()
				now := time.Now()
				if configured, ok := ledger.Config.Prices[t.Profile+"/"+event.Model]; ok {
					currentPrice = configured
				} else if t.Profile == "deepseek" && event.Model == "deepseek-flash" && strings.TrimRight(t.BaseURL, "/") == "https://api.deepseek.com/anthropic" && !currentPrice.Valid(now) {
					var err error
					currentPrice, err = budget.DeepSeek(ctx)
					if err != nil {
						return nil, fmt.Errorf("paid fallback blocked: current DeepSeek price unavailable")
					}
				}
				var body struct {
					Max int `json:"max_tokens"`
				}
				if json.Unmarshal(raw, &body) != nil || body.Max <= 0 || body.Max > output {
					return nil, fmt.Errorf("paid fallback blocked: invalid output limit")
				}
				return ledger.Reserve(currentPrice, contextLimit, body.Max)
			}
			base, token, close, e := openrouter.StartSchemaProxy(ctx, t.BaseURL, key, transport)
			if e != nil {
				continue
			}
			cleaners = append(cleaners, close)
			accounts[t.Profile] = routeAccount{t.BaseURL, key, scope}
			routes = append(routes, fallback.Route{Provider: t.Profile, Model: t.Model, Base: base, Key: token, Context: contextLimit, Output: output, Images: images})
		}
	}
	router := &fallback.Router{Routes: routes, Notice: func(s string) { fmt.Fprintln(os.Stderr, s) }, Client: &http.Client{Timeout: 10 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	var quotaMu sync.Mutex
	var quotaChecked time.Time
	var quotaOK bool
	var quotaRemaining *int
	router.Check = func(ctx context.Context, r fallback.Route) bool {
		account := accounts[r.Provider]
		events, e := usage.NewStore(paths.DataDir).Read(time.Now())
		if e != nil {
			return false
		}
		var scoped []usage.Event
		for _, event := range events {
			if event.Provider == r.Provider && event.Scope == account.scope {
				scoped = append(scoped, event)
			}
		}
		if usage.Measure(scoped, r.Provider, account.scope, r.Model, time.Now()).Cooling(time.Now()) {
			return false
		}
		for _, event := range usage.ActiveBlocks(scoped, time.Now()) {
			if event.Limit.Kind == "invalid_request" {
				continue
			}
			if event.Limit.Scope == "provider" || event.Limit.Scope == "unknown" || event.Model == r.Model {
				return false
			}
		}
		if r.Provider == "openrouter" {
			quotaMu.Lock()
			defer quotaMu.Unlock()
			if time.Since(quotaChecked) < 5*time.Second {
				return quotaOK
			}
			live, e := usage.FetchOpenRouter(ctx, account.base, account.key)
			quotaChecked = time.Now()
			quotaOK = false
			quotaRemaining = nil
			if e != nil {
				return false
			}
			if live.FreeDaily != nil {
				v := live.FreeDaily.Remaining
				quotaRemaining = &v
			}
			if r.Free && live.FreeDaily != nil && live.FreeDaily.Remaining == 0 {
				return false
			}
			if live.CreditRemaining != nil && *live.CreditRemaining <= 0 {
				return false
			}
			quotaOK = true
		}
		return true
	}
	router.Rank = func(r fallback.Route) float64 {
		events, e := usage.NewStore(paths.DataDir).Read(time.Now())
		if e != nil {
			return 0
		}
		return usage.Measure(events, r.Provider, accounts[r.Provider].scope, r.Model, time.Now()).Score()
	}
	router.Recover = func(ctx context.Context, r fallback.Route) bool { return fallback.ProbeTool(ctx, r) == nil }
	if os.Getenv("CLOTHER_FALLBACK_PLANNER") != "0" {
		if e := router.Warm(ctx, launchers.FreeFallbackSkill(), category); e != nil {
			warn("fallback planner unavailable; using validated catalog/benchmark order")
		} else {
			warn("fallback order prepared with " + routes[0].ID() + " (" + category + ")")
		}
	}
	base, token, close, e := router.Start()
	if e != nil {
		cleanup()
		return nil, noop, e
	}
	cleaners = append(cleaners, close)
	statusDir := filepath.Join(paths.DataDir, "usage", "sessions")
	_ = os.MkdirAll(statusDir, 0700)
	status, err := os.CreateTemp(statusDir, "route-*.json")
	if err != nil {
		cleanup()
		return nil, noop, err
	}
	statusPath := status.Name()
	_ = status.Close()
	var statusMu sync.Mutex
	router.StateChanged = func(state fallback.State) {
		statusMu.Lock()
		defer statusMu.Unlock()
		if summary, e := ledger.Summary(); e == nil {
			state.Budget = &summary
		}
		quotaMu.Lock()
		if state.Provider == "openrouter" && time.Since(quotaChecked) < time.Minute {
			state.QuotaRemaining = quotaRemaining
			state.QuotaCheckedAt = quotaChecked
		}
		quotaMu.Unlock()
		data, _ := json.Marshal(state)
		_ = platform.AtomicWrite(statusPath, data, 0600)
	}
	router.StateChanged(router.State())
	cleaners = append(cleaners, func() { _ = os.Remove(statusPath) })
	values["CLOTHER_ROUTE_STATUS"] = statusPath
	values["CLOTHER_BUDGET_SESSION"] = ledger.Session
	values["CLOTHER_CONTROL_URL"] = base
	values["CLOTHER_CONTROL_TOKEN"] = token
	values["ANTHROPIC_BASE_URL"] = base
	values["ANTHROPIC_AUTH_TOKEN"] = token
	values["ANTHROPIC_API_KEY"] = ""
	return flattenEnv(values), cleanup, nil
}
func otherFree(id string) string {
	if id == "kilo" {
		return "openrouter"
	}
	return "kilo"
}
