package fallback

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func route(provider, model, base string, free bool) Route {
	return Route{Provider: provider, Model: model, Base: base, Key: provider + "-KEY", Free: free, Context: 100000, Output: 128}
}
func request(t *testing.T, base, key string, stream bool) (int, string) {
	t.Helper()
	body := fmt.Sprintf(`{"model":"original","max_tokens":9999,"stream":%t,"messages":[{"role":"user","content":"keep my history"}]}`, stream)
	req, _ := http.NewRequest("POST", base+"/v1/messages", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+key)
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(data)
}
func start(t *testing.T, r *Router) (string, string) {
	t.Helper()
	base, key, close, e := r.Start()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(close)
	return base, key
}

func TestProviderExhaustionCrossesGatewaysThenPaidAndPersists(t *testing.T) {
	var hits [4]atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(req.Body).Decode(&body)
		model := body["model"].(string)
		if body["max_tokens"].(float64) != 128 {
			t.Error("output ceiling not applied")
		}
		if !strings.Contains(fmt.Sprint(body["messages"]), "keep my history") {
			t.Error("history lost")
		}
		switch model {
		case "a":
			hits[0].Add(1)
			w.WriteHeader(429)
			fmt.Fprint(w, `{"error":{"message":"Rate limit exceeded for free models"}}`)
		case "a2":
			hits[1].Add(1)
			t.Error("rotated model on exhausted provider")
		case "b":
			hits[2].Add(1)
			w.WriteHeader(429)
			fmt.Fprint(w, `{"error":{"message":"free-models-per-day"}}`)
		case "deepseek-flash":
			hits[3].Add(1)
			if req.Header.Get("Authorization") != "Bearer deepseek-KEY" {
				t.Error("wrong credential")
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"type":"message","content":[{"type":"text","text":"OK"}]}`)
		}
	}))
	defer server.Close()
	r := &Router{Routes: []Route{route("kilo", "a", server.URL, true), route("kilo", "a2", server.URL, true), route("openrouter", "b", server.URL, true), route("deepseek", "deepseek-flash", server.URL, false)}}
	base, key := start(t, r)
	code, text := request(t, base, key, false)
	if code != 200 || !strings.Contains(text, "may incur cost") || !strings.Contains(text, "OK") {
		t.Fatalf("%d %s", code, text)
	}
	code, _ = request(t, base, key, false)
	if code != 200 || hits[0].Load() != 1 || hits[1].Load() != 0 || hits[2].Load() != 1 || hits[3].Load() != 2 {
		t.Fatal("active route not retained")
	}
	code, _ = request(t, base, "bad", false)
	if code != 401 {
		t.Fatal("unauthenticated relay")
	}
}

const startEvent = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"x\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"b\",\"content\":[],\"usage\":{\"input_tokens\":2,\"output_tokens\":0}}}\n\n"
const toolEvent = "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"call1\",\"name\":\"artifact\",\"input\":{}}}\n\n"
const errorEvent = "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"rate_limit_error\",\"message\":\"429\"}}\n\n"

func TestStreamRetriesOnlyBeforeContentAndKeepsToolIndices(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(fmt.Sprint(partial), func(t *testing.T) {
			var replacement atomic.Int32
			first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, startEvent)
				if partial {
					fmt.Fprint(w, toolEvent)
				}
				fmt.Fprint(w, errorEvent)
			}))
			defer first.Close()
			next := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				replacement.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, startEvent+toolEvent+"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
			}))
			defer next.Close()
			r := &Router{Routes: []Route{route("kilo", "a", first.URL, true), route("openrouter", "b", next.URL, true)}}
			base, key := start(t, r)
			_, text := request(t, base, key, true)
			if partial {
				if replacement.Load() != 0 || !strings.Contains(text, "rate_limit_error") {
					t.Fatal("replayed after tool start")
				}
			} else {
				if replacement.Load() != 1 || !strings.Contains(text, `"index":1`) || !strings.Contains(text, "Clother: switched") || strings.Count(text, `"id":"call1"`) != 1 {
					t.Fatal(text)
				}
			}
		})
	}
}
func TestLimitsRecheckedAndOversizeNeverTruncated(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); fmt.Fprint(w, `{"content":[]}`) }))
	defer server.Close()
	routes := []Route{route("kilo", "a", server.URL, true), route("openrouter", "b", server.URL, true)}
	routes[1].Context = 10
	checks := 0
	r := &Router{Routes: routes, Check: func(ctx context.Context, r Route) bool { checks++; return false }}
	base, key := start(t, r)
	code, _ := request(t, base, key, false)
	if code != 429 || calls.Load() != 0 || checks != 1 {
		t.Fatal("invalid route attempted")
	}
}
func TestWarmPlannerCannotIntroduceUnknownOrPaidRoute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"content":[{"type":"text","text":"{\"order\":[\"deepseek/paid\",\"attacker/injected\",\"openrouter/b\"]}"}]}`)
	}))
	defer server.Close()
	r := &Router{Routes: []Route{route("kilo", "a", server.URL, true), route("kilo", "c", server.URL, true), route("openrouter", "b", server.URL, true), route("deepseek", "paid", server.URL, false)}, Client: http.DefaultClient}
	if e := r.Warm(context.Background(), "choose task-fit models", "Coding"); e != nil {
		t.Fatal(e)
	}
	if r.Routes[1].ID() != "openrouter/b" || r.Routes[3].ID() != "deepseek/paid" {
		t.Fatal(r.Routes)
	}
}
