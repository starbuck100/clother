package usage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Transport struct {
	Base                   http.RoundTripper
	Store                  Store
	Provider, AccountScope string
	IsFree                 func(string) bool
	Warn                   func(string)
	warnOnce               sync.Once
	quotaOnce              sync.Once
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	if !strings.HasSuffix(req.URL.Path, "/messages") && !strings.HasSuffix(req.URL.Path, "/chat/completions") {
		return base.RoundTrip(req)
	}
	event := Event{At: time.Now().UTC(), Provider: t.Provider, Scope: t.AccountScope, Outcome: "incomplete"}
	if req.Body != nil {
		body, err := io.ReadAll(io.LimitReader(req.Body, 32<<20+1))
		_ = req.Body.Close()
		if err != nil {
			return nil, err
		}
		if len(body) > 32<<20 {
			return nil, fmt.Errorf("inference request exceeds 32 MiB")
		}
		req.Body = io.NopCloser(bytes.NewReader(body))
		var payload map[string]any
		if json.Unmarshal(body, &payload) == nil {
			event.Model, _ = payload["model"].(string)
			event.Images = containsImage(payload["messages"])
		}
	}
	if t.IsFree != nil {
		event.Free = t.IsFree(event.Model)
	}
	observation := &observer{event: event, transport: t, headers: http.Header{}}
	resp, err := base.RoundTrip(req)
	if err != nil {
		observation.event.Outcome = "connection_error"
		observation.finish()
		return nil, err
	}
	observation.event.Status = resp.StatusCode
	observation.headers = resp.Header.Clone()
	observation.stream = strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")
	resp.Body = &observedBody{ReadCloser: resp.Body, observe: observation}
	return resp, nil
}

func containsImage(value any) bool {
	switch v := value.(type) {
	case map[string]any:
		if v["type"] == "image" || v["type"] == "image_url" {
			return true
		}
		for _, entry := range v {
			if containsImage(entry) {
				return true
			}
		}
	case []any:
		for _, entry := range v {
			if containsImage(entry) {
				return true
			}
		}
	}
	return false
}

type observedBody struct {
	io.ReadCloser
	observe *observer
}

func (r *observedBody) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.observe.feed(p[:n])
	if err == io.EOF {
		r.observe.eof = true
		r.observe.finish()
	}
	return n, err
}
func (r *observedBody) Close() error { err := r.ReadCloser.Close(); r.observe.finish(); return err }

type observer struct {
	event                                   Event
	transport                               *Transport
	headers                                 http.Header
	stream, eof, complete, failed, overflow bool
	buffer                                  []byte
	once                                    sync.Once
}

func (o *observer) feed(data []byte) {
	if o.overflow {
		return
	}
	o.buffer = append(o.buffer, data...)
	if o.stream {
		for {
			idx := bytes.IndexByte(o.buffer, '\n')
			if idx < 0 {
				break
			}
			line := bytes.TrimSpace(o.buffer[:idx])
			o.buffer = o.buffer[idx+1:]
			if bytes.HasPrefix(line, []byte("data:")) {
				o.consume(bytes.TrimSpace(line[5:]))
			}
		}
	}
	if len(o.buffer) > 2<<20 {
		o.buffer = nil
		o.overflow = true
	}
}

func (o *observer) consume(data []byte) {
	if bytes.Equal(data, []byte("[DONE]")) {
		return
	}
	var payload map[string]any
	if json.Unmarshal(data, &payload) != nil {
		return
	}
	if value, ok := payload["error"]; ok && value != nil {
		o.failed = true
		o.event.Outcome = "api_error"
	}
	if limit := classify(o.event.Provider, o.event.Status, o.headers, payload, o.event.At); limit != nil {
		o.event.Limit = limit
	}
	o.takeUsage(payload["usage"])
	if message, ok := payload["message"].(map[string]any); ok {
		o.takeUsage(message["usage"])
	}
	if payload["type"] == "message_stop" {
		o.complete = true
	}
	if choices, ok := payload["choices"].([]any); ok {
		for _, value := range choices {
			if choice, ok := value.(map[string]any); ok {
				if reason, ok := choice["finish_reason"].(string); ok && reason != "" {
					if reason == "error" {
						o.failed = true
					} else {
						o.complete = true
					}
				}
			}
		}
	}
	if !o.stream && (payload["type"] == "message" || payload["choices"] != nil) {
		o.complete = true
	}
}

func intValue(values map[string]any, key string) *int {
	value, ok := values[key].(float64)
	if !ok || value < 0 {
		return nil
	}
	n := int(value)
	return &n
}
func (o *observer) takeUsage(value any) {
	values, ok := value.(map[string]any)
	if !ok {
		return
	}
	for _, entry := range []struct {
		key string
		dst **int
	}{{"prompt_tokens", &o.event.Input}, {"input_tokens", &o.event.Input}, {"completion_tokens", &o.event.Output}, {"output_tokens", &o.event.Output}, {"cache_read_input_tokens", &o.event.CacheRead}, {"cache_creation_input_tokens", &o.event.CacheWrite}} {
		if n := intValue(values, entry.key); n != nil {
			*entry.dst = n
		}
	}
	if details, ok := values["prompt_tokens_details"].(map[string]any); ok {
		o.event.CacheRead = intValue(details, "cached_tokens")
	}
	if cost, ok := values["cost"].(float64); ok && cost >= 0 {
		o.event.Cost = &cost
	}
}

func (o *observer) finish() {
	o.once.Do(func() {
		if !o.overflow {
			if o.stream {
				line := bytes.TrimSpace(o.buffer)
				if bytes.HasPrefix(line, []byte("data:")) {
					o.consume(bytes.TrimSpace(line[5:]))
				}
			} else {
				o.consume(o.buffer)
			}
		}
		if o.event.Limit == nil {
			o.event.Limit = classify(o.event.Provider, o.event.Status, o.headers, nil, o.event.At)
		}
		if o.event.Status >= 400 {
			o.failed = true
			o.event.Outcome = "http_error"
		}
		if o.complete && !o.failed && !o.overflow {
			o.event.Outcome = "ok"
		}
		o.event.DurationMS = time.Since(o.event.At).Milliseconds()
		if err := o.transport.Store.Record(o.event); err != nil && o.transport.Warn != nil {
			o.transport.warnOnce.Do(func() { o.transport.Warn("Usage tracking could not be saved; local counters are incomplete.") })
		}
		if limit := o.event.Limit; limit != nil && o.transport.Warn != nil {
			if strings.HasPrefix(limit.Kind, "free_") || limit.Kind == "upstream_rate" || limit.Kind == "rate_limit" {
				o.transport.quotaOnce.Do(func() {
					detail := "reset unknown"
					if limit.ResetAt != nil {
						detail = "retry/reset " + limit.ResetAt.Local().Format(time.RFC3339)
					}
					o.transport.Warn(fmt.Sprintf("%s: %s (%s scope, %s). Check clother usage or clother usage next %s %s.", o.event.Provider, limit.Kind, limit.Scope, detail, o.event.Provider, o.event.Model))
				})
			}
		}
	})
}
