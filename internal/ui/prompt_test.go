package ui

import (
	"io"
	"strings"
	"testing"
)

func TestPromptKeepsBufferedAnswers(t *testing.T) {
	p := NewPrompter(strings.NewReader("first\nsecond\n\n"), io.Discard)
	for _, want := range []string{"first", "second", "fallback"} {
		got, err := p.Prompt("value", "fallback")
		if err != nil || got != want {
			t.Fatalf("got %q, %v; want %q", got, err, want)
		}
	}
}
