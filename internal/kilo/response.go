package kilo

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type toolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}
type output struct {
	Content   string     `json:"content"`
	ToolCalls []toolCall `json:"tool_calls"`
}
type completion struct {
	ID      string `json:"id"`
	Choices []struct {
		Message output `json:"message"`
		Delta   output `json:"delta"`
		Finish  string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		Input  int `json:"prompt_tokens"`
		Output int `json:"completion_tokens"`
	} `json:"usage"`
	Error json.RawMessage `json:"error"`
}

func stopReason(reason string) (string, error) {
	switch reason {
	case "stop":
		return "end_turn", nil
	case "tool_calls", "function_call":
		return "tool_use", nil
	case "length":
		return "max_tokens", nil
	default:
		return "", fmt.Errorf("Kilo ended with unsupported finish reason %q", reason)
	}
}
func (c completion) message(model string) (map[string]any, error) {
	if len(c.Error) > 0 && string(c.Error) != "null" {
		return nil, fmt.Errorf("Kilo returned an API error")
	}
	if len(c.Choices) != 1 {
		return nil, fmt.Errorf("Kilo response has no unique completion")
	}
	choice := c.Choices[0]
	reason, err := stopReason(choice.Finish)
	if err != nil {
		return nil, err
	}
	content := []map[string]any{}
	if choice.Message.Content != "" {
		content = append(content, map[string]any{"type": "text", "text": choice.Message.Content})
	}
	for _, call := range choice.Message.ToolCalls {
		var args map[string]any
		if call.ID == "" || call.Function.Name == "" || json.Unmarshal([]byte(call.Function.Arguments), &args) != nil || args == nil {
			return nil, fmt.Errorf("Kilo returned an invalid tool call")
		}
		content = append(content, map[string]any{"type": "tool_use", "id": call.ID, "name": call.Function.Name, "input": args})
	}
	if len(content) == 0 {
		return nil, fmt.Errorf("Kilo returned no text or tool calls; try a larger output limit or another model")
	}
	return map[string]any{"id": c.ID, "type": "message", "role": "assistant", "model": model, "content": content, "stop_reason": reason, "stop_sequence": nil, "usage": map[string]int{"input_tokens": c.Usage.Input, "output_tokens": c.Usage.Output}}, nil
}

type streamedTool struct {
	id, name, args string
	index          int
}

func streamResponse(w http.ResponseWriter, body io.Reader, model string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	emit := func(kind string, fields map[string]any) {
		fields["type"] = kind
		data, _ := json.Marshal(fields)
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, data)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
	fail := func(message string) {
		emit("error", map[string]any{"error": map[string]any{"type": "api_error", "message": message}})
	}
	started := false
	textIndex := -1
	next := 0
	tools := map[int]*streamedTool{}
	var order []*streamedTool
	finish := ""
	inputTokens, outputTokens := 0, 0
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	var data []string
	done := false
	consume := func() bool {
		if len(data) == 0 {
			return true
		}
		payload := strings.Join(data, "\n")
		data = nil
		if payload == "[DONE]" {
			done = true
			return true
		}
		var chunk completion
		if json.Unmarshal([]byte(payload), &chunk) != nil {
			fail("invalid Kilo stream event")
			return false
		}
		if len(chunk.Error) > 0 && string(chunk.Error) != "null" {
			fail("Kilo returned a streaming API error")
			return false
		}
		if !started {
			emit("message_start", map[string]any{"message": map[string]any{"id": chunk.ID, "type": "message", "role": "assistant", "model": model, "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]int{"input_tokens": chunk.Usage.Input, "output_tokens": 0}}})
			started = true
		}
		if chunk.Usage.Input > 0 {
			inputTokens = chunk.Usage.Input
		}
		if chunk.Usage.Output > 0 {
			outputTokens = chunk.Usage.Output
		}
		if len(chunk.Choices) == 0 {
			return true
		}
		if len(chunk.Choices) != 1 {
			fail("Kilo returned multiple completions")
			return false
		}
		choice := chunk.Choices[0]
		if choice.Delta.Content != "" {
			if textIndex < 0 {
				textIndex = next
				next++
				emit("content_block_start", map[string]any{"index": textIndex, "content_block": map[string]any{"type": "text", "text": ""}})
			}
			emit("content_block_delta", map[string]any{"index": textIndex, "delta": map[string]any{"type": "text_delta", "text": choice.Delta.Content}})
		}
		for _, call := range choice.Delta.ToolCalls {
			t := tools[call.Index]
			if t == nil {
				t = &streamedTool{}
				tools[call.Index] = t
				order = append(order, t)
			}
			t.id += call.ID
			t.name += call.Function.Name
			t.args += call.Function.Arguments
			// Emit the tool only after its complete name and arguments are known. This
			// handles split names, interleaved calls and rejects truncated JSON before
			// Claude can execute a partially received tool.
		}
		if choice.Finish != "" {
			finish = choice.Finish
		}
		return true
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if !consume() {
				return
			}
			if done {
				break
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if len(data) > 0 && !consume() {
		return
	}
	if scanner.Err() != nil {
		fail("Kilo stream was interrupted")
		return
	}
	if !done || finish == "" {
		fail("Kilo stream ended before completion")
		return
	}
	reason, err := stopReason(finish)
	if err != nil {
		fail(err.Error())
		return
	}
	if textIndex < 0 && len(order) == 0 {
		fail("Kilo returned no text or tool calls; try a larger output limit or another model")
		return
	}
	for _, t := range order {
		var args map[string]any
		if t.id == "" || t.name == "" || json.Unmarshal([]byte(t.args), &args) != nil || args == nil {
			fail("Kilo returned an incomplete tool call")
			return
		}
	}
	if textIndex >= 0 {
		emit("content_block_stop", map[string]any{"index": textIndex})
	}
	for _, t := range order {
		t.index = next
		next++
		emit("content_block_start", map[string]any{"index": t.index, "content_block": map[string]any{"type": "tool_use", "id": t.id, "name": t.name, "input": map[string]any{}}})
		emit("content_block_delta", map[string]any{"index": t.index, "delta": map[string]any{"type": "input_json_delta", "partial_json": t.args}})
		emit("content_block_stop", map[string]any{"index": t.index})
	}
	emit("message_delta", map[string]any{"delta": map[string]any{"stop_reason": reason, "stop_sequence": nil}, "usage": map[string]int{"input_tokens": inputTokens, "output_tokens": outputTokens}})
	emit("message_stop", map[string]any{})
}
