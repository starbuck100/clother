package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestObservedUsageDoesNotChangeStreamOrStoreContent(t *testing.T) {
	for _, tc := range []struct {
		name, body    string
		input, output int
	}{
		{"anthropic", "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":12,\"output_tokens\":1,\"cache_read_input_tokens\":100}}}\n\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"PRIVATE_OUTPUT\"}}\n\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":7}}\n\ndata: {\"type\":\"message_stop\"}\n\n", 12, 7},
		{"openai", "data: {\"choices\":[{\"delta\":{\"content\":\"PRIVATE_OUTPUT\"},\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":15,\"completion_tokens\":8,\"cost\":0,\"prompt_tokens_details\":{\"cached_tokens\":10}}}\n\ndata: [DONE]\n\n", 15, 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data, _ := io.ReadAll(r.Body)
				if !strings.Contains(string(data), "PRIVATE_PROMPT") {
					t.Error("request changed")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			store := NewStore(t.TempDir())
			client := &http.Client{Transport: &Transport{Store: store, Provider: "kilo", AccountScope: "scope", IsFree: func(string) bool { return true }}}
			req, _ := http.NewRequest("POST", server.URL+"/chat/completions", strings.NewReader(`{"model":"vendor/free","messages":[{"role":"user","content":"PRIVATE_PROMPT"}]}`))
			req.Header.Set("Authorization", "Bearer PRIVATE_KEY")
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			_ = resp.Body.Close()
			if string(data) != tc.body {
				t.Fatal("response changed")
			}
			events, err := store.Read(time.Now())
			if err != nil || len(events) != 1 {
				t.Fatalf("events=%v err=%v", events, err)
			}
			e := events[0]
			if e.Input == nil || *e.Input != tc.input || e.Output == nil || *e.Output != tc.output || e.Outcome != "ok" {
				t.Fatalf("incorrect or duplicated usage: %+v", e)
			}
			_ = filepath.Walk(store.Dir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if !info.IsDir() {
					raw, _ := os.ReadFile(path)
					if strings.Contains(string(raw), "PRIVATE_") {
						t.Error("content or credential persisted")
					}
				}
				return nil
			})
		})
	}
}

func TestLimitClassificationAndReset(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		provider          string
		status            int
		body, scope, kind string
	}{
		{"openrouter", 429, `{"error":{"message":"Rate limit exceeded: free-models-per-day","metadata":{"headers":{"X-RateLimit-Limit":"50","X-RateLimit-Remaining":"0","X-RateLimit-Reset":"1790380860000"}}}}`, "provider", "free_daily"},
		{"openrouter", 429, `{"error":{"metadata":{"raw":"temporarily rate-limited upstream","provider_name":"Provider"}}}`, "model", "upstream_rate"},
		{"openrouter", 429, `{"error":{"message":"Rate limit exceeded"}}`, "unknown", "rate_limit"},
		{"kilo", 429, `{"error":{"message":"Rate limit exceeded for free models. Please try again later."}}`, "provider", "free_hour"},
		{"kilo", 200, `{"error":{"code":429,"message":"upstream rate limit"}}`, "model", "upstream_rate"},
		{"openrouter", 402, `{"error":{"metadata":{"limit_source":"openrouter_in_flight_budget"}}}`, "provider", "in_flight_budget"},
		{"kilo", 400, `{"error":{"message":"max_tokens exceeds context"}}`, "model", "invalid_request"},
	}
	for _, tc := range tests {
		var payload map[string]any
		_ = json.Unmarshal([]byte(tc.body), &payload)
		limit := classify(tc.provider, tc.status, http.Header{}, payload, now)
		if limit == nil || limit.Scope != tc.scope || limit.Kind != tc.kind {
			t.Fatalf("%s: %+v", tc.body, limit)
		}
		if tc.kind == "free_hour" && limit.ResetAt != nil {
			t.Fatal("invented Kilo reset")
		}
	}
	h := http.Header{"Retry-After": []string{"60"}}
	limit := classify("kilo", 429, h, nil, now)
	if limit.ResetAt == nil || !limit.ResetAt.Equal(now.Add(time.Minute)) {
		t.Fatal("Retry-After not honored")
	}
}

func TestProviderQuotaMissingIsNotZero(t *testing.T) {
	for _, tc := range []struct {
		body      string
		remaining int
		known     bool
	}{
		{`{"data":{"usage":0}}`, 0, false},
		{`{"data":{"free_model_daily_requests":null}}`, 0, false},
		{`{"data":{"free_model_daily_requests":{"used":12,"limit":50,"remaining":38},"limit_remaining":0}}`, 38, true},
		{`{"data":{"free_model_daily_requests":{"used":50,"limit":50,"remaining":0}}}`, 0, true},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/key" || r.Header.Get("Authorization") != "Bearer test-key" {
				t.Error("bad quota request")
			}
			fmt.Fprint(w, tc.body)
		}))
		account, err := FetchOpenRouter(context.Background(), server.URL, "test-key")
		server.Close()
		if err != nil {
			t.Fatal(err)
		}
		if (account.FreeDaily != nil) != tc.known {
			t.Fatal("missing quota treated as zero")
		}
		if tc.known && account.FreeDaily.Remaining != tc.remaining {
			t.Fatal("wrong remaining")
		}
	}
}

func TestConcurrentStoreAndScopeIsolation(t *testing.T) {
	store := NewStore(t.TempDir())
	now := time.Now().UTC()
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := store.Record(Event{At: now, Provider: "kilo", Scope: "s", Model: "free", Outcome: "ok"}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	events, err := store.Read(time.Now())
	if err != nil || len(events) != 40 {
		t.Fatalf("lost requests: %d %v", len(events), err)
	}
	if Scope("kilo", "https://gateway", "a") != Scope("kilo", "https://gateway", "b") {
		t.Fatal("Kilo IP quota must span keys")
	}
	if Scope("openrouter", "https://gateway", "a") == Scope("openrouter", "https://gateway", "b") {
		t.Fatal("OpenRouter credential scopes collided")
	}
	reset := now.Add(time.Hour)
	events = []Event{{At: now, Model: "a", Limit: &Limit{Kind: "free_hour", Scope: "provider", ResetAt: &reset}}, {At: now.Add(time.Second), Model: "b", Outcome: "ok"}}
	if len(ActiveBlocks(events, now.Add(2*time.Second))) != 0 {
		t.Fatal("later success did not clear provider block")
	}
	events = events[:1]
	if len(ActiveBlocks(events, reset.Add(time.Second))) != 0 {
		t.Fatal("expired block still active")
	}
}

func TestFailedAndIncompleteRequests(t *testing.T) {
	for _, tc := range []struct {
		status                int
		stream, body, outcome string
	}{
		{429, "application/json", `{"error":{"message":"Rate limit exceeded for free models"}}`, "http_error"},
		{200, "text/event-stream", "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n", "incomplete"},
		{200, "text/event-stream", "data: {\"error\":{\"code\":429,\"message\":\"upstream limit\"}}\n\ndata: [DONE]\n\n", "api_error"},
	} {
		store := NewStore(t.TempDir())
		o := observer{event: Event{At: time.Now().UTC(), Provider: "kilo", Status: tc.status, Outcome: "incomplete"}, transport: &Transport{Store: store}, stream: strings.Contains(tc.stream, "event-stream"), headers: http.Header{}}
		for _, b := range []byte(tc.body) {
			o.feed([]byte{b})
		}
		o.finish()
		events, err := store.Read(time.Now())
		if err != nil || len(events) != 1 || events[0].Outcome != tc.outcome {
			t.Fatalf("%+v %v", events, err)
		}
	}
}
