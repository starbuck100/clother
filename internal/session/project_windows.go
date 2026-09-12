//go:build windows

package session

import (
	"os"
	"path/filepath"
	"strings"
)

// resolveProjectDir returns the session directory for a working directory,
// falling back to a case-insensitive match.
//
// Windows carries one path under many spellings — a drive letter is however it
// was typed — and the slug preserves that case, so `C:\work` and `c:\work`
// produce two directories that Claude Code treats as a single project (its own
// comparison lowercases on Windows). When the exact name is missing, the
// case-insensitive match is that same project.
func resolveProjectDir(root, cwd string) string {
	exact := ProjectDir(root, cwd)
	if exact == "" {
		return ""
	}
	if _, err := os.Stat(exact); err == nil {
		return exact
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return exact
	}
	name := filepath.Base(exact)
	for _, entry := range entries {
		if entry.IsDir() && strings.EqualFold(entry.Name(), name) {
			return filepath.Join(root, entry.Name())
		}
	}
	return exact
}
