// Where each agent keeps its configuration and its own skills, relative to HOME, as agentx's
// registry (apps/cli/internal/scan) reads them with XDG_CONFIG_HOME and COPILOT_HOME unset.
// agentx detects an agent when its configuration folder exists.

export interface AgentDirs {
  config: string;
  skills: string;
}

const own = (config: string): AgentDirs => ({
  config,
  skills: `${config}/skills`,
});

export const agents = {
  adal: own(".adal"),
  "aider-desk": own(".aider-desk"),
  antigravity: own(".gemini/antigravity"),
  augment: own(".augment"),
  bob: own(".bob"),
  "claude-code": own(".claude"),
  cline: own(".cline"),
  codebuddy: own(".codebuddy"),
  codex: own(".codex"),
  "command-code": own(".commandcode"),
  continue: own(".continue"),
  crush: own(".config/crush"),
  cursor: own(".cursor"),
  devin: own(".config/devin"),
  droid: own(".factory"),
  forgecode: own(".forge"),
  "gemini-cli": own(".gemini"),
  "github-copilot": own(".copilot"),
  goose: own(".config/goose"),
  grok: own(".grok"),
  "hermes-agent": own(".hermes"),
  "iflow-cli": own(".iflow"),
  junie: own(".junie"),
  kilo: own(".kilo"),
  "kiro-cli": own(".kiro"),
  kode: own(".kode"),
  lingma: own(".lingma"),
  "mistral-vibe": own(".vibe"),
  mux: own(".mux"),
  neovate: own(".neovate"),
  ona: own(".ona"),
  opencode: own(".config/opencode"),
  openhands: own(".openhands"),
  pochi: own(".pochi"),
  qoder: own(".qoder"),
  "qwen-code": own(".qwen"),
  roo: own(".roo"),
  rovodev: own(".rovodev"),
  trae: own(".trae"),
  zencoder: own(".zencoder"),
} as const satisfies Record<string, AgentDirs>;

export type AgentId = keyof typeof agents;

/** The ten first-class agents a typical machine has. */
export const firstClass: AgentId[] = [
  "claude-code",
  "github-copilot",
  "codex",
  "cursor",
  "antigravity",
  "gemini-cli",
  "opencode",
  "junie",
  "devin",
  "cline",
];

const isAgentId = (id: string): id is AgentId => Object.hasOwn(agents, id);

export const agentIds: AgentId[] = Object.keys(agents).filter(isAgentId);
