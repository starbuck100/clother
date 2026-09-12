//go:build windows

package platform

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
)

// Console mode flags from the Windows console API. Go's syscall package
// exposes GetConsoleMode but not SetConsoleMode, so the latter is resolved
// through a lazy DLL handle rather than pulling in golang.org/x/sys.
const (
	enableLineInput                 = 0x0002
	enableEchoInput                 = 0x0004
	enableVirtualTerminalProcessing = 0x0004
)

var procSetConsoleMode = syscall.NewLazyDLL("kernel32.dll").NewProc("SetConsoleMode")

// ReadSecretLine reads one line from the console with echo turned off, so a
// pasted API key never lands in the console scrollback or in Windows Terminal's
// history.
//
// handled is false when stdin is not a console — a piped or redirected
// invocation, in which case the caller falls back to a plain echoing prompt.
func ReadSecretLine(prompt string, out io.Writer) (string, bool) {
	handle := syscall.Handle(os.Stdin.Fd())

	var mode uint32
	if err := syscall.GetConsoleMode(handle, &mode); err != nil {
		// No console at all, so there is nothing the input could be echoed to.
		return "", false
	}

	// ENABLE_ECHO_INPUT is cleared while ENABLE_LINE_INPUT is kept: ReadFile
	// still returns a whole line once Enter is pressed, it just does not draw
	// it. Echo goes off before the prompt is written, so a failure here leaves
	// the caller's fallback prompt as the only one the user sees.
	if err := setConsoleMode(handle, mode&^enableEchoInput); err != nil {
		// A console exists but its mode could not be changed, so the secret
		// would be typed in the clear. Saying so is the whole point: falling
		// back silently is exactly the bug this function exists to fix.
		fmt.Fprintln(out, "warning: cannot disable terminal echo here; input will be visible")
		return "", false
	}
	defer setConsoleMode(handle, mode)

	fmt.Fprintf(out, "%s: ", prompt)
	value, readErr := bufio.NewReader(os.Stdin).ReadString('\n')
	// With echo off the console did not render the newline the user typed.
	fmt.Fprintln(out)
	if readErr != nil && readErr != io.EOF {
		return "", false
	}
	return strings.TrimSpace(value), true
}

// IsTerminal reports whether file is attached to a console. This is asked of
// the console API rather than inferred from the file mode, because that is the
// question that actually decides whether the mode flags can be set.
func IsTerminal(file *os.File) bool {
	var mode uint32
	return syscall.GetConsoleMode(syscall.Handle(file.Fd()), &mode) == nil
}

// EnableVirtualTerminal asks the console to interpret ANSI escape sequences, so
// the banner and the coloured markers render instead of printing their escape
// codes. Windows Terminal does this already; the legacy console host, which is
// still what cmd.exe gets on some installs, does not. Nothing happens when
// stdout is not a console — a redirected stream has no mode to change.
func EnableVirtualTerminal() {
	handle := syscall.Handle(os.Stdout.Fd())
	var mode uint32
	if err := syscall.GetConsoleMode(handle, &mode); err != nil {
		return
	}
	if mode&enableVirtualTerminalProcessing != 0 {
		return
	}
	_ = setConsoleMode(handle, mode|enableVirtualTerminalProcessing)
}

func setConsoleMode(handle syscall.Handle, mode uint32) error {
	result, _, err := procSetConsoleMode.Call(uintptr(handle), uintptr(mode))
	if result == 0 {
		return err
	}
	return nil
}
