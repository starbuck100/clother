package ui

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/jolehuit/clother/internal/platform"
)

type Prompter struct {
	In  io.Reader
	Out io.Writer
}

func NewPrompter(in io.Reader, out io.Writer) *Prompter {
	return &Prompter{In: in, Out: out}
}

func (p *Prompter) Prompt(label, defaultValue string) (string, error) {
	if defaultValue != "" {
		fmt.Fprintf(p.Out, "%s [%s]: ", label, defaultValue)
	} else {
		fmt.Fprintf(p.Out, "%s: ", label)
	}
	reader := bufio.NewReader(p.In)
	value, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultValue, nil
	}
	return value, nil
}

// PromptSecret reads a secret without echoing it to the terminal.
//
// When no terminal is available to switch the echo off on, the plain prompt is
// used instead of failing: refusing would make the command unusable in the
// non-interactive setups that rely on it.
func (p *Prompter) PromptSecret(label string) (string, error) {
	value, handled := platform.ReadSecretLine(label, p.Out)
	if !handled {
		return p.Prompt(label, "")
	}
	return value, nil
}

func (p *Prompter) Confirm(label string, defaultYes bool) (bool, error) {
	hint := "[y/N]"
	if defaultYes {
		hint = "[Y/n]"
	}
	answer, err := p.Prompt(label+" "+hint, "")
	if err != nil {
		return false, err
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer == "" {
		return defaultYes, nil
	}
	return strings.HasPrefix(answer, "y"), nil
}

