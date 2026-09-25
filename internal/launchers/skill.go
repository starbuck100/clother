package launchers

import (
	_ "embed"
	"path/filepath"
	"strings"
)

//go:embed skills/clother-free-fallback/SKILL.md
var freeFallbackSkill string

// FreeFallbackSkill supplies the same decision instructions to the startup planner.
func FreeFallbackSkill() string { return freeFallbackSkill }

func SyncFreeSkill(dir, clother string) (GeneratedFile, error) {
	path := filepath.Join(dir, "skills", "clother-free-fallback", "SKILL.md")
	body := strings.ReplaceAll(freeFallbackSkill, "{{CLOTHER}}", invoke(clother))
	if err := writeIfChanged(path, body); err != nil {
		return GeneratedFile{}, err
	}
	return GeneratedFile{Path: path, SHA256: hashOf(body)}, nil
}
