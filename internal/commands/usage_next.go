package commands

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jolehuit/clother/internal/benchmarks"
	"github.com/jolehuit/clother/internal/openrouter"
	"github.com/jolehuit/clother/internal/usage"
)

type candidate struct {
	Local      usage.Metrics    `json:"local_observations"`
	Benchmarks []benchmarks.Row `json:"benchmark_evidence,omitempty"`
	Provider   string           `json:"provider"`
	Model      string           `json:"model"`
	Context    int              `json:"context_tokens"`
	Output     int              `json:"max_output_tokens"`
	MayTrain   *bool            `json:"may_train_on_prompts,omitempty"`
	Score      int              `json:"score"`
	Reasons    []string         `json:"reasons"`
}

func providerBlocked(p providerUsage) bool {
	if p.Provider == "openrouter" && (!p.Credential || p.CheckError != "") {
		return true
	}
	if p.Account != nil && p.Account.FreeDaily != nil && p.Account.FreeDaily.Remaining == 0 {
		return true
	}
	for _, e := range p.Blocks {
		if e.Limit.Scope == "provider" {
			return true
		}
	}
	return false
}

func recommend(r *usageReport) {
	var current openrouter.Model
	for _, p := range r.Providers {
		if p.Provider == r.CurrentProvider {
			current, _ = p.Catalog.Find(r.CurrentModel)
			for i := len(p.Events) - 1; i >= 0; i-- {
				e := p.Events[i]
				if e.Model != r.CurrentModel || r.At.Sub(e.At) > 24*time.Hour {
					continue
				}
				r.NeedImages = r.NeedImages || e.Images
				if e.Input != nil {
					input := *e.Input
					// Anthropic cache tokens are separate; OpenAI prompt_tokens already
					// includes cache hits. Leave a safety margin for tokenizer differences.
					if p.Provider == "openrouter" {
						if e.CacheRead != nil {
							input += *e.CacheRead
						}
						if e.CacheWrite != nil {
							input += *e.CacheWrite
						}
					}
					r.RequiredContext = input + input/5 + 4096
					break
				}
			}
		}
	}
	r.RecommendationNote = "Both live catalogs checked. Ranking uses capability fit and observed success, not a quality benchmark. Availability is untested; no inference or provider switch is performed."
	if r.RequiredContext > 0 {
		r.RecommendationNote += fmt.Sprintf(" Context estimate including output reserve: %d tokens.", r.RequiredContext)
	} else {
		r.RecommendationNote += " Current history size is unknown; check context before resuming."
	}
	for _, p := range r.Providers {
		if providerBlocked(p) || p.CatalogError != "" {
			continue
		}
		for _, m := range p.Catalog.FreeModels() {
			if p.Provider == r.CurrentProvider && m.ID == r.CurrentModel {
				continue
			}
			if m.ContextLimit() < r.RequiredContext || (r.NeedImages && !slices.Contains(m.Architecture.InputModalities, "image")) {
				continue
			}
			blocked := false
			for _, e := range p.Blocks {
				if e.Model == m.ID {
					blocked = true
					break
				}
			}
			if blocked {
				continue
			}
			item := candidate{Provider: p.Provider, Model: m.ID, Context: m.ContextLimit(), Output: m.TopProvider.MaxCompletionTokens, Reasons: []string{"free pricing and tool support advertised"}}
			scope := ""
			if len(p.Events) > 0 {
				scope = p.Events[0].Scope
			}
			item.Local = usage.Measure(p.Events, p.Provider, scope, m.ID, r.At)
			if item.Local.Cooling(r.At) {
				continue
			}
			item.Score += int(item.Local.Score())
			if item.Local.Samples > 0 {
				item.Reasons = append(item.Reasons, fmt.Sprintf("local 24h: %d/%d successful responses, %d tool responses, mean %d ms", item.Local.Successes, item.Local.Samples, item.Local.ToolResponses, item.Local.MeanMS))
			}
			if p.Provider == "kilo" {
				value := m.MayTrainOnPrompts
				item.MayTrain = &value
			}
			normalize := func(id string) string { return strings.TrimSuffix(id, ":free") }
			if normalize(m.ID) == normalize(r.CurrentModel) {
				item.Score += 60
				item.Reasons = append(item.Reasons, "same model on another gateway")
			}
			if current.ContextLimit() > 0 && m.ContextLimit() >= current.ContextLimit() {
				item.Score += 15
				item.Reasons = append(item.Reasons, "preserves advertised context capacity")
			}
			if p.Provider == r.CurrentProvider {
				item.Score += 5
			}
			for i := len(p.Events) - 1; i >= 0; i-- {
				e := p.Events[i]
				if e.Model == m.ID && e.Outcome == "ok" && r.At.Sub(e.At) < 24*time.Hour {
					item.Score += 20
					item.Reasons = append(item.Reasons, "upstream request completed locally within 24h")
					break
				}
			}
			if item.Output >= 4096 {
				item.Score += 5
			}
			if m.MayTrainOnPrompts {
				item.Reasons = append(item.Reasons, "provider may train on prompts")
			}
			r.Candidates = append(r.Candidates, item)
		}
	}
	sort.SliceStable(r.Candidates, func(i, j int) bool {
		a, b := r.Candidates[i], r.Candidates[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.Provider != b.Provider {
			return a.Provider < b.Provider
		}
		return a.Model < b.Model
	})
}
