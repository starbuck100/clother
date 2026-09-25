package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/jolehuit/clother/internal/budget"
	"github.com/jolehuit/clother/internal/fallback"
)

func sessionControl(ctx context.Context, c Context, action string) (int, error) {
	base, token := os.Getenv("CLOTHER_CONTROL_URL"), os.Getenv("CLOTHER_CONTROL_TOKEN")
	u, e := url.Parse(base)
	if e != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || len(token) != 64 {
		return 2, fmt.Errorf("no live Clother router in this session")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	raw, _ := json.Marshal(map[string]string{"action": action})
	req, _ := http.NewRequestWithContext(ctx, "POST", base+"/clother/control", strings.NewReader(string(raw)))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	client := http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, e := client.Do(req)
	if e != nil {
		return 1, fmt.Errorf("Clother session router unreachable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 1, fmt.Errorf("Clother control rejected (HTTP %d)", resp.StatusCode)
	}
	var result struct {
		Message string `json:"message"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&result) != nil {
		return 1, fmt.Errorf("invalid control response")
	}
	fmt.Fprintln(c.Output.Stdout, result.Message)
	return 0, nil
}
func statusText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}
func sessionStatusline(c Context) (int, error) {
	var state fallback.State
	data, e := os.ReadFile(os.Getenv("CLOTHER_ROUTE_STATUS"))
	if e != nil || json.Unmarshal(data, &state) != nil || state.Model == "" {
		fmt.Fprintln(c.Output.Stdout, "Clother | backend unavailable")
		return 0, nil
	}
	var input struct {
		Context struct {
			Current struct {
				Input      int `json:"input_tokens"`
				CacheRead  int `json:"cache_read_input_tokens"`
				CacheWrite int `json:"cache_creation_input_tokens"`
			} `json:"current_usage"`
		} `json:"context_window"`
	}
	_ = json.NewDecoder(io.LimitReader(os.Stdin, 256<<10)).Decode(&input)
	mode := "Free"
	if !state.Free {
		mode = "Paid"
	}
	used := input.Context.Current.Input + input.Context.Current.CacheRead + input.Context.Current.CacheWrite
	contextText := "context ?"
	if state.Context > 0 && used > 0 {
		contextText = fmt.Sprintf("context %.0f%% (%d/%d)", 100*float64(used)/float64(state.Context), used, state.Context)
	}
	quota := "quota ?"
	if state.Free && state.QuotaRemaining != nil && time.Since(state.QuotaCheckedAt) < time.Minute {
		quota = fmt.Sprintf("free remaining %d (last check)", *state.QuotaRemaining)
	}
	parts := []string{statusText(state.Provider + "/" + state.Model), mode, contextText, quota}
	ledger := budget.Ledger{Dir: filepath.Join(c.Paths.DataDir, "budget", "v1"), Session: os.Getenv("CLOTHER_BUDGET_SESSION"), Config: budget.Defaults(c.Config.Budget)}
	if state.Budget != nil {
		ledger.Config.Daily = state.Budget.DailyLimit
		ledger.Config.Session = state.Budget.SessionLimit
	}
	if b, e := ledger.Summary(); e == nil {
		parts = append(parts, fmt.Sprintf("budget day $%.3f/$%.2f session $%.3f/$%.2f (est.+reserved)", b.Daily, b.DailyLimit, b.Session, b.SessionLimit))
	} else {
		parts = append(parts, "budget unavailable")
	}
	if state.Pinned {
		parts = append(parts, "PINNED")
	}
	if state.FreeOnly {
		parts = append(parts, "FREE ONLY")
	}
	if state.Pending != "" {
		parts = append(parts, "pending "+statusText(state.Pending))
	}
	parts = append(parts, statusText(state.Reason))
	fmt.Fprintln(c.Output.Stdout, strings.Join(parts, " | "))
	return 0, nil
}
