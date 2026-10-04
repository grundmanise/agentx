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

// pluginForkNotes is what each client is known to do when the library holds
// a skill of the same name as one of its plugins' skills, which a fork of a
// plugin's skill makes so: which of the two its model sees.
var pluginForkNotes = map[string]string{
	"claude-code": "the fork owns the bare name and the plugin's copy stays reachable under the plugin's prefix, so the model sees both",
	"codex":       "both load, the plugin's copy under the plugin's namespace",
	"gemini-cli":  "the fork replaces the plugin's copy",
	"cursor":      "which of the two copies wins is not documented",
}

// PluginForkNote is how the client with the slug treats a fork of a
// plugin's skill beside the plugin's own copy, which agentx leaves in
// place, and "" for a client nothing is known of.
func PluginForkNote(slug string) string { return pluginForkNotes[slug] }
