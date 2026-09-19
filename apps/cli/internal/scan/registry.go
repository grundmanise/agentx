package scan

// registry lists every client agentx knows. The first six have fixtures in
// the tests; the rest are path data taken from the skills CLI's agent list.
var registry = append([]Client{
	claudeCode{},
	codex{},
	cursor{},
	geminiCLI{},
	windsurf{},
	githubCopilot{},
}, pathClients...)
