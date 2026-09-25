package usage

import (
	"strings"
	"time"
)

type Metrics struct {
	Samples             int       `json:"samples"`
	Successes           int       `json:"successful_responses"`
	ToolResponses       int       `json:"responses_with_generated_tools"`
	MeanMS              int64     `json:"mean_duration_ms"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	LastFailure         time.Time `json:"last_failure,omitempty"`
}

// Local response observations, not a coding-quality benchmark or proof that a
// generated tool was executed successfully. Quota failures are handled separately.
func Measure(events []Event, provider, scope, model string, now time.Time) Metrics {
	var m Metrics
	var duration int64
	for _, e := range events {
		if e.Provider != provider || e.Scope != scope || e.Model != model || now.Sub(e.At) > 24*time.Hour {
			continue
		}
		if e.Limit != nil && (e.Status == 429 || e.Status == 401 || e.Status == 402 || strings.HasPrefix(e.Limit.Kind, "free_") || e.Limit.Kind == "rate_limit" || e.Limit.Kind == "upstream_rate" || e.Limit.Kind == "authentication" || e.Limit.Kind == "credit_or_budget") {
			continue
		}
		m.Samples++
		if e.Outcome == "ok" {
			m.Successes++
			duration += e.DurationMS
			if e.ToolCalls > 0 {
				m.ToolResponses++
			}
			m.ConsecutiveFailures = 0
		} else {
			m.ConsecutiveFailures++
			m.LastFailure = e.At
		}
	}
	if m.Successes > 0 {
		m.MeanMS = duration / int64(m.Successes)
	}
	return m
}
func (m Metrics) Cooling(now time.Time) bool {
	return m.ConsecutiveFailures >= 3 && now.Sub(m.LastFailure) < 10*time.Minute
}
func (m Metrics) Score() float64 {
	if m.Samples < 3 {
		return 0
	}
	// Bayesian smoothing prevents one quick reply from dominating the selection.
	score := 40 * (float64(m.Successes+2)/float64(m.Samples+4) - 0.5)
	score += min(5, float64(m.ToolResponses))
	if m.MeanMS > 0 {
		score -= min(5, float64(m.MeanMS)/12000)
	}
	return score
}
