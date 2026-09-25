// Package kilo adapts Claude's Messages API to the public Kilo gateway.
package kilo

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/jolehuit/clother/internal/openrouter"
)

type Request struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	Stream    bool            `json:"stream"`
	System    json.RawMessage `json:"system"`
	Messages  []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"messages"`
	Tools []struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Schema      json.RawMessage `json:"input_schema"`
	} `json:"tools"`
	ToolChoice  map[string]any `json:"tool_choice"`
	Temperature *float64       `json:"temperature"`
	TopP        *float64       `json:"top_p"`
	Stop        []string       `json:"stop_sequences"`
}

type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	Source    struct {
		Type      string `json:"type"`
		URL       string `json:"url"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
	} `json:"source"`
}

func blocks(raw json.RawMessage) ([]block, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return []block{{Type: "text", Text: text}}, nil
	}
	var result []block
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("invalid message content")
	}
	return result, nil
}

func textContent(raw json.RawMessage) (string, error) {
	parts, err := blocks(raw)
	if err != nil {
		return "", err
	}
	var texts []string
	for _, part := range parts {
		if part.Type != "text" {
			return "", fmt.Errorf("Kilo does not support %q in system prompts or tool results", part.Type)
		}
		texts = append(texts, part.Text)
	}
	return strings.Join(texts, "\n"), nil
}

func translateRequest(r Request, model openrouter.Model) (map[string]any, error) {
	var messages []map[string]any
	if len(r.System) > 0 {
		text, err := textContent(r.System)
		if err != nil {
			return nil, err
		}
		if text != "" {
			messages = append(messages, map[string]any{"role": "system", "content": text})
		}
	}
	for _, message := range r.Messages {
		// Some Claude Code paths put additional system messages in the message
		// list rather than the top-level system field.
		if message.Role == "system" {
			text, err := textContent(message.Content)
			if err != nil {
				return nil, err
			}
			messages = append(messages, map[string]any{"role": "system", "content": text})
			continue
		}
		if message.Role != "user" && message.Role != "assistant" {
			return nil, fmt.Errorf("unsupported message role %q", message.Role)
		}
		parts, err := blocks(message.Content)
		if err != nil {
			return nil, err
		}
		var content []map[string]any
		var calls []map[string]any
		flush := func() {
			if len(content) == 0 && len(calls) == 0 {
				return
			}
			item := map[string]any{"role": message.Role, "content": content}
			if len(calls) > 0 {
				item["tool_calls"] = calls
			}
			messages = append(messages, item)
			content = nil
			calls = nil
		}
		for _, part := range parts {
			switch part.Type {
			case "text":
				content = append(content, map[string]any{"type": "text", "text": part.Text})
			case "image":
				if !slices.Contains(model.Architecture.InputModalities, "image") {
					return nil, fmt.Errorf("%s does not advertise image input", model.ID)
				}
				url := part.Source.URL
				if part.Source.Type == "base64" {
					url = "data:" + part.Source.MediaType + ";base64," + part.Source.Data
				}
				if url == "" {
					return nil, fmt.Errorf("image source missing")
				}
				content = append(content, map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}})
			case "tool_use":
				if message.Role != "assistant" {
					return nil, fmt.Errorf("tool_use must belong to assistant")
				}
				calls = append(calls, map[string]any{"id": part.ID, "type": "function", "function": map[string]any{"name": part.Name, "arguments": string(part.Input)}})
			case "tool_result":
				if message.Role != "user" {
					return nil, fmt.Errorf("tool_result must belong to user")
				}
				flush()
				text, err := textContent(part.Content)
				if err != nil {
					return nil, err
				}
				messages = append(messages, map[string]any{"role": "tool", "tool_call_id": part.ToolUseID, "content": text})
			case "thinking", "redacted_thinking":
				// Provider-specific reasoning signatures are not portable history.
			default:
				return nil, fmt.Errorf("Kilo does not support message block %q", part.Type)
			}
		}
		flush()
	}
	if len(messages) == 0 {
		return nil, fmt.Errorf("messages are required")
	}
	ceiling := min(32000, max(1, model.ContextLimit()/4))
	if model.TopProvider.MaxCompletionTokens > 0 {
		ceiling = min(ceiling, model.TopProvider.MaxCompletionTokens)
	} else {
		ceiling = min(ceiling, 4096)
	}
	if r.MaxTokens > 0 {
		ceiling = min(ceiling, r.MaxTokens)
	}
	out := map[string]any{"model": r.Model, "messages": messages, "max_tokens": ceiling, "stream": r.Stream}
	if r.Stream {
		out["stream_options"] = map[string]any{"include_usage": true}
	}
	if r.Temperature != nil && model.Supports("temperature") {
		out["temperature"] = *r.Temperature
	}
	if r.TopP != nil && model.Supports("top_p") {
		out["top_p"] = *r.TopP
	}
	if len(r.Stop) > 0 && model.Supports("stop") {
		out["stop"] = r.Stop
	}
	// Do not force reasoning off: some free endpoints require it. The output
	// ceiling applies to generated tokens; low effort is used where advertised.
	if model.Supports("reasoning_effort") {
		out["reasoning"] = map[string]any{"effort": "low"}
	}
	if len(r.Tools) > 0 {
		var tools []map[string]any
		for _, tool := range r.Tools {
			if tool.Name == "" || len(tool.Schema) == 0 {
				return nil, fmt.Errorf("Kilo requires client tools with input schemas; server tools are unsupported")
			}
			tools = append(tools, map[string]any{"type": "function", "function": map[string]any{"name": tool.Name, "description": tool.Description, "parameters": tool.Schema}})
		}
		out["tools"] = tools
		if choice, ok := r.ToolChoice["type"].(string); ok {
			switch choice {
			case "auto", "none":
				out["tool_choice"] = choice
			case "any":
				out["tool_choice"] = "required"
			case "tool":
				out["tool_choice"] = map[string]any{"type": "function", "function": map[string]any{"name": r.ToolChoice["name"]}}
			default:
				return nil, fmt.Errorf("unsupported tool choice %q", choice)
			}
		}
		if disabled, ok := r.ToolChoice["disable_parallel_tool_use"].(bool); ok && disabled {
			out["parallel_tool_calls"] = false
		}
	}
	return out, nil
}
