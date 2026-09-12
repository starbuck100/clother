//go:build !windows

package platform

import (
	"context"
	"os/exec"
)

// ExecutableNames returns the file names a command may be installed under
// inside a single directory, in resolution order.
func ExecutableNames(base string) []string {
	return []string{base}
}

// CommandFor builds the command that launches the real Claude Code at path.
//
// On Unix the binary is executed directly; there is no shell layer, so every
// argument reaches the child byte for byte.
func CommandFor(ctx context.Context, path string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, path, args...)
}
