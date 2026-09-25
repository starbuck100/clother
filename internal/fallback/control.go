package fallback

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jolehuit/clother/internal/budget"
)

type State struct {
	Route
	Pinned         bool            `json:"pinned"`
	FreeOnly       bool            `json:"free_only"`
	Pending        string          `json:"pending,omitempty"`
	Reason         string          `json:"reason"`
	ChangedAt      time.Time       `json:"changed_at"`
	Budget         *budget.Summary `json:"budget,omitempty"`
	QuotaCheckedAt time.Time       `json:"quota_checked_at,omitempty"`
	QuotaRemaining *int            `json:"quota_remaining,omitempty"`
}

func (r *Router) State() State { r.mu.Lock(); defer r.mu.Unlock(); return r.stateLocked() }
func (r *Router) stateLocked() State {
	return State{Route: r.Routes[r.active], Pinned: r.pinned, FreeOnly: r.freeOnly, Pending: r.pending, Reason: r.reason, ChangedAt: r.changedAt}
}
func (r *Router) publish() {
	if r.StateChanged != nil {
		r.StateChanged(r.State())
	}
}
func (r *Router) control(w http.ResponseWriter, req *http.Request) {
	if req.Method == "GET" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(r.State())
		return
	}
	if req.Method != "POST" {
		fail(w, 405, "POST required")
		return
	}
	var body struct {
		Action string `json:"action"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, req.Body, 1024)).Decode(&body) != nil {
		fail(w, 400, "invalid control")
		return
	}
	r.mu.Lock()
	switch body.Action {
	case "next":
		r.pinned = false
		r.pending = "next"
	case "pin":
		r.pinned = true
		r.pending = ""
		r.reason = "pinned by user"
	case "free":
		r.pinned = false
		r.freeOnly = true
		r.pending = "free"
	case "auto":
		r.pinned = false
		r.freeOnly = false
		r.pending = ""
		r.reason = "automatic selection enabled"
	default:
		r.mu.Unlock()
		fail(w, 400, "expected next, pin, free or auto")
		return
	}
	r.mu.Unlock()
	r.publish()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Clother: " + body.Action + " accepted; applies at the next request boundary. In-flight requests finish normally."})
}
func (r *Router) allowed(index int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return (!r.freeOnly || r.Routes[index].Free) && (!r.pinned || index == r.active)
}

// Called with the request gate held. Probing is bounded and never replays any
// conversation/tool request. A successful probe proves availability now, not a
// known remaining daily quota. No timer alone clears a provider quota.
func (r *Router) initialRoute(ctx context.Context, required int, images bool) (int, string) {
	r.mu.Lock()
	current, pending, pinned, last := r.active, r.pending, r.pinned, r.lastRecovery
	r.pending = ""
	r.mu.Unlock()
	if pending != "" {
		for i, v := range r.Routes {
			if (pending == "next" && i == current) || (pending == "free" && !v.Free) || !r.allowed(i) || v.Context < required || (images && !v.Images) {
				continue
			}
			if r.Check == nil || r.Check(ctx, v) {
				return i, "user requested " + pending
			}
		}
		if pending == "next" {
			return -1, "no eligible next model"
		}
	}
	interval := r.RecoveryInterval
	if interval == 0 {
		interval = 5 * time.Minute
	}
	if !pinned && !r.Routes[current].Free && r.Recover != nil && time.Since(last) >= interval {
		r.mu.Lock()
		r.lastRecovery = time.Now()
		r.mu.Unlock()
		// At most one eligible candidate per provider and two probes per interval.
		seen := map[string]bool{}
		for i, v := range r.Routes {
			if !v.Free || seen[v.Provider] || !r.allowed(i) || v.Context < required || (images && !v.Images) {
				continue
			}
			if r.Check != nil && !r.Check(ctx, v) {
				continue
			}
			seen[v.Provider] = true
			if r.Recover(ctx, v) {
				return i, "free availability confirmed by probe"
			}
			if len(seen) >= 2 {
				break
			}
		}
	}
	return current, ""
}

// ProbeTool performs a real, harmless two-turn tool round trip. It executes no
// external tool: Clother validates the call and supplies a fixed local result.
func ProbeTool(ctx context.Context, route Route) error {
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	r := Router{Client: &http.Client{Timeout: 35 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	tool := map[string]any{"name": "clother_health", "description": "Echo a diagnostic favicon. Call once.", "input_schema": map[string]any{"type": "object", "properties": map[string]any{"favicon": map[string]any{"type": "string", "minLength": 1}}, "required": []string{"favicon"}}}
	messages := []any{map[string]any{"role": "user", "content": "Call clother_health with favicon X. After its result, reply exactly OK."}}
	call := func(body map[string]any) (map[string]any, error) {
		raw, _ := json.Marshal(body)
		resp, e := r.call(ctx, route, raw)
		if e != nil {
			return nil, fmt.Errorf("probe connection failed")
		}
		defer resp.Body.Close()
		data, e := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
		if e != nil {
			return nil, fmt.Errorf("probe response incomplete")
		}
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("probe HTTP %d", resp.StatusCode)
		}
		var msg map[string]any
		if json.Unmarshal(data, &msg) != nil {
			return nil, fmt.Errorf("invalid probe response")
		}
		return msg, nil
	}
	body := map[string]any{"model": route.Model, "max_tokens": 512, "stream": false, "messages": messages, "tools": []any{tool}, "tool_choice": map[string]any{"type": "tool", "name": "clother_health"}}
	first, e := call(body)
	if e != nil {
		return e
	}
	blocks, _ := first["content"].([]any)
	id := ""
	calls := 0
	for _, v := range blocks {
		b, _ := v.(map[string]any)
		if b["type"] == "tool_use" {
			calls++
			args, _ := b["input"].(map[string]any)
			if b["name"] != "clother_health" || args["favicon"] != "X" {
				return fmt.Errorf("probe tool arguments invalid")
			}
			id, _ = b["id"].(string)
		}
	}
	if calls != 1 || id == "" {
		return fmt.Errorf("probe did not produce one valid tool call")
	}
	messages = append(messages, map[string]any{"role": "assistant", "content": blocks}, map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": id, "content": "OK"}}})
	body["messages"] = messages
	body["tool_choice"] = map[string]any{"type": "none"}
	second, e := call(body)
	if e != nil {
		return e
	}
	text := ""
	content, _ := second["content"].([]any)
	for _, v := range content {
		b, _ := v.(map[string]any)
		if b["type"] == "text" {
			s, _ := b["text"].(string)
			text += s
		}
		if b["type"] == "tool_use" {
			return fmt.Errorf("unexpected second tool call")
		}
	}
	if strings.TrimSpace(text) != "OK" {
		return fmt.Errorf("probe final reply was not OK")
	}
	return nil
}
