package session

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type ProjectSession struct {
	ID      string
	Path    string
	ModTime time.Time
}

// slugMaxLen is Claude Code's own limit on the directory name it derives from a
// working directory.
const slugMaxLen = 200

// ProjectDir returns the directory Claude Code keeps a project's sessions in.
//
// The rule is Claude Code's rather than an approximation of it: every character
// that is not an ASCII letter or digit becomes a hyphen, and nothing is added at
// the front — a Unix path therefore begins with the hyphen its leading slash
// became, while a Windows path begins with the drive letter, `C--Users-…`. It
// was read out of the installed CLI (`e.replace(/[^a-zA-Z0-9]/g,"-")`), which is
// also why underscores and dots collapse: replacing only the path separator, as
// this used to, named a directory that does not exist for any project whose path
// contains one, so the resume hint silently never fired there.
//
// One case is deliberately not reproduced. Claude Code honours
// CLAUDE_CODE_PROJECT_DIR_NAME instead of the derived name when CLAUDE_CONFIG_DIR
// is set alongside it, and resolving that would need the *child's* environment
// rather than this process's. A user who sets it gets a hint that stays silent,
// which is the same as having no hint at all.
func ProjectDir(root, cwd string) string {
	slug := projectSlug(cwd)
	if slug == "" {
		return ""
	}
	return filepath.Join(root, slug)
}

func projectSlug(cwd string) string {
	clean := filepath.Clean(cwd)
	if clean == "." || clean == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(clean))
	for _, r := range clean {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		// JavaScript's regex walks UTF-16 code units, so a character outside the
		// basic plane becomes two hyphens there; match that rather than the rune.
		b.WriteByte('-')
		if r > 0xFFFF {
			b.WriteByte('-')
		}
	}
	slug := b.String()
	// Past the limit Claude Code appends a hash of the original path that is
	// not reproducible here, so this deliberately names nothing instead of
	// guessing: a resume hint that stays silent is honest, one that points at a
	// different project's session is not.
	if len(slug) > slugMaxLen {
		return ""
	}
	return slug
}

// LatestInProject reports the most recent session in the project containing
// cwd, if there is one.
func LatestInProject(root, cwd string) (ProjectSession, error) {
	dir := resolveProjectDir(root, cwd)
	if dir == "" {
		return ProjectSession{}, nil
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return ProjectSession{}, nil
	}
	if err != nil {
		return ProjectSession{}, err
	}

	var sessions []ProjectSession
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".jsonl")
		sessions = append(sessions, ProjectSession{
			ID:      id,
			Path:    filepath.Join(dir, entry.Name()),
			ModTime: info.ModTime(),
		})
	}
	sort.SliceStable(sessions, func(i, j int) bool {
		if sessions[i].ModTime.Equal(sessions[j].ModTime) {
			return sessions[i].ID < sessions[j].ID
		}
		return sessions[i].ModTime.After(sessions[j].ModTime)
	})
	if len(sessions) == 0 {
		return ProjectSession{}, nil
	}
	return sessions[0], nil
}

func ChangedProjectSession(before, after ProjectSession) bool {
	if after.ID == "" {
		return false
	}
	if before.ID == "" {
		return true
	}
	if before.ID != after.ID {
		return true
	}
	return after.ModTime.After(before.ModTime)
}
