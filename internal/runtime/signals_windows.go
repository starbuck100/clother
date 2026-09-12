//go:build windows

package runtime

import "os"

// forwardSignals is deliberately inert on Windows.
//
// There is nothing to relay: a console control event such as Ctrl-C is
// delivered by the console to every process attached to it, so the Claude Code
// child receives it directly. Go's os.Process.Signal, meanwhile, can only
// terminate a process on Windows — passing an interrupt through it would kill
// the session instead of interrupting what it is doing, which is worse than
// doing nothing.
func forwardSignals(process *os.Process, signals <-chan os.Signal) {
	for range signals {
	}
}
