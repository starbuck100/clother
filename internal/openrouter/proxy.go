package openrouter

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

// NeedsSchemaCompatibility is deliberately scoped to the model whose provider
// rejects minLength while compiling tool grammars. Do not relax other models.
func NeedsSchemaCompatibility(model string) bool {
	return model == "qwen/qwen3.8-27b:free" || model == "qwen/qwen3.8-27b"
}

// StartSchemaProxy keeps the upstream credential out of the loopback client.
// Only this session's random token can use the proxy; it is never an open relay.
// The proxy lives only as long as the Claude process and never records bodies.
func StartSchemaProxy(ctx context.Context, baseURL, upstreamKey string, transports ...http.RoundTripper) (string, string, func(), error) {
	upstream, err := url.Parse(baseURL)
	if err != nil || upstream.Host == "" || (upstream.Scheme != "https" && upstream.Scheme != "http") || upstream.User != nil {
		return "", "", nil, fmt.Errorf("invalid OpenRouter endpoint")
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", "", nil, err
	}
	token := hex.EncodeToString(secret)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return "", "", nil, fmt.Errorf("start OpenRouter schema compatibility: %w", err)
	}
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	if len(transports) > 0 {
		proxy.Transport = transports[0]
	}
	director := proxy.Director
	proxy.Director = func(r *http.Request) {
		director(r)
		r.Host = upstream.Host
		r.Header.Set("Authorization", "Bearer "+upstreamKey)
		r.Header.Del("X-Api-Key")
		r.Header.Del("Cookie")
	}
	proxy.FlushInterval = -1 // Preserve streamed tool calls and token deltas.
	proxy.ErrorLog = log.New(io.Discard, "", 0)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, "OpenRouter upstream connection failed", http.StatusBadGateway)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost || (r.URL.Path != "/v1/messages" && r.URL.Path != "/v1/messages/count_tokens") {
			http.NotFound(w, r)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<20))
		_ = r.Body.Close()
		if err != nil {
			http.Error(w, "request body too large or unreadable", http.StatusRequestEntityTooLarge)
			return
		}
		body, err = CompatibleToolSchemas(body)
		if err != nil {
			http.Error(w, "invalid messages JSON", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		r.Header.Del("Content-Length")
		proxy.ServeHTTP(w, r)
	})
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	stop := context.AfterFunc(ctx, func() { _ = server.Close() })
	go func() { _ = server.Serve(listener) }()
	cleanup := func() {
		stop()
		_ = server.Close()
	}
	return "http://" + listener.Addr().String(), token, cleanup, nil
}

// CompatibleToolSchemas changes only outbound tool input schemas. Claude/MCP
// still owns the original schema and validates tool arguments normally. Keep
// the unsupported constraint as a textual instruction instead of dropping it
// without explanation. Prompts, tool results and schema property names stay
// untouched, including a property literally named "minLength".
func CompatibleToolSchemas(body []byte) ([]byte, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	var model string
	_ = json.Unmarshal(payload["model"], &model)
	if !NeedsSchemaCompatibility(model) {
		return body, nil
	}
	var tools []map[string]json.RawMessage
	if raw := payload["tools"]; len(raw) == 0 {
		return body, nil
	} else if err := json.Unmarshal(raw, &tools); err != nil {
		return nil, err
	}
	changed := false
	for _, tool := range tools {
		var schema map[string]any
		decoder := json.NewDecoder(bytes.NewReader(tool["input_schema"]))
		decoder.UseNumber()
		if err := decoder.Decode(&schema); err != nil {
			continue // Server-defined tools need not have an input schema.
		}
		if relaxMinLength(schema) {
			tool["input_schema"], _ = json.Marshal(schema)
			changed = true
		}
	}
	if !changed {
		return body, nil
	}
	payload["tools"], _ = json.Marshal(tools)
	return json.Marshal(payload)
}

func relaxMinLength(schema map[string]any) bool {
	changed := false
	if value, ok := schema["minLength"].(json.Number); ok {
		description, _ := schema["description"].(string)
		schema["description"] = strings.TrimSpace(description + " Minimum string length: " + value.String() + " characters.")
		delete(schema, "minLength")
		changed = true
	}
	// Walk schema positions only, never examples/defaults/enum or property names.
	for _, key := range []string{"properties", "patternProperties", "$defs", "definitions", "dependentSchemas"} {
		if children, ok := schema[key].(map[string]any); ok {
			for _, child := range children {
				if node, ok := child.(map[string]any); ok {
					changed = relaxMinLength(node) || changed
				}
			}
		}
	}
	for _, key := range []string{"items", "additionalProperties", "additionalItems", "contains", "not", "if", "then", "else", "propertyNames", "unevaluatedProperties", "unevaluatedItems", "allOf", "anyOf", "oneOf", "prefixItems"} {
		switch child := schema[key].(type) {
		case map[string]any:
			changed = relaxMinLength(child) || changed
		case []any:
			for _, entry := range child {
				if node, ok := entry.(map[string]any); ok {
					changed = relaxMinLength(node) || changed
				}
			}
		}
	}
	return changed
}
