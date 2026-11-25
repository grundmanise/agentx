---
status: accepted
date: 2026-11-25
---

# An agent reads its own skills folder and maybe the library

Some agents also read other agents' skills folders: Cursor reads `~/.claude/skills` and `~/.codex/skills`, and OpenCode reads `~/.claude/skills`. agentx modelled these reads, so one skill showed under several agents, and the desktop showed an Inherited chip. The rules differ per agent and change between versions, and agentx can only learn them by hand.

## Decision

- agentx models only each agent's own skills folder, plus one yes or no: does it read the library, `~/.agents/skills`. The vercel skills CLI uses the same model.
- An agent reads the library only when `readsTheLibrary` in `apps/cli/internal/scan/reads.go` names it. An agent whose own skills folder is a link to the library, such as `~/.claude/skills`, still uses its own folder: the link only decides how a skill appears there.
- A skill folder an agent reaches through its own folder and the library is one occurrence: the library entry, with the other path in `also_at`.

## Considered and rejected

- Keep a table of cross-agent reads: it goes stale, and every agent version can change it.
- Merge skills by name, as `skills list` does: two different skills can share a name.

## Consequences

- A skill only in another agent's folder does not show under the agent that reads that folder. To reach every agent, put the skill in the library.
- Plugins and MCP servers follow the same rule: Cursor loads Claude Code plugins, and agentx lists them under Claude Code only.
- Until a follow-up change lands, agentx still counts an agent whose skills folder links to the library as reading the library.
