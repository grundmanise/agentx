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

// Registered is the number of clients the registry knows.
func Registered() int { return len(registry) }

// Slugs are the ids of every registered client, in registry order. It is
// what a caller checks its own rule about configuration ids against: the
// settings hold these ids, and a rule about their shape has to be one
// every client in here satisfies.
func Slugs() []string {
	slugs := make([]string, 0, len(registry))
	for _, c := range registry {
		slugs = append(slugs, c.Slug())
	}
	return slugs
}
