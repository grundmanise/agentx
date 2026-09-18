package scan

import (
	"path/filepath"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

type githubCopilot struct{}

func (githubCopilot) Slug() string                 { return "github-copilot" }
func (githubCopilot) Name() string                 { return "GitHub Copilot" }
func (githubCopilot) ConfigDir(d home.Dirs) string { return filepath.Join(d.User, ".copilot") }
func (c githubCopilot) SkillsDirs(d home.Dirs) []string {
	return []string{filepath.Join(c.ConfigDir(d), "skills")}
}
func (githubCopilot) ProjectSkillsDirs() []string {
	return []string{".github/skills", ".agents/skills"}
}
