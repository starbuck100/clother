package platform

import (
	"os"
	"path/filepath"
	"testing"
)

// Many of the assertions below are written against exeExt, envCaseSensitive and
// sizeTiebreak — the in-package constants that differ per platform — so they
// state the same rule on both platforms and compile on both, rather than being
// skipped into meaninglessness on one of them.

func TestInstallationNamesCarryThePlatformExtension(t *testing.T) {
	t.Parallel()

	cases := []struct {
		got  string
		want string
	}{
		{BinaryName(), "clother" + exeExt},
		{ClaudeName(), "claude" + exeExt},
		{RealClaudeName(), "claude-real" + exeExt},
		{LauncherName("zai"), "clother-zai" + exeExt},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("name = %q, want %q", tc.got, tc.want)
		}
	}
}

func TestInvocationNameIgnoresThePlatformExtension(t *testing.T) {
	t.Parallel()

	// argv0 arrives with the extension the shell used. Stripping it is what
	// keeps `clother-zai` and `clother-zai.exe` the same profile.
	if got := InvocationName("clother-zai" + exeExt); got != "clother-zai" {
		t.Fatalf("InvocationName() = %q, want clother-zai", got)
	}
	if got := InvocationName(filepath.Join("some", "dir", "clother-zai"+exeExt)); got != "clother-zai" {
		t.Fatalf("InvocationName() = %q, want clother-zai", got)
	}
	if IsClaudeName("claude" + exeExt) != true {
		t.Fatal("IsClaudeName(claude) = false, want true")
	}
	if IsClaudeName("clother-zai" + exeExt) {
		t.Fatal("IsClaudeName(clother-zai) = true, want false")
	}
	if IsClaudeName("claude-real" + exeExt) {
		t.Fatal("IsClaudeName(claude-real) = true, want false")
	}
}

func TestInvocationNameFallsBackToTheExecutable(t *testing.T) {
	t.Parallel()

	// argv0 is empty for a process started without one; the name still has to
	// be resolved, or every launcher would fall through to the shim.
	if got := InvocationName(""); got == "" {
		t.Fatal("InvocationName(\"\") is empty; it must fall back to the running executable")
	}
}

func TestInvocationNameKeepsNonPlatformExtensions(t *testing.T) {
	t.Parallel()

	if IsWindows {
		return
	}
	// On Unix a dot is an ordinary part of a file name; only the platform's own
	// launcher extensions may be stripped.
	if got := InvocationName("clother-zai.exe"); got != "clother-zai.exe" {
		t.Fatalf("InvocationName() = %q, want clother-zai.exe", got)
	}
}

// Not parallel: it sets PATHEXT, which is process-wide.
func TestExecutableNames(t *testing.T) {
	// Pinned so the expectation does not depend on the machine's PATHEXT.
	t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")

	got := ExecutableNames("claude")
	if len(got) == 0 {
		t.Fatal("ExecutableNames() returned nothing")
	}
	// The bare name is always tried last, so an extensionless install still
	// resolves on Unix and as a last resort on Windows.
	if last := got[len(got)-1]; last != "claude" {
		t.Fatalf("last name = %q, want claude", last)
	}
	if IsWindows {
		// Without the extension the Windows loader refuses to start the file,
		// which is why a bare "claude" lookup misses an npm claude.cmd.
		want := []string{"claude.COM", "claude.EXE", "claude.BAT", "claude.CMD", "claude"}
		if len(got) != len(want) {
			t.Fatalf("ExecutableNames() = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("ExecutableNames() = %v, want %v", got, want)
			}
		}
	} else if len(got) != 1 {
		t.Fatalf("ExecutableNames() = %v, want exactly [claude]", got)
	}
}

func TestHasEnvPrefixFollowsThePlatform(t *testing.T) {
	t.Parallel()

	cases := []struct {
		key  string
		want bool
	}{
		{"ANTHROPIC_API_KEY", true},
		{"anthropic_api_key", !envCaseSensitive},
		{"Anthropic_Base_Url", !envCaseSensitive},
		{"ANTHROPIC", false},
		{"CLAUDE_CODE_SUBAGENT_MODEL", false},
	}
	for _, tc := range cases {
		if got := HasEnvPrefix(tc.key, "ANTHROPIC_"); got != tc.want {
			t.Errorf("HasEnvPrefix(%q) = %v, want %v", tc.key, got, tc.want)
		}
	}
	// A prefix longer than the key must not panic on the slice.
	if HasEnvPrefix("A", "ANTHROPIC_") {
		t.Error("HasEnvPrefix(\"A\", \"ANTHROPIC_\") = true, want false")
	}
}

func TestSameInstallationSeesThroughLinks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(root, "clother"+exeExt)
	blob := make([]byte, 2<<20)
	if err := os.WriteFile(target, blob, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "claude"+exeExt)
	if err := LinkOrCopy(target, link); err != nil {
		t.Fatal(err)
	}
	if !SameInstallation(link, target) {
		t.Fatalf("%s and %s were linked but not recognised as one installation", link, target)
	}
}

func TestSameInstallationOnACopy(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(root, "clother"+exeExt)
	blob := make([]byte, 2<<20)
	if err := os.WriteFile(target, blob, 0o755); err != nil {
		t.Fatal(err)
	}
	copied := filepath.Join(root, "claude"+exeExt)
	if err := CopyFile(target, copied); err != nil {
		t.Fatal(err)
	}

	// A copy shares no file identity. On Windows it is still clother's own
	// binary — the shim is installed as a copy there — and the size tiebreak is
	// what says so. On Unix nothing is ever installed as a copy, so the tiebreak
	// is off and a copy is a genuinely different file.
	if got, want := SameInstallation(copied, target), sizeTiebreak; got != want {
		t.Fatalf("SameInstallation(copy) = %v, want %v", got, want)
	}

	// The tiebreak must not fire on files small enough to collide by accident.
	small := filepath.Join(root, "small"+exeExt)
	if err := os.WriteFile(small, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	smallCopy := filepath.Join(root, "small-copy"+exeExt)
	if err := os.WriteFile(smallCopy, []byte("y"), 0o755); err != nil {
		t.Fatal(err)
	}
	if SameInstallation(small, smallCopy) {
		t.Fatal("two one-byte files were treated as one installation")
	}
}

func TestSameInstallationOnMissingPaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if SameInstallation(filepath.Join(root, "nope"), filepath.Join(root, "nope2")) {
		t.Fatal("two missing paths were treated as one installation")
	}
	if SameInstallation("", filepath.Join(root, "nope")) {
		t.Fatal("an empty path was treated as an installation")
	}
}

func TestAtomicWriteReplacesContent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "state.json")
	if err := AtomicWrite(path, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWrite(path, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "second" {
		t.Fatalf("content = %q, want second", string(got))
	}

	// The temporary file must not be left behind next to the result.
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("directory holds %v, want only the target file", names)
	}
}

func TestRemoveExecutable(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "clother"+exeExt)
	if err := os.WriteFile(path, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	gone, err := RemoveExecutable(path)
	if err != nil || !gone {
		t.Fatalf("RemoveExecutable() = %v, %v; want true, nil", gone, err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("%s still exists, stat err=%v", path, err)
	}

	// Removing something that is already gone is a success, not an error: it is
	// the state the caller asked for.
	gone, err = RemoveExecutable(path)
	if err != nil || !gone {
		t.Fatalf("RemoveExecutable() on a missing file = %v, %v; want true, nil", gone, err)
	}
}
