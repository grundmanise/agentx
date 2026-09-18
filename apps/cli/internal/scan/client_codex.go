package scan

import (
	"path/filepath"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

type codex struct{}

func (codex) Slug() string                 { return "codex" }
func (codex) Name() string                 { return "Codex" }
func (codex) ConfigDir(d home.Dirs) string { return filepath.Join(d.User, ".codex") }
func (c codex) SkillsDirs(d home.Dirs) []string {
	return []string{filepath.Join(c.ConfigDir(d), "skills"), d.Library}
}
func (codex) ProjectSkillsDirs() []string { return []string{".codex/skills", ".agents/skills"} }
