package launchers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jolehuit/clother/internal/platform"
)

const testClotherPath = "/opt/clother/bin/clother"

func syncTestCommands(t *testing.T, dir string) []GeneratedFile {
	t.Helper()

	files, err := SyncCommands(dir, CommandFileOptions{Clother: testClotherPath})
	if err != nil {
		t.Fatalf("SyncCommands error = %v", err)
	}
	if len(files) == 0 {
		t.Fatal("SyncCommands wrote nothing")
	}
	return files
}

func TestSyncCommandsWritesEveryFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	files := syncTestCommands(t, dir)

	if len(files) != len(commandTemplates) {
		t.Fatalf("SyncCommands wrote %d files, want %d", len(files), len(commandTemplates))
	}

	for _, file := range files {
		if file.SHA256 == "" {
			t.Errorf("%s has no recorded hash", file.Path)
		}
		body, err := os.ReadFile(file.Path)
		if err != nil {
			t.Fatalf("reading %s: %v", file.Path, err)
		}
		// Frontmatter is read only when it begins at line 1; otherwise the whole
		// file, markers included, is content.
		if !strings.HasPrefix(string(body), "---\n") {
			t.Errorf("%s does not begin with frontmatter:\n%s", file.Path, body)
		}
		// The whole layout matters, not just the last segment: the files are
		// commands only if they sit under commands/, which is where Claude Code
		// looks, and the directory below it is what makes them /clother:*.
		want := filepath.Join(dir, commandRootName, commandDirName, filepath.Base(file.Path))
		if file.Path != want {
			t.Errorf("%s is not where Claude Code reads user commands from; want %s", file.Path, want)
		}
	}
}

// The one hard rule for these files. Argument substitution runs over the whole
// document before shell execution, so a placeholder inside a command position
// would execute whatever the user typed. Nothing here may be a command
// substitution, and the assertion names the exact form rather than banning the
// backtick, which is ordinary prose in a description.
func TestGeneratedCommandsContainNoShellExecution(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, file := range syncTestCommands(t, dir) {
		body, err := os.ReadFile(file.Path)
		if err != nil {
			t.Fatalf("reading %s: %v", file.Path, err)
		}
		content := string(body)

		if strings.Contains(content, "```!") {
			t.Errorf("%s opens a shell block", filepath.Base(file.Path))
		}
		if strings.Contains(content, "!`") {
			t.Errorf("%s contains inline command substitution", filepath.Base(file.Path))
		}
		if strings.Contains(content, "shell:") {
			t.Errorf("%s sets a shell in its frontmatter, which differs per platform and per machine", filepath.Base(file.Path))
		}
	}
}

// The helper call and the namespace are what make the files work, so they are
// pinned by content rather than left to the template.
func TestGeneratedCommandsCallTheHelper(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, file := range syncTestCommands(t, dir) {
		body, err := os.ReadFile(file.Path)
		if err != nil {
			t.Fatalf("reading %s: %v", file.Path, err)
		}
		content := string(body)

		if !strings.Contains(content, testClotherPath+" __session ") {
			t.Errorf("%s does not call the helper through the absolute path:\n%s", filepath.Base(file.Path), content)
		}
		if !strings.Contains(content, "disable-model-invocation: true") {
			t.Errorf("%s lets the model invoke it on its own", filepath.Base(file.Path))
		}
		if !strings.Contains(content, "allowed-tools:") {
			t.Errorf("%s would stop for a permission prompt", filepath.Base(file.Path))
		}
	}
}

// A matching file is not rewritten. The distinctive past modification time is
// what makes that observable: a rewrite sets it to now on any platform.
func TestSyncCommandsLeavesMatchingFilesAlone(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	files := syncTestCommands(t, dir)

	past := time.Date(2001, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, file := range files {
		if err := os.Chtimes(file.Path, past, past); err != nil {
			t.Fatal(err)
		}
	}

	syncTestCommands(t, dir)

	for _, file := range files {
		info, err := os.Stat(file.Path)
		if err != nil {
			t.Fatal(err)
		}
		if !info.ModTime().Equal(past) {
			t.Errorf("%s was rewritten although its content already matched", filepath.Base(file.Path))
		}
	}
}

// A file whose content differs is brought up to date, which is what makes a
// template change reach an existing installation.
func TestSyncCommandsRewritesChangedFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	files := syncTestCommands(t, dir)

	stale := files[0].Path
	if err := os.WriteFile(stale, []byte("from an older version\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	syncTestCommands(t, dir)

	body, err := os.ReadFile(stale)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "from an older version") {
		t.Errorf("%s was not brought up to date:\n%s", filepath.Base(stale), body)
	}
}

func TestSyncCommandsWithoutADirectory(t *testing.T) {
	t.Parallel()

	files, err := SyncCommands("", CommandFileOptions{Clother: testClotherPath})
	if err != nil {
		t.Fatalf("SyncCommands error = %v, want nil for an unknown directory", err)
	}
	if len(files) != 0 {
		t.Errorf("SyncCommands wrote %d files with no directory to write to", len(files))
	}
}

// Without a way to invoke clother the files would be instructions to run a
// command that may not be on PATH, so this is refused rather than guessed.
func TestSyncCommandsWithoutAnInvocation(t *testing.T) {
	t.Parallel()

	if _, err := SyncCommands(t.TempDir(), CommandFileOptions{}); err == nil {
		t.Error("SyncCommands accepted an empty invocation")
	}
}

func TestRemoveCommandsRemovesOnlyWhatItWrote(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	files := syncTestCommands(t, dir)

	// One file edited by the user, and one file that was never ours.
	edited := files[1].Path
	if err := os.WriteFile(edited, []byte("mine now\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(filepath.Dir(files[0].Path), "notes.md")
	if err := os.WriteFile(foreign, []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	removed, kept := RemoveCommands(files)

	if len(removed) != len(files)-1 {
		t.Errorf("removed %d files, want %d: %q", len(removed), len(files)-1, removed)
	}
	if len(kept) != 1 || kept[0] != edited {
		t.Errorf("kept = %q, want only the edited %s", kept, filepath.Base(edited))
	}
	if _, err := os.Stat(edited); err != nil {
		t.Errorf("the edited file was deleted: %v", err)
	}
	// The directory holds a file that is not ours, so it has to survive.
	if _, err := os.Stat(foreign); err != nil {
		t.Errorf("a file clother did not write was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(foreign)); err != nil {
		t.Errorf("the directory was removed while it still held a file: %v", err)
	}
}

func TestRemoveCommandsRemovesTheDirectoryWhenItEmpties(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	files := syncTestCommands(t, dir)
	commandDir := filepath.Dir(files[0].Path)

	removed, kept := RemoveCommands(files)

	if len(kept) != 0 {
		t.Fatalf("kept = %q, want nothing", kept)
	}
	if len(removed) != len(files) {
		t.Fatalf("removed %d of %d files", len(removed), len(files))
	}
	if _, err := os.Stat(commandDir); !os.IsNotExist(err) {
		t.Errorf("the command directory survived at %s", commandDir)
	}
}

// Uninstall has to be able to run twice, and on a machine where the files were
// already deleted by hand.
func TestRemoveCommandsToleratesMissingFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	files := syncTestCommands(t, dir)
	for _, file := range files {
		if err := os.Remove(file.Path); err != nil {
			t.Fatal(err)
		}
	}

	removed, kept := RemoveCommands(files)

	if len(removed) != 0 || len(kept) != 0 {
		t.Errorf("RemoveCommands reported (%q, %q) for files that were already gone", removed, kept)
	}
	// A manifest with no commands at all is the ordinary state of an
	// installation that predates them.
	if removed, kept := RemoveCommands(nil); len(removed) != 0 || len(kept) != 0 {
		t.Errorf("RemoveCommands(nil) reported (%q, %q), want nothing", removed, kept)
	}
}

// The generated files are baked with a path, and the copy in the bin directory
// is the one that stays where it is: the release installer runs from a temporary
// directory that is deleted when it finishes.
func TestClotherInvocationPrefersTheInstalledBinary(t *testing.T) {
	t.Parallel()

	paths := testPaths(t.TempDir())

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if got := ClotherInvocation(paths); got != self {
		t.Errorf("ClotherInvocation() = %q with nothing installed, want the running binary %q", got, self)
	}

	installed := filepath.Join(paths.BinDir, platform.BinaryName())
	if err := os.MkdirAll(paths.BinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installed, []byte("stub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := ClotherInvocation(paths); got != installed {
		t.Errorf("ClotherInvocation() = %q, want the installed copy %q", got, installed)
	}
}
