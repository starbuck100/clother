//go:build windows

package platform

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// defaultPathExt is the value Windows uses when PATHEXT is unset.
const defaultPathExt = ".COM;.EXE;.BAT;.CMD"

// ExecutableNames returns the file names a command may be installed under
// inside a single directory, in resolution order, derived from PATHEXT.
//
// This matters for finding the real Claude Code: the Windows loader will not
// execute an extensionless file, so looking for a literal "claude" misses both
// a native install's claude.exe and npm's claude.cmd.
func ExecutableNames(base string) []string {
	pathExt := os.Getenv("PATHEXT")
	if pathExt == "" {
		pathExt = defaultPathExt
	}
	var names []string
	for _, ext := range strings.Split(pathExt, ";") {
		ext = strings.TrimSpace(ext)
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		names = append(names, base+ext)
	}
	// The bare name is kept last so a Unix-style extensionless file still
	// resolves if one is ever placed on PATH.
	return append(names, base)
}

// CommandFor builds the command that launches the real Claude Code at path.
//
// Go's os/exec hands a path straight to CreateProcess, which refuses batch
// files with ERROR_BAD_EXE_FORMAT. An npm-installed Claude Code is a
// `claude.cmd`, so that case has to go through cmd.exe — the one place in this
// port where arguments are re-parsed by a second interpreter and therefore
// cannot be made perfectly transparent. Set CLOTHER_CLAUDE_LAUNCH=direct to
// bypass the shell, or =cmd to force it.
func CommandFor(ctx context.Context, path string, args ...string) *exec.Cmd {
	if !needsShell(path) {
		return exec.CommandContext(ctx, path, args...)
	}
	line := make([]string, 0, len(args)+1)
	line = append(line, quoteForCmd(path))
	for _, arg := range args {
		line = append(line, quoteForCmd(arg))
	}
	// The extra outer pair is what makes cmd.exe /s stop at the first argument
	// instead of eating the opening quote of the command itself.
	return exec.CommandContext(ctx, comspec(), "/d", "/s", "/c", `"`+strings.Join(line, " ")+`"`)
}

func needsShell(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".cmd", ".bat":
	default:
		return false
	}
	switch strings.ToLower(os.Getenv("CLOTHER_CLAUDE_LAUNCH")) {
	case "direct":
		return false
	case "cmd":
		return true
	}
	return true
}

func comspec() string {
	if value := os.Getenv("COMSPEC"); value != "" {
		return value
	}
	if root := os.Getenv("SystemRoot"); root != "" {
		return filepath.Join(root, "System32", "cmd.exe")
	}
	return "cmd.exe"
}

// quoteForCmd renders one argument for a cmd.exe command line. cmd.exe does not
// take an argv: it re-parses the whole line, so quoting is a best effort. The
// characters that force quoting are exactly the ones cmd treats as syntax.
func quoteForCmd(arg string) string {
	if arg == "" {
		return `""`
	}
	if !strings.ContainsAny(arg, " \t\"&|<>^()%!,") {
		return arg
	}
	return `"` + strings.ReplaceAll(arg, `"`, `\"`) + `"`
}
