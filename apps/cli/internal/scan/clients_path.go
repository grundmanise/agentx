package scan

import (
	"path/filepath"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// pathClient is a client known by its paths alone. config and skills are
// relative to the user's HOME, or to the XDG config home with an "xdg/"
// prefix; skills may be "library" for a client that reads the library as its
// user skills directory.
type pathClient struct {
	slug, name, config, skills, project string
}

func (p pathClient) Slug() string                 { return p.slug }
func (p pathClient) Name() string                 { return p.name }
func (p pathClient) ConfigDir(d home.Dirs) string { return p.at(d, p.config) }
func (p pathClient) SkillsDirs(d home.Dirs) []string {
	return []string{p.at(d, p.skills)}
}
func (p pathClient) ReadsLibrary() bool          { return p.skills == "library" }
func (p pathClient) ProjectSkillsDirs() []string { return []string{p.project} }

func (pathClient) at(d home.Dirs, spec string) string {
	if spec == "library" {
		return d.Library
	}
	if rel, ok := strings.CutPrefix(spec, "xdg/"); ok {
		return filepath.Join(d.Config, rel)
	}
	return filepath.Join(d.User, spec)
}

// pathClients is the agent list of the skills CLI (npm package skills 1.7.0)
// minus the six clients with their own file and minus the agents that are
// only detected from a project directory, which have no user-scope
// configuration: eve, promptscript, replit and the universal pseudo-agent.
var pathClients = []Client{
	pathClient{"adal", "AdaL", ".adal", ".adal/skills", ".adal/skills"},
	pathClient{"aider-desk", "AiderDesk", ".aider-desk", ".aider-desk/skills", ".aider-desk/skills"},
	pathClient{"amp", "Amp", "xdg/amp", "xdg/agents/skills", ".agents/skills"},
	pathClient{"antigravity", "Antigravity", ".gemini/antigravity", ".gemini/antigravity/skills", ".agents/skills"},
	pathClient{"antigravity-cli", "Antigravity CLI", ".gemini/antigravity-cli", ".gemini/antigravity-cli/skills", ".agents/skills"},
	pathClient{"astrbot", "AstrBot", ".astrbot", ".astrbot/data/skills", "data/skills"},
	pathClient{"augment", "Augment", ".augment", ".augment/skills", ".augment/skills"},
	pathClient{"autohand-code", "Autohand Code CLI", ".autohand", ".autohand/skills", ".autohand/skills"},
	pathClient{"bob", "IBM Bob", ".bob", ".bob/skills", ".bob/skills"},
	pathClient{"cline", "Cline", ".cline", "library", ".agents/skills"},
	pathClient{"codearts-agent", "CodeArts Agent", ".codeartsdoer", ".codeartsdoer/skills", ".codeartsdoer/skills"},
	pathClient{"codebuddy", "CodeBuddy", ".codebuddy", ".codebuddy/skills", ".codebuddy/skills"},
	pathClient{"codemaker", "Codemaker", ".codemaker", ".codemaker/skills", ".codemaker/skills"},
	pathClient{"codestudio", "Code Studio", ".codestudio", ".codestudio/skills", ".codestudio/skills"},
	pathClient{"command-code", "Command Code", ".commandcode", ".commandcode/skills", ".commandcode/skills"},
	pathClient{"continue", "Continue", ".continue", ".continue/skills", ".continue/skills"},
	pathClient{"cortex", "Cortex Code", ".snowflake/cortex", ".snowflake/cortex/skills", ".cortex/skills"},
	pathClient{"crush", "Crush", ".config/crush", ".config/crush/skills", ".crush/skills"},
	pathClient{"deepagents", "Deep Agents", ".deepagents", ".deepagents/agent/skills", ".agents/skills"},
	pathClient{"devin", "Devin for Terminal", "xdg/devin", "xdg/devin/skills", ".devin/skills"},
	pathClient{"dexto", "Dexto", ".dexto", "library", ".agents/skills"},
	pathClient{"droid", "Droid", ".factory", ".factory/skills", ".agents/skills"},
	pathClient{"firebender", "Firebender", ".firebender", ".firebender/skills", ".agents/skills"},
	pathClient{"forgecode", "ForgeCode", ".forge", ".forge/skills", ".forge/skills"},
	pathClient{"fx", "fx", ".fx", ".fx/skills", ".fx/skills"},
	pathClient{"goose", "Goose", "xdg/goose", "xdg/goose/skills", ".goose/skills"},
	pathClient{"grok", "Grok Build", ".grok", ".grok/skills", ".grok/skills"},
	pathClient{"hermes-agent", "Hermes Agent", ".hermes", ".hermes/skills", ".hermes/skills"},
	pathClient{"iflow-cli", "iFlow CLI", ".iflow", ".iflow/skills", ".iflow/skills"},
	pathClient{"inference-sh", "inference.sh", ".inferencesh", ".inferencesh/skills", ".inferencesh/skills"},
	pathClient{"jazz", "Jazz", ".jazz", ".jazz/skills", ".jazz/skills"},
	pathClient{"junie", "Junie", ".junie", ".junie/skills", ".junie/skills"},
	pathClient{"kilo", "Kilo Code", ".kilo", ".kilo/skills", ".agents/skills"},
	pathClient{"kimchi", "Kimchi", ".config/kimchi", ".config/kimchi/harness/skills", ".kimchi/skills"},
	pathClient{"kimi-code-cli", "Kimi Code CLI", ".kimi-code", "library", ".agents/skills"},
	pathClient{"kiro-cli", "Kiro CLI", ".kiro", ".kiro/skills", ".kiro/skills"},
	pathClient{"kode", "Kode", ".kode", ".kode/skills", ".kode/skills"},
	pathClient{"lingma", "Lingma", ".lingma", ".lingma/skills", ".lingma/skills"},
	pathClient{"loaf", "Loaf", ".loaf", "library", ".agents/skills"},
	pathClient{"mcpjam", "MCPJam", ".mcpjam", ".mcpjam/skills", ".mcpjam/skills"},
	pathClient{"minimax-code", "MiniMax Code", ".minimax", ".minimax/skills", ".minimax/skills"},
	pathClient{"mistral-vibe", "Mistral Vibe", ".vibe", ".vibe/skills", ".vibe/skills"},
	pathClient{"moxby", "Moxby", ".moxby", ".moxby/skills", ".moxby/skills"},
	pathClient{"mux", "Mux", ".mux", ".mux/skills", ".mux/skills"},
	pathClient{"neovate", "Neovate", ".neovate", ".neovate/skills", ".neovate/skills"},
	pathClient{"ona", "Ona", ".ona", ".ona/skills", ".ona/skills"},
	pathClient{"openclaw", "OpenClaw", ".openclaw", ".openclaw/skills", "skills"},
	pathClient{"opencode", "OpenCode", "xdg/opencode", "xdg/opencode/skills", ".agents/skills"},
	pathClient{"openhands", "OpenHands", ".openhands", ".openhands/skills", ".openhands/skills"},
	pathClient{"pi", "Pi", ".pi/agent", ".pi/agent/skills", ".pi/skills"},
	pathClient{"pochi", "Pochi", ".pochi", ".pochi/skills", ".pochi/skills"},
	pathClient{"posit-assistant", "Posit Assistant", ".posit/assistant", ".posit/assistant/skills", ".posit/assistant/skills"},
	pathClient{"qoder", "Qoder", ".qoder", ".qoder/skills", ".qoder/skills"},
	pathClient{"qoder-cn", "Qoder CN", ".qoder-cn", ".qoder-cn/skills", ".qoder/skills"},
	pathClient{"qwen-code", "Qwen Code", ".qwen", ".qwen/skills", ".qwen/skills"},
	pathClient{"reasonix", "Reasonix", ".reasonix", ".reasonix/skills", ".reasonix/skills"},
	pathClient{"roo", "Roo Code", ".roo", ".roo/skills", ".roo/skills"},
	pathClient{"rovodev", "Rovo Dev", ".rovodev", ".rovodev/skills", ".rovodev/skills"},
	pathClient{"sarvam-code", "Sarvam Code", ".sarvam", "library", ".agents/skills"},
	pathClient{"tabnine-cli", "Tabnine CLI", ".tabnine", ".tabnine/agent/skills", ".tabnine/agent/skills"},
	pathClient{"terramind", "Terramind", ".terramind", ".terramind/skills", ".terramind/skills"},
	pathClient{"tinycloud", "Tinycloud", ".tinycloud", ".tinycloud/skills", ".tinycloud/skills"},
	pathClient{"trae", "Trae", ".trae", ".trae/skills", ".trae/skills"},
	pathClient{"trae-cn", "Trae CN", ".trae-cn", ".trae-cn/skills", ".trae/skills"},
	pathClient{"warp", "Warp", ".warp", "library", ".agents/skills"},
	pathClient{"zcode", "ZCode", ".zcode", ".zcode/skills", ".zcode/skills"},
	pathClient{"zed", "Zed", "xdg/zed", "library", ".agents/skills"},
	pathClient{"zencoder", "Zencoder", ".zencoder", ".zencoder/skills", ".zencoder/skills"},
	pathClient{"zenflow", "Zenflow", ".zencoder", ".zencoder/skills", ".zencoder/skills"},
}
