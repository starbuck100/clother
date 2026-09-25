package launchers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFreeSkillInstallAndSafeRemoval(t *testing.T) {
	dir := t.TempDir()
	file, err := SyncFreeSkill(dir, testClotherPath)
	if err != nil {
		t.Fatal(err)
	}
	if file.Path != filepath.Join(dir, "skills", "clother-free-fallback", "SKILL.md") {
		t.Fatal("skill not discoverable")
	}
	body, err := os.ReadFile(file.Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "{{CLOTHER}}") || !strings.Contains(string(body), testClotherPath+" usage next") {
		t.Fatal("skill cannot invoke installed binary")
	}
	if err := os.WriteFile(file.Path, append(body, []byte("\nUser notes\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	removed, kept := RemoveCommands([]GeneratedFile{file})
	if len(removed) != 0 || len(kept) != 1 {
		t.Fatal("user-edited skill removed")
	}
	file, err = SyncFreeSkill(dir, testClotherPath)
	if err != nil {
		t.Fatal(err)
	}
	removed, kept = RemoveCommands([]GeneratedFile{file})
	if len(removed) != 1 || len(kept) != 0 {
		t.Fatal("owned skill not removed")
	}
}
