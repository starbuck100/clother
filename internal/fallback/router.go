// Package fallback keeps provider changes inside the running Claude session.
package fallback

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jolehuit/clother/internal/benchmarks"
	"github.com/jolehuit/clother/internal/usage"
)

type Route struct {
	Local     usage.Metrics    `json:"local_observations"`
	MayTrain  *bool            `json:"may_train_on_prompts,omitempty"`
	Provider  string           `json:"provider"`
	Model     string           `json:"model"`
	Base      string           `json:"-"`
	Key       string           `json:"-"`
	Context   int              `json:"context_tokens"`
	Output    int              `json:"max_output_tokens"`
	Free      bool             `json:"free"`
	Images    bool             `json:"images"`
	Reasoning bool             `json:"-"`
	Benchmark []benchmarks.Row `json:"benchmark_evidence,omitempty"`
}

func (r Route) ID() string { return r.Provider + "/" + r.Model }

type Router struct {
	StateChanged            func(State)
	Recover                 func(context.Context, Route) bool
	Rank                    func(Route) float64
	RecoveryInterval        time.Duration
	gate                    chan struct{}
	pinned, freeOnly        bool
	pending, reason         string
	changedAt, lastRecovery time.Time
	Routes                  []Route
	Check                   func(context.Context, Route) bool
	Notice                  func(string)
	Changed                 func(Route)
	Client                  *http.Client
	mu                      sync.Mutex
	active                  int
	token                   string
}

func (r *Router) Start() (string, string, func(), error) {
	if len(r.Routes) == 0 {
		return "", "", nil, fmt.Errorf("no fallback routes")
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", "", nil, err
	}
	r.token = hex.EncodeToString(random)
	r.gate = make(chan struct{}, 1)
	r.changedAt = time.Now()
	r.lastRecovery = time.Now()
	r.reason = "session started"
	if r.Client == nil {
		r.Client = &http.Client{Timeout: 10 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", "", nil, err
	}
	server := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: r}
	go server.Serve(listener)
	return "http://" + listener.Addr().String(), r.token, func() { _ = server.Close() }, nil
}
func fail(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "error", "error": map[string]any{"type": "api_error", "message": message}})
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	key := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
	if key == "" {
		key = req.Header.Get("x-api-key")
	}
	if subtle.ConstantTimeCompare([]byte(key), []byte(r.token)) != 1 {
		fail(w, 401, "invalid session token")
		return
	}
	if req.URL.Path == "/clother/control" {
		r.control(w, req)
		return
	}
	if req.Method != "POST" || req.URL.Path != "/v1/messages" {
		fail(w, 501, "automatic routing supports Messages; exact token counting unavailable")
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, req.Body, 32<<20))
	if err != nil {
		fail(w, 413, "request too large")
		return
	}
	var body map[string]any
	if json.Unmarshal(raw, &body) != nil {
		fail(w, 400, "invalid Messages request")
		return
	}
	// Serialize request boundaries, including complete streams. Controls remain
	// responsive and affect the next request, never an in-flight tool stream.
	if r.gate != nil {
		select {
		case r.gate <- struct{}{}:
			defer func() { <-r.gate }()
		case <-req.Context().Done():
			return
		}
	}
	r.mu.Lock()
	initial := r.active
	r.mu.Unlock()
	// A conservative UTF-8 byte bound avoids claiming exact cross-tokenizer fit.
	// Images require their advertised modality as well as a per-image reserve.
	required := len(raw) + 4096
	images := bytes.Count(raw, []byte(`"image"`))
	required += images * 8192
	tried := map[int]bool{}
	blocked := map[string]bool{}
	chosen, switchReason := r.initialRoute(req.Context(), required, images > 0)
	if chosen < 0 {
		fail(w, 429, "Clother: no eligible next model; current selection retained")
		r.publish()
		return
	}
	attempts := 0
	notice := ""
	for attempts < 6 {
		route := r.Routes[chosen]
		eligible := r.allowed(chosen) && !tried[chosen] && !blocked[route.Provider] && route.Context >= required && (images == 0 || route.Images)
		if eligible && r.Check != nil {
			eligible = r.Check(req.Context(), route)
		}
		if !eligible {
			tried[chosen] = true
			chosen = r.next(tried, blocked, required, images > 0)
			if chosen < 0 {
				break
			}
			continue
		}
		tried[chosen] = true
		attempts++
		if chosen != initial {
			notice = fmt.Sprintf("Clother: switched %s → %s (%s).", r.Routes[initial].ID(), route.ID(), price(route))
			if r.Notice != nil {
				r.Notice(notice)
			}
		}
		payload, err := rewrite(body, route, required, chosen != 0)
		if err == nil && chosen != 0 {
			var routed map[string]any
			_ = json.Unmarshal(payload, &routed)
			context := fmt.Sprintf("Clother gateway runtime status: this request is served by %s (%s). Clother, outside the model, manages backend changes and inserts visible 'Clother' status text in responses. Those notices are accurate runtime metadata, not actions taken by the assistant. Do not deny or ask the user to ignore them. Continue the task normally.", route.ID(), price(route))
			blocks := []any{map[string]any{"type": "text", "text": context}}
			switch old := routed["system"].(type) {
			case string:
				blocks = append(blocks, map[string]any{"type": "text", "text": old})
			case []any:
				blocks = append(blocks, old...)
			}
			routed["system"] = blocks
			payload, err = json.Marshal(routed)
		}
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		response, err := r.call(req.Context(), route, payload)
		if err != nil { // A transport failure does not prove the request was rejected.
			fail(w, 502, "Clother: upstream connection failed; no automatic replay because completion is unknown")
			return
		}
		var reject []byte
		var prefix []byte
		var reader *bufio.Reader
		if response.StatusCode >= 400 {
			reject, _ = io.ReadAll(io.LimitReader(response.Body, 128<<10))
			_ = response.Body.Close()
		} else if strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
			reader = bufio.NewReader(response.Body)
			prefix, reject, err = prelude(reader)
			if err != nil {
				_ = response.Body.Close()
				fail(w, 502, "Clother: incomplete stream before output; no automatic replay")
				return
			}
		}
		if reject != nil {
			_ = response.Body.Close()
			rejectionText := strings.ToLower(string(reject))
			if strings.Contains(rejectionText, "connection failed") || strings.Contains(rejectionText, "timed out") {
				fail(w, 502, "Clother: upstream completion unknown; no automatic replay")
				return
			}
			status := response.StatusCode
			if status == 200 {
				status = streamStatus(reject)
			}
			if status == 429 || status == 402 || status == 401 || status == 503 || status == 502 || status == 500 {
				switch status {
				case 429:
					switchReason = route.Provider + " quota/rate limit (HTTP 429)"
				case 402:
					switchReason = route.Provider + " budget/credit limit (HTTP 402)"
				case 401:
					switchReason = route.Provider + " authentication failed"
				default:
					switchReason = fmt.Sprintf("%s provider error (HTTP %d)", route.Provider, status)
				}
				// Unknown/gateway quota and outages skip the entire provider. Only an
				// explicit upstream-model rate limit allows another model on that gateway.
				text := strings.ToLower(string(reject))
				modelLimit := status == 429 && (strings.Contains(text, "provider_name") || strings.Contains(text, "upstream"))
				if !modelLimit {
					blocked[route.Provider] = true
				}
				chosen = r.next(tried, blocked, required, images > 0)
				if chosen >= 0 {
					continue
				}
			}
			fail(w, status, "Clother: provider rejected request; no eligible fallback (check clother usage). "+safeError(reject, route.Key))
			return
		}
		r.mu.Lock()
		changed := r.active != chosen
		r.active = chosen
		if changed {
			r.changedAt = time.Now()
			r.lastRecovery = time.Now()
			r.reason = switchReason
			if r.reason == "" {
				r.reason = "previous route rejected or unavailable"
			}
		}
		r.mu.Unlock()
		r.publish()
		if changed && r.Changed != nil {
			r.Changed(route)
		}
		if notice == "" && chosen != 0 {
			notice = fmt.Sprintf("Clother active backend: %s (%s).", route.ID(), price(route))
		}
		w.Header().Set("Content-Type", response.Header.Get("Content-Type"))
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Clother-Provider", route.Provider)
		w.Header().Set("X-Clother-Model", route.Model)
		defer response.Body.Close()
		if reader != nil {
			// Once any content/tool block is exposed, this request is NEVER retried.
			forwardStream(w, io.MultiReader(bytes.NewReader(prefix), reader), notice)
		} else {
			data, readErr := io.ReadAll(io.LimitReader(response.Body, 32<<20))
			if readErr != nil {
				fail(w, 502, "incomplete response; no replay")
				return
			}
			if notice != "" {
				var msg map[string]any
				if json.Unmarshal(data, &msg) == nil {
					if content, ok := msg["content"].([]any); ok {
						msg["content"] = append([]any{map[string]any{"type": "text", "text": notice + "\n\n"}}, content...)
						data, _ = json.Marshal(msg)
					}
				}
			}
			w.WriteHeader(response.StatusCode)
			_, _ = w.Write(data)
		}
		return
	}
	fail(w, 429, "Clother: no eligible route or bounded retry budget exhausted. Check usage/reset, credentials and context capacity; paid fallback requires opt-in.")
}
func price(route Route) string {
	if route.Free {
		return "free"
	}
	return "configured API key; may incur cost"
}
func (r *Router) next(tried map[int]bool, blocked map[string]bool, required int, images bool) int {
	best := -1
	bestScore := -1e20
	for i, v := range r.Routes {
		if tried[i] || blocked[v.Provider] || v.Context < required || (images && !v.Images) || !r.allowed(i) {
			continue
		}
		score := -float64(i) * 0.01
		if v.Free {
			score += 10000
			if r.Rank != nil {
				score += r.Rank(v)
			}
		} else {
			score -= float64(i)
		}
		if score > bestScore {
			best = i
			bestScore = score
		}
	}
	return best
}
func rewrite(body map[string]any, route Route, required int, migrated bool) ([]byte, error) {
	copy := map[string]any{}
	for k, v := range body {
		copy[k] = v
	}
	copy["model"] = route.Model
	maxOutput := min(route.Output, route.Context-required+4096)
	if maxOutput <= 0 {
		maxOutput = min(4096, route.Context-required)
	}
	if n, ok := body["max_tokens"].(float64); ok {
		maxOutput = min(maxOutput, int(n))
	}
	if maxOutput < 1 {
		return nil, fmt.Errorf("fallback context budget exceeded")
	}
	copy["max_tokens"] = maxOutput
	if !migrated {
		if thinking, ok := body["thinking"].(map[string]any); ok {
			bounded := map[string]any{}
			for k, v := range thinking {
				bounded[k] = v
			}
			if budget, ok := bounded["budget_tokens"].(float64); ok && int(budget) >= maxOutput {
				if maxOutput/2 < 1024 {
					delete(copy, "thinking")
				} else {
					bounded["budget_tokens"] = maxOutput / 2
					copy["thinking"] = bounded
				}
			}
		}
		return json.Marshal(copy)
	}
	// Thinking signatures and provider-specific output settings cannot migrate.
	delete(copy, "thinking")
	delete(copy, "output_config")
	delete(copy, "context_management")
	if messages, ok := body["messages"].([]any); ok {
		result := make([]any, 0, len(messages))
		for _, item := range messages {
			msg, ok := item.(map[string]any)
			if !ok {
				result = append(result, item)
				continue
			}
			m := map[string]any{}
			for k, v := range msg {
				m[k] = v
			}
			if blocks, ok := msg["content"].([]any); ok {
				clean := []any{}
				for _, block := range blocks {
					b, _ := block.(map[string]any)
					if b["type"] != "thinking" && b["type"] != "redacted_thinking" {
						clean = append(clean, block)
					}
				}
				if len(clean) == 0 {
					clean = append(clean, map[string]any{"type": "text", "text": "[previous reasoning omitted]"})
				}
				m["content"] = clean
			}
			result = append(result, m)
		}
		copy["messages"] = result
	}
	return json.Marshal(copy)
}
func (r *Router) call(ctx context.Context, route Route, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(route.Base, "/")+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Authorization", "Bearer "+route.Key)
	req.Header.Set("x-api-key", route.Key)
	return r.Client.Do(req)
}
func safeError(data []byte, key string) string {
	value := string(data)
	if key != "" {
		value = strings.ReplaceAll(value, key, "[redacted]")
	}
	if len(value) > 1000 {
		value = value[:1000]
	}
	return value
}
func streamStatus(data []byte) int {
	var p map[string]any
	_ = json.Unmarshal(data, &p)
	e, _ := p["error"].(map[string]any)
	if n, ok := e["code"].(float64); ok && n >= 400 && n < 600 {
		return int(n)
	}
	s := strings.ToLower(string(data))
	if strings.Contains(s, "rate_limit") || strings.Contains(s, "429") {
		return 429
	}
	if strings.Contains(s, "overloaded") {
		return 503
	}
	return 400
}

// Buffer only the prelude. Never retry even an empty tool/content block once
// its start arrived: the model may already have initiated a tool operation.
func prelude(reader *bufio.Reader) ([]byte, []byte, error) {
	var prefix bytes.Buffer
	for prefix.Len() < 256<<10 {
		line, err := reader.ReadBytes('\n')
		prefix.Write(line)
		if bytes.HasPrefix(line, []byte("data:")) {
			data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
			var event map[string]any
			if json.Unmarshal(data, &event) == nil {
				switch event["type"] {
				case "error":
					return nil, data, nil
				case "content_block_start", "content_block_delta", "message_delta", "message_stop":
					return prefix.Bytes(), nil, nil
				}
			}
		}
		if err != nil {
			return nil, nil, err
		}
	}
	return nil, nil, fmt.Errorf("stream prelude too large")
}
func forwardStream(w http.ResponseWriter, reader io.Reader, notice string) {
	scan := bufio.NewScanner(reader)
	scan.Buffer(make([]byte, 4096), 2<<20)
	injected := false
	for scan.Scan() {
		line := scan.Text()
		if strings.HasPrefix(line, "data:") && notice != "" {
			var event map[string]any
			if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &event) == nil {
				if index, ok := event["index"].(float64); ok {
					event["index"] = index + 1
				}
				encoded, _ := json.Marshal(event)
				line = "data: " + string(encoded)
				if event["type"] == "message_start" && !injected {
					fmt.Fprintln(w, line)
					fmt.Fprintln(w)
					for _, e := range []map[string]any{{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}}, {"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": notice + "\n\n"}}, {"type": "content_block_stop", "index": 0}} {
						data, _ := json.Marshal(e)
						fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e["type"], data)
					}
					injected = true
					continue
				}
			}
		}
		fmt.Fprintln(w, line)
		if line == "" {
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}
	// A malformed/truncated stream remains an error at the client. No replay.
}
