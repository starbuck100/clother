//go:build !windows

package runtime

import (
	"os"
	"syscall"
)

// forwardSignals relays signals the parent received to the Claude Code child.
//
// The shell that started clother is in the parent's process group, not the
// child's, so an interrupt sent to the foreground job does not reach the child
// on its own and has to be passed along explicitly.
func forwardSignals(process *os.Process, signals <-chan os.Signal) {
	for sig := range signals {
		if signalValue, ok := sig.(syscall.Signal); ok {
			_ = process.Signal(signalValue)
		}
	}
}
