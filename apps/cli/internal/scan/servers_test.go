package scan

import (
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/mcp"
)

// A tool's identity is its server, kind and key, whatever its definition,
// and is computed afresh for a stored entry an earlier agentx keyed by its
// hash.
func TestKeyedToolIdentity(t *testing.T) {
	t.Parallel()
	logical := id("server", "url", "https://mcp.example.com/mcp")
	got := keyed(logical, []home.Tool{
		{ID: "tool:stale", Kind: mcp.KindTool, Name: "search", Description: "Search the web", Hash: "1"},
		{Kind: mcp.KindTool, Name: "search", Description: "a second definition under one name", Hash: "2"},
		{Kind: mcp.KindPrompt, Name: "search"},
		{Kind: mcp.KindResource, Name: "notes", URI: "file:///a.md"},
		{Kind: mcp.KindResource, Name: "notes", URI: "file:///b.md"},
		{Kind: mcp.KindResource, Name: "legacy"},
	})
	want := []struct{ id, hash string }{
		{id("tool", logical, mcp.KindTool, "search"), "1"},
		{id("tool", logical, mcp.KindPrompt, "search"), ""},
		{id("tool", logical, mcp.KindResource, "file:///a.md"), ""},
		{id("tool", logical, mcp.KindResource, "file:///b.md"), ""},
		{id("tool", logical, mcp.KindResource, "legacy"), ""},
	}
	if len(got) != len(want) {
		t.Fatalf("keyed = %d tools, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].ID != w.id || got[i].Hash != w.hash {
			t.Errorf("tool %d = %s with hash %q, want %s with hash %q", i, got[i].ID, got[i].Hash, w.id, w.hash)
		}
	}
	// The same server elsewhere gives the same ids; another server, others.
	if again := keyed(logical, []home.Tool{{Kind: mcp.KindTool, Name: "search", Hash: "3"}}); again[0].ID != want[0].id {
		t.Errorf("a new definition changed the id: %s", again[0].ID)
	}
	if other := keyed(id("server", "npm", "pkg"), []home.Tool{{Kind: mcp.KindTool, Name: "search"}}); other[0].ID == want[0].id {
		t.Errorf("two servers share a tool id: %s", other[0].ID)
	}
}
