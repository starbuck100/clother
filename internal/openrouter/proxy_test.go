package openrouter

import (
	"context"
	"encoding/json"
	"github.com/jolehuit/clother/internal/usage"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const artifactRequest = `{"model":"qwen/qwen3.8-27b:free","max_tokens":512,"stream":true,"messages":[{"role":"user","content":"literal minLength must remain"}],"tools":[{"name":"artifact","input_schema":{"type":"object","properties":{"favicon":{"type":"string","minLength":1,"description":"Icon"},"minLength":{"type":"number"},"nested":{"type":"array","items":{"anyOf":[{"type":"string","minLength":2}]}}},"required":["favicon"],"additionalProperties":false,"examples":[{"minLength":9}]}}]}`

func TestUsageProxyPreservesOtherModelSchemas(t *testing.T) {
	body := strings.ReplaceAll(artifactRequest, "qwen/qwen3.8-27b:free", "vendor/other:free")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ := io.ReadAll(r.Body)
		if string(got) != body || r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("tracking changed request or credential")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":15,\"output_tokens\":1}}}\n\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":4}}\n\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer upstream.Close()
	store := usage.NewStore(t.TempDir())
	tracker := &usage.Transport{Store: store, Provider: "openrouter", AccountScope: "scope"}
	base, token, cleanup, err := StartSchemaProxy(context.Background(), upstream.URL, "secret", tracker)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	req, _ := http.NewRequest("POST", base+"/v1/messages", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	// ReverseProxy may finish closing its upstream immediately after the client's
	// EOF. Bound the wait instead of assuming goroutine scheduling order.
	deadline := time.Now().Add(time.Second)
	for {
		events, err := store.Read(time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if len(events) == 1 {
			e := events[0]
			if e.Input == nil || *e.Input != 15 || e.Output == nil || *e.Output != 4 {
				t.Fatalf("bad usage: %+v", e)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("usage not persisted")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestCompatibleToolSchemas(t *testing.T) {
	patched, err := CompatibleToolSchemas([]byte(artifactRequest))
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		MaxTokens int `json:"max_tokens"`
		Tools     []struct {
			Schema map[string]any `json:"input_schema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(patched, &payload); err != nil {
		t.Fatal(err)
	}
	schema := payload.Tools[0].Schema
	properties := schema["properties"].(map[string]any)
	icon := properties["favicon"].(map[string]any)
	if _, exists := icon["minLength"]; exists || icon["description"] != "Icon Minimum string length: 1 characters." {
		t.Fatalf("favicon schema not made compatible: %v", icon)
	}
	if properties["minLength"] == nil || schema["additionalProperties"] != false || payload.MaxTokens != 512 {
		t.Fatal("unrelated schema or request fields modified")
	}
	for _, fragment := range []string{`"minLength":9`, `"required":["favicon"]`, "literal minLength must remain", "Minimum string length: 2 characters."} {
		if !strings.Contains(string(patched), fragment) {
			t.Errorf("missing preserved or nested value: %s", fragment)
		}
	}
	other := strings.ReplaceAll(artifactRequest, "qwen/qwen3.8-27b:free", "anthropic/claude-sonnet-5")
	unchanged, err := CompatibleToolSchemas([]byte(other))
	if err != nil || string(unchanged) != other {
		t.Fatal("unaffected model changed")
	}
	if _, err := CompatibleToolSchemas([]byte("not JSON")); err == nil {
		t.Fatal("invalid JSON accepted")
	}
}

func TestSchemaProxyStreamsWithSessionAuthAndCleanup(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/api/v1/messages" || r.Header.Get("Authorization") != "Bearer upstream-secret" || r.Header.Get("X-Api-Key") != "" {
			t.Error("upstream path or authentication mismatch")
		}
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), `"minLength":1`) {
			http.Error(w, `failed to translate request: tool "artifact" parameter "favicon": unsupported schema keyword "minLength"`, 400)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\"}\n\n")
		w.(http.Flusher).Flush()
		io.WriteString(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer upstream.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	base, token, cleanup, err := StartSchemaProxy(ctx, upstream.URL+"/api", "upstream-secret")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	client := &http.Client{Timeout: 5 * time.Second}
	for _, validAuth := range []bool{false, true} {
		req, _ := http.NewRequest(http.MethodPost, base+"/v1/messages", strings.NewReader(artifactRequest))
		if validAuth {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		req.Header.Set("X-Api-Key", "must-not-forward")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if !validAuth && resp.StatusCode != 401 {
			t.Fatal("unauthenticated local request accepted")
		}
		if validAuth && (resp.StatusCode != 200 || !strings.Contains(string(body), "message_stop")) {
			t.Fatalf("stream failed: %d %s", resp.StatusCode, body)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("unexpected upstream calls: %d", calls.Load())
	}
	cleanup()
	if resp, err := client.Post(base+"/v1/messages", "application/json", strings.NewReader(artifactRequest)); err == nil {
		resp.Body.Close()
		t.Fatal("proxy survived cleanup")
	}
}
