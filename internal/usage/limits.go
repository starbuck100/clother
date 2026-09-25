package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type FreeQuota struct {
	Used      int `json:"used"`
	Limit     int `json:"limit"`
	Remaining int `json:"remaining"`
}

func (q *FreeQuota) UnmarshalJSON(data []byte) error {
	var fields struct {
		Used      *int `json:"used"`
		Limit     *int `json:"limit"`
		Remaining *int `json:"remaining"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields.Used == nil || fields.Limit == nil || fields.Remaining == nil {
		return fmt.Errorf("incomplete free quota")
	}
	q.Used = *fields.Used
	q.Limit = *fields.Limit
	q.Remaining = *fields.Remaining
	return nil
}

type OpenRouterAccount struct {
	FreeDaily       *FreeQuota `json:"free_model_daily_requests"`
	UsedUSD         *float64   `json:"usage"`
	UsedTodayUSD    *float64   `json:"usage_daily"`
	CreditRemaining *float64   `json:"limit_remaining"`
	CreditReset     *string    `json:"limit_reset"`
	CheckedAt       time.Time  `json:"checked_at"`
	FreeResetAt     time.Time  `json:"free_reset_at"`
}

func FetchOpenRouter(ctx context.Context, base, key string) (OpenRouterAccount, error) {
	var account OpenRouterAccount
	if key == "" {
		return account, fmt.Errorf("OPENROUTER_API_KEY not configured")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(base, "/")+"/v1/key", nil)
	if err != nil {
		return account, fmt.Errorf("invalid OpenRouter endpoint")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return account, fmt.Errorf("OpenRouter usage check unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return account, fmt.Errorf("OpenRouter usage check: HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Data *OpenRouterAccount `json:"data"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 128<<10)).Decode(&payload) != nil || payload.Data == nil {
		return account, fmt.Errorf("invalid OpenRouter usage response")
	}
	account = *payload.Data
	account.CheckedAt = time.Now().UTC()
	account.FreeResetAt = NextUTCDay(account.CheckedAt)
	if q := account.FreeDaily; q != nil && (q.Used < 0 || q.Limit < 0 || q.Remaining < 0 || q.Remaining > q.Limit) {
		return OpenRouterAccount{}, fmt.Errorf("invalid OpenRouter free quota")
	}
	return account, nil
}

func NextUTCDay(now time.Time) time.Time {
	utc := now.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day()+1, 0, 0, 0, 0, time.UTC)
}

func numberHeader(h http.Header, name string) *int {
	n, err := strconv.Atoi(h.Get(name))
	if err != nil || n < 0 {
		return nil
	}
	return &n
}

func classify(provider string, status int, headers http.Header, payload map[string]any, now time.Time) *Limit {
	raw, _ := json.Marshal(payload["error"])
	text := strings.ToLower(string(raw)) // Inspect only in memory; never persist raw errors.
	if status == 200 {
		if errorValue, ok := payload["error"].(map[string]any); ok {
			if code, ok := errorValue["code"].(float64); ok {
				status = int(code)
			}
			if status == 200 && strings.Contains(text, "rate_limit") {
				status = 429
			}
		}
	}
	upstream := false
	if e, ok := payload["error"].(map[string]any); ok {
		if meta, ok := e["metadata"].(map[string]any); ok {
			name, _ := meta["provider_name"].(string)
			upstream = name != "" || meta["provider_code"] != nil
		}
	}
	var limit *Limit
	switch {
	case status == 429:
		limit = &Limit{Kind: "rate_limit", Scope: "unknown"}
		switch {
		case provider == "openrouter" && strings.Contains(text, "free-models-per-day"):
			limit.Kind = "free_daily"
			limit.Scope = "provider"
			reset := NextUTCDay(now)
			limit.ResetAt = &reset
			limit.ResetSource = "documented UTC day"
		case provider == "openrouter" && strings.Contains(text, "free-models-per-min"):
			limit.Kind = "free_minute"
			limit.Scope = "provider"
		case provider == "kilo" && strings.Contains(text, "rate limit exceeded for free models"):
			limit.Kind = "free_hour"
			limit.Scope = "provider"
		case strings.Contains(text, "upstream") || upstream:
			limit.Kind = "upstream_rate"
			limit.Scope = "model"
		}
	case status == 401:
		limit = &Limit{Kind: "authentication", Scope: "provider"}
	case status == 403:
		limit = &Limit{Kind: "policy", Scope: "unknown"}
	case status == 402:
		limit = &Limit{Kind: "credit_or_budget", Scope: "provider"}
		if strings.Contains(text, "openrouter_in_flight_budget") {
			limit.Kind = "in_flight_budget"
		}
	case status == 400:
		limit = &Limit{Kind: "invalid_request", Scope: "model"}
	case status >= 500:
		limit = &Limit{Kind: "provider_error", Scope: "model"}
	}
	if limit == nil {
		return nil
	}
	// OpenRouter may put the rate-limit headers inside error.metadata.headers.
	h := headers.Clone()
	if h == nil {
		h = http.Header{}
	}
	if e, ok := payload["error"].(map[string]any); ok {
		if meta, ok := e["metadata"].(map[string]any); ok {
			if nested, ok := meta["headers"].(map[string]any); ok {
				for k, v := range nested {
					if h.Get(k) == "" {
						h.Set(k, fmt.Sprint(v))
					}
				}
			}
		}
	}
	limit.Ceiling = numberHeader(h, "X-RateLimit-Limit")
	limit.Remaining = numberHeader(h, "X-RateLimit-Remaining")
	if raw := h.Get("X-RateLimit-Reset"); raw != "" {
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
			if n > 100000000000 {
				n /= 1000
			}
			if n > now.Unix()-60 && n < now.Add(366*24*time.Hour).Unix() {
				reset := time.Unix(n, 0).UTC()
				limit.ResetAt = &reset
				limit.ResetSource = "provider header"
			}
		}
	}
	if limit.ResetAt == nil {
		if seconds, err := strconv.Atoi(h.Get("Retry-After")); err == nil && seconds >= 0 {
			reset := now.Add(time.Duration(seconds) * time.Second)
			limit.ResetAt = &reset
			limit.ResetSource = "Retry-After"
		} else if reset, err := http.ParseTime(h.Get("Retry-After")); err == nil {
			limit.ResetAt = &reset
			limit.ResetSource = "Retry-After"
		}
	}
	return limit
}
