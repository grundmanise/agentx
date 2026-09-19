package scan

import (
	"path/filepath"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

type cursor struct{}

func (cursor) Slug() string                 { return "cursor" }
func (cursor) Name() string                 { return "Cursor" }
func (cursor) ConfigDir(d home.Dirs) string { return filepath.Join(d.User, ".cursor") }

// SkillsDirs: Cursor also reads the Claude Code and Codex user skills directories.
func (c cursor) SkillsDirs(d home.Dirs) []string {
	return []string{
		filepath.Join(c.ConfigDir(d), "skills"),
		filepath.Join(claudeCode{}.ConfigDir(d), "skills"),
		filepath.Join(codex{}.ConfigDir(d), "skills"),
	}
}
func (cursor) ProjectSkillsDirs() []string {
	return []string{".cursor/skills", ".claude/skills", ".codex/skills", ".agents/skills"}
}
