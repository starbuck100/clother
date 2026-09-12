//go:build !windows

package platform

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// ReadSecretLine reads one line from the controlling terminal with echo turned
// off, so a pasted API key never lands in the console scrollback.
//
// The prompt goes to out but the input is read from /dev/tty, which is what
// makes the function usable when stdin is a pipe. handled is false when there
// is no terminal to read from — a piped invocation, for instance — in which
// case the caller falls back to a plain echoing prompt.
func ReadSecretLine(prompt string, out io.Writer) (string, bool) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", false
	}
	defer tty.Close()

	// Echo is switched off before anything is written, so that a failure here
	// returns before the prompt appears and the caller's fallback is the only
	// prompt the user ever sees.
	if err := setTTYEcho(false); err != nil {
		return "", false
	}
	defer setTTYEcho(true)

	fmt.Fprintf(out, "%s: ", prompt)
	value, readErr := bufio.NewReader(tty).ReadString('\n')
	// With echo off the terminal did not render the newline the user typed.
	fmt.Fprintln(out)
	if readErr != nil && readErr != io.EOF {
		return "", false
	}
	return strings.TrimSpace(value), true
}

// IsTerminal reports whether file is attached to a terminal.
func IsTerminal(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && (info.Mode()&os.ModeCharDevice) != 0
}

// EnableVirtualTerminal is a no-op on Unix, where terminals have always
// understood escape sequences.
func EnableVirtualTerminal() {}

func setTTYEcho(enabled bool) error {
	args := []string{}
	switch runtime.GOOS {
	case "darwin", "freebsd":
		args = append(args, "-f", "/dev/tty")
	default:
		args = append(args, "-F", "/dev/tty")
	}
	if enabled {
		args = append(args, "echo")
	} else {
		args = append(args, "-echo")
	}
	cmd := exec.Command("stty", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}
