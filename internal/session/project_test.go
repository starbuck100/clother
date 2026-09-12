package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The expected directory names below are the ones Claude Code itself derives:
// every character that is not an ASCII letter or digit becomes a hyphen. Both
// separators are covered because the slug must come out the same on either
// platform — filepath.Clean normalises them, and the rule collapses them anyway.
func TestProjectDirMatchesClaudeLayout(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cases := []struct {
		name string
		cwd  string
		want string
	}{
		{"unix path", "/Users/max/Downloads/clother", "-Users-max-Downloads-clother"},
		{"windows path", `C:\Users\max\dev\clother`, "C--Users-max-dev-clother"},
		{"forward slashes", "C:/Users/max/dev/clother", "C--Users-max-dev-clother"},
		{"underscore collapses", `/home/max/my_project`, "-home-max-my-project"},
		{"dot collapses", "/home/max/.config/app", "-home-max--config-app"},
		{"space collapses", "/home/max/My Projects", "-home-max-My-Projects"},
		{"root", "/", "-"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ProjectDir(root, tc.cwd)
			want := filepath.Join(root, tc.want)
			if got != want {
				t.Fatalf("ProjectDir(%q) = %q, want %q", tc.cwd, got, want)
			}
		})
	}
}

func TestProjectDirNamesNothingForAnUnusablePath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	// Past Claude Code's limit it appends a hash of the original path that
	// cannot be reproduced here. Naming a directory anyway would point the
	// resume hint at some other project's session.
	long := "/home/max/" + strings.Repeat("deeply-nested/", 30)
	if slug := projectSlug(long); len(slug) > slugMaxLen {
		t.Fatalf("test path produced a %d character slug, want it over the %d limit", len(slug), slugMaxLen)
	}
	if got := ProjectDir(root, long); got != "" {
		t.Fatalf("ProjectDir() = %q for an over-long path, want the empty string", got)
	}

	// The directory the process happens to be in is not a project path.
	if got := ProjectDir(root, "."); got != "" {
		t.Fatalf("ProjectDir(\".\") = %q, want the empty string", got)
	}
}

func TestLatestInProjectReturnsMostRecentSession(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cwd := `C:\Users\max\dev\clother`
	projectDir := ProjectDir(root, cwd)
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	older := filepath.Join(projectDir, "older.jsonl")
	newer := filepath.Join(projectDir, "newer.jsonl")
	if err := os.WriteFile(older, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(newer, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A session file is not the only thing that can appear in there.
	if err := os.MkdirAll(filepath.Join(projectDir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "notes.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := LatestInProject(root, cwd)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "newer" {
		t.Fatalf("LatestInProject() ID = %q, want newer", got.ID)
	}
	if got.Path != newer {
		t.Fatalf("LatestInProject() Path = %q, want %q", got.Path, newer)
	}
}

func TestLatestInProjectWithoutADirectory(t *testing.T) {
	t.Parallel()

	got, err := LatestInProject(t.TempDir(), `C:\Users\nobody\never-ran-claude`)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "" {
		t.Fatalf("LatestInProject() ID = %q, want empty", got.ID)
	}
}

func TestChangedProjectSession(t *testing.T) {
	t.Parallel()

	now := time.Now()
	before := ProjectSession{ID: "abc", ModTime: now}
	after := ProjectSession{ID: "abc", ModTime: now.Add(time.Second)}
	if !ChangedProjectSession(before, after) {
		t.Fatal("expected updated modtime to count as changed")
	}
	if ChangedProjectSession(after, after) {
		t.Fatal("an unchanged session must not count as changed")
	}
	if !ChangedProjectSession(ProjectSession{}, after) {
		t.Fatal("a first session must count as changed")
	}
	if ChangedProjectSession(after, ProjectSession{}) {
		t.Fatal("a vanished session must not count as changed")
	}
}
