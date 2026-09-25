package kilo

import (
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
	"time"

	"github.com/jolehuit/clother/internal/openrouter"
)

func Load(ctx context.Context, base, cache string, refresh bool) (openrouter.Catalog, error) {
	return openrouter.LoadAt(ctx, base, "/models", cache, "kilo-models.json", refresh)
}

// Start keeps the gateway credential in this process. Claude receives only an
// ephemeral token accepted by this loopback server.
func Start(ctx context.Context, base, key string, catalog openrouter.Catalog) (string, string, func(), error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", "", nil, err
	}
	token := hex.EncodeToString(secret)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", "", nil, err
	}
	client := &http.Client{Timeout: 10 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	server := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		supplied := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if supplied == "" {
			supplied = r.Header.Get("x-api-key")
		}
		if subtle.ConstantTimeCompare([]byte(supplied), []byte(token)) != 1 {
			apiError(w, 401, "invalid session token")
			return
		}
		if r.Method != http.MethodPost || (r.URL.Path != "/v1/messages" && r.URL.Path != "/v1/messages/count_tokens") {
			apiError(w, 404, "unsupported Kilo bridge endpoint")
			return
		}
		if r.URL.Path == "/v1/messages/count_tokens" {
			apiError(w, 501, "Kilo does not expose an exact token counter")
			return
		}
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<20))
		if err != nil {
			apiError(w, 413, "request too large")
			return
		}
		var input Request
		if err = json.Unmarshal(data, &input); err != nil {
			apiError(w, 400, "invalid Messages request")
			return
		}
		model, err := catalog.Find(input.Model)
		if err == nil {
			err = model.Validate()
		}
		if err != nil {
			apiError(w, 400, err.Error())
			return
		}
		if key == "" && !model.Free() {
			apiError(w, 401, "This model requires a Kilo API key; run clother config kilo or select a free model")
			return
		}
		if openrouter.NeedsSchemaCompatibility(input.Model) {
			data, err = openrouter.CompatibleToolSchemas(data)
			if err == nil {
				err = json.Unmarshal(data, &input)
			}
			if err != nil {
				apiError(w, 400, err.Error())
				return
			}
		}
		translated, err := translateRequest(input, model)
		if err != nil {
			apiError(w, 400, err.Error())
			return
		}
		body, err := json.Marshal(translated)
		if err != nil {
			apiError(w, 400, err.Error())
			return
		}
		req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, strings.TrimRight(base, "/")+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			apiError(w, 502, "invalid Kilo endpoint")
			return
		}
		req.Header.Set("Content-Type", "application/json")
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		resp, err := client.Do(req)
		if err != nil {
			apiError(w, 502, "Kilo request failed or timed out")
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
			message := fmt.Sprintf("Kilo HTTP %d: %s", resp.StatusCode, string(body))
			if key != "" {
				message = strings.ReplaceAll(message, key, "[redacted]")
			}
			if retry := resp.Header.Get("Retry-After"); retry != "" {
				w.Header().Set("Retry-After", retry)
			}
			apiError(w, resp.StatusCode, message)
			return
		}
		if input.Stream {
			streamResponse(w, resp.Body, input.Model)
			return
		}
		var result completion
		if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&result); err != nil {
			apiError(w, 502, "invalid Kilo response")
			return
		}
		message, err := result.message(input.Model)
		if err != nil {
			apiError(w, 502, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(message)
	})}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = server.Close()
		case <-done:
		}
	}()
	go func() { _ = server.Serve(listener) }()
	return "http://" + listener.Addr().String(), token, func() { close(done); _ = server.Close(); client.CloseIdleConnections() }, nil
}

func apiError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "error", "error": map[string]any{"type": "api_error", "message": message}})
}
