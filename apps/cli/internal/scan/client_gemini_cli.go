package scan

import (
	"path/filepath"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

type geminiCLI struct{}

func (geminiCLI) Slug() string                 { return "gemini-cli" }
func (geminiCLI) Name() string                 { return "Gemini CLI" }
func (geminiCLI) ConfigDir(d home.Dirs) string { return filepath.Join(d.User, ".gemini") }
func (c geminiCLI) SkillsDirs(d home.Dirs) []string {
	return []string{filepath.Join(c.ConfigDir(d), "skills"), d.Library}
}
func (geminiCLI) ProjectSkillsDirs() []string { return []string{".gemini/skills", ".agents/skills"} }
