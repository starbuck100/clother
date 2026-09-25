package kilo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jolehuit/clother/internal/openrouter"
)

func testCatalog(t *testing.T) openrouter.Catalog {
	t.Helper()
	var c openrouter.Catalog
	err := json.Unmarshal([]byte(`{"data":[{"id":"stealth/test","context_length":16000,"top_provider":{"max_completion_tokens":2048},"pricing":{"prompt":"0","completion":"0","discount":0},"supported_parameters":["tools","reasoning_effort"],"architecture":{"input_modalities":["text"],"output_modalities":["text"]}}]}`), &c)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestBridgeToolRoundTrip(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			calls := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "" {
					t.Error("incorrect anonymous gateway request")
				}
				var req map[string]any
				_ = json.NewDecoder(r.Body).Decode(&req)
				if req["max_tokens"] != float64(2048) {
					t.Errorf("unbounded output: %v", req["max_tokens"])
				}
				messages := req["messages"].([]any)
				if calls == 2 {
					last := messages[len(messages)-1].(map[string]any)
					if last["role"] != "tool" || last["tool_call_id"] != "call1" || last["content"] != "created" {
						t.Errorf("tool result lost: %v", last)
					}
				}
				if stream {
					if calls == 1 {
						fmt.Fprint(w, "data: {\"id\":\"msg1\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call1\",\"function\":{\"name\":\"arti\",\"arguments\":\"{\\\"favicon\\\":\"}}]}}]}\n\n")
						fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"fact\",\"arguments\":\"\\\"X\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n")
					} else {
						fmt.Fprint(w, "data: {\"id\":\"msg2\",\"choices\":[{\"delta\":{\"content\":\"OK\"},\"finish_reason\":\"stop\"}]}\n\n")
					}
					fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":9}}\n\ndata: [DONE]\n\n")
				} else if calls == 1 {
					fmt.Fprint(w, `{"id":"msg1","choices":[{"message":{"tool_calls":[{"id":"call1","function":{"name":"artifact","arguments":"{\"favicon\":\"X\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":12,"completion_tokens":9}}`)
				} else {
					fmt.Fprint(w, `{"id":"msg2","choices":[{"message":{"content":"OK"},"finish_reason":"stop"}]}`)
				}
			}))
			defer upstream.Close()
			endpoint, token, cleanup, err := Start(context.Background(), upstream.URL, "", testCatalog(t))
			if err != nil {
				t.Fatal(err)
			}
			defer cleanup()
			for turn := 0; turn < 2; turn++ {
				messages := `[{"role":"user","content":"create artifact"}]`
				if turn == 1 {
					messages = `[{"role":"user","content":"create artifact"},{"role":"assistant","content":[{"type":"tool_use","id":"call1","name":"artifact","input":{"favicon":"X"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call1","content":[{"type":"text","text":"created"}]}]}]`
				}
				body := fmt.Sprintf(`{"model":"stealth/test","stream":%t,"max_tokens":999999,"messages":%s,"tools":[{"name":"artifact","input_schema":{"type":"object","properties":{"favicon":{"type":"string","minLength":1}}}}]}`, stream, messages)
				req, _ := http.NewRequest("POST", endpoint+"/v1/messages", strings.NewReader(body))
				req.Header.Set("Authorization", "Bearer "+token)
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				data, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				if resp.StatusCode != 200 {
					t.Fatalf("%d %s", resp.StatusCode, data)
				}
				expected := "artifact"
				if turn == 1 {
					expected = "OK"
				}
				if !strings.Contains(string(data), expected) {
					t.Fatalf("missing %s: %s", expected, data)
				}
				if stream && (!strings.Contains(string(data), "message_stop") || !strings.Contains(string(data), `"output_tokens":9`)) {
					t.Fatalf("incomplete SSE: %s", data)
				}
			}
		})
	}
}

func TestBridgeGuardsAndGatewayErrors(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer private-key" {
			t.Error("wrong gateway key")
		}
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(429)
		fmt.Fprint(w, `{"error":"quota private-key"}`)
	}))
	defer upstream.Close()
	for _, key := range []string{"", "private-key"} {
		cat := testCatalog(t)
		paid := cat.Models[0]
		paid.ID = "paid/model"
		paid.Pricing = map[string]json.RawMessage{"prompt": json.RawMessage(`"1"`), "completion": json.RawMessage(`"1"`)}
		cat.Models = append(cat.Models, paid)
		endpoint, token, cleanup, err := Start(context.Background(), upstream.URL, key, cat)
		if err != nil {
			t.Fatal(err)
		}
		cases := []struct {
			path, auth, model string
			status            int
		}{{"/v1/messages", "wrong", "stealth/test", 401}, {"/other", token, "stealth/test", 404}, {"/v1/messages/count_tokens", token, "stealth/test", 501}, {"/v1/messages", token, "unknown", 400}}
		if key == "" {
			cases = append(cases, struct {
				path, auth, model string
				status            int
			}{"/v1/messages", token, "paid/model", 401})
		} else {
			cases = append(cases, struct {
				path, auth, model string
				status            int
			}{"/v1/messages", token, "stealth/test", 429})
		}
		for _, tc := range cases {
			req, _ := http.NewRequest("POST", endpoint+tc.path, strings.NewReader(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hi"}]}`, tc.model)))
			req.Header.Set("x-api-key", tc.auth)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			data, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != tc.status || strings.Contains(string(data), "private-key") {
				t.Fatalf("%s: status %d body %s", tc.path, resp.StatusCode, data)
			}
			if tc.status == 429 && resp.Header.Get("Retry-After") != "7" {
				t.Error("Retry-After lost")
			}
		}
		cleanup()
	}
	if calls != 1 {
		t.Fatalf("unauthorized traffic reached upstream: %d", calls)
	}
}

func TestIncompleteStreamsNeverCompleteTools(t *testing.T) {
	for _, body := range []string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n",
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c\",\"function\":{\"name\":\"exec\",\"arguments\":\"{\"}}]},\"finish_reason\":\"length\"}]}\n\ndata: [DONE]\n\n",
		"data: {\"error\":{\"message\":\"provider failed\"}}\n\n",
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n",
	} {
		out := httptest.NewRecorder()
		streamResponse(out, strings.NewReader(body), "stealth/test")
		if !strings.Contains(out.Body.String(), "event: error") || strings.Contains(out.Body.String(), "message_stop") || strings.Contains(out.Body.String(), "content_block_start\ndata: {\"content_block\":{\"id\"") {
			t.Fatalf("false success: %s", out.Body.String())
		}
	}
}

func TestRequestRejectsUnsupportedContent(t *testing.T) {
	for _, kind := range []string{"document", "server_tool_use"} {
		var req Request
		_ = json.Unmarshal([]byte(fmt.Sprintf(`{"model":"stealth/test","messages":[{"role":"user","content":[{"type":%q}]}]}`, kind)), &req)
		if _, err := translateRequest(req, testCatalog(t).Models[0]); err == nil {
			t.Fatal("unsupported content silently lost")
		}
	}
}

func TestAdditionalSystemMessages(t *testing.T) {
	var req Request
	if err := json.Unmarshal([]byte(`{"model":"stealth/test","system":"first","messages":[{"role":"system","content":[{"type":"text","text":"additional instructions"}]},{"role":"user","content":"hi"}]}`), &req); err != nil {
		t.Fatal(err)
	}
	out, err := translateRequest(req, testCatalog(t).Models[0])
	if err != nil {
		t.Fatal(err)
	}
	messages := out["messages"].([]map[string]any)
	if len(messages) != 3 || messages[1]["role"] != "system" || messages[1]["content"] != "additional instructions" {
		t.Fatalf("system message lost: %v", messages)
	}
}
