package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
)

// bothToolsHarness is a machine both tools installed skills on, each from
// a source of its own that agentx has not added: alpha by the Vercel
// skills CLI and beta by Skillshare. agentx home holds an empty store,
// whose creation is a change of its own. It returns the two sources.
func bothToolsHarness(t *testing.T) (h *harness, vercel, shared *sourceRepo) {
	t.Helper()
	h = newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	if _, _, err := gitx.OpenStore(context.Background(), h.invocation().git, h.agentx); err != nil {
		t.Fatal(err)
	}
	vercel = h.newSourceRepo("skills", true)
	vercel.skill("skills/alpha", "alpha", "The first skill", nil)
	vercel.commit("one skill")
	vercelInstall(t, h, vercel, "skills/alpha", "alpha")
	shared = h.newSourceRepo("team", true)
	shared.skill("skills/beta", "beta", "The second skill", map[string]string{"notes.md": "beta notes\n"})
	shared.commit("one skill")
	skillshareInstall(t, h, shared, "skills/beta", "beta")
	h.writeStore(h.skillshareSkills(), map[string]storeEntry{
		"beta": {Source: "github.com/owner/team/skills/beta", Type: "github-subdir", RepoURL: shared.url, Subdir: "skills/beta"},
	})
	return h, vercel, shared
}

// adoptionsOf is the adoption events of a run, each as "name tool state".
func adoptionsOf(h *harness, stdout string) string {
	var rows []string
	for _, ev := range h.eventsOfType(stdout, "adoption") {
		rows = append(rows, ev["name"].(string)+" "+ev["tool"].(string)+" "+ev["state"].(string))
	}
	return strings.Join(rows, ", ")
}

// TestAdoptFromBothToolsIsOneChange gives --from once for each tool: one
// run adopts the skills of both, the one installed from a source and the
// one adopted with its files as they are, adds both sources, and lists the
// skill it cannot adopt without that costing the skills after it. All of
// it is behind one change signal and reported from one rescan, and a skill
// no tool recorded is left as it is.
func TestAdoptFromBothToolsIsOneChange(t *testing.T) {
	t.Parallel()
	h, vercel, _ := bothToolsHarness(t)
	nowhere := lockEntry{Source: "example.com", SourceType: "well-known", SourceURL: "https://example.com/skills"}
	h.writeLock(h.lockPath(), map[string]lockEntry{
		"alpha":   {Source: "owner/repo", SourceType: "github", SourceURL: vercel.url, SkillPath: "skills/alpha"},
		"aliased": nowhere,
		"kept":    nowhere,
	})
	for _, name := range []string{"kept", "notes"} {
		writeFile(t, mkdirs(t, filepath.Join(h.library, name), "SKILL.md"), skill(name, "A skill with nothing to install it from"))
	}
	if err := os.Symlink(filepath.Join(h.library, "kept"), filepath.Join(h.library, "aliased")); err != nil {
		t.Fatal(err)
	}

	version := readVersion(t, h)

	out := h.run("--json", "skill", "import", "--from", "skills-cli", "--from", "skillshare", "--all")

	equal(t, "exit", out.exit, 0)
	equal(t, "version", readVersion(t, h), version+1)
	equal(t, "the adoption events", adoptionsOf(h, out.stdout),
		"aliased vercel refused, alpha vercel adopted, beta skillshare adopted, kept vercel adopted")
	equal(t, "summary", h.one(out.stdout, "result")["summary"], "imported 3 skills; 1 could not be imported")
	rescans := 0
	for _, ev := range h.eventsOfType(out.stdout, "progress") {
		if ev["phase"] == phaseRescan {
			rescans++
		}
	}
	equal(t, "rescans", rescans, 1)
	equal(t, "sources", len(readSettingsFile(t, h)["sources"].([]any)), 2)
	equal(t, "branches", h.storeGit("for-each-ref", "--format=%(refname:short)", "refs/heads/"), "managed/alpha\nmanaged/beta\nskills/kept")
}

// TestAdoptFromBothToolsWhenOneSourceIsGone: the source of the skill the
// Vercel skills CLI installed cannot be fetched, which costs that skill and
// fails the run. The skill Skillshare installed is adopted all the same,
// behind the one change signal of the run.
func TestAdoptFromBothToolsWhenOneSourceIsGone(t *testing.T) {
	t.Parallel()
	h, vercel, shared := bothToolsHarness(t)
	h.writeLock(h.lockPath(), map[string]lockEntry{
		"alpha": {Source: "owner/repo", SourceType: "github", SourceURL: vercel.url + "-gone", SkillPath: "skills/alpha"},
	})

	version := readVersion(t, h)

	out := h.run("--json", "skill", "import", "--from", "skills-cli", "--from", "skillshare", "--all")

	equal(t, "exit", out.exit, exitSource.exit)
	equal(t, "version", readVersion(t, h), version+1)
	equal(t, "the adoption events", adoptionsOf(h, out.stdout), "alpha vercel refused, beta skillshare adopted")
	sources := readSettingsFile(t, h)["sources"].([]any)
	equal(t, "sources", len(sources), 1)
	equal(t, "the source", sources[0].(map[string]any)["url"], shared.url)
	equal(t, "remotes", len(strings.Fields(h.storeGit("remote"))), 1)
}

// TestAdoptFromBothToolsWhenAnAddedSourceIsGone: the source of the skill
// Skillshare installed was added and has gone since, so the files of that
// skill cannot be fetched, which costs it and fails the run. The skill the
// Vercel skills CLI installed is adopted all the same.
func TestAdoptFromBothToolsWhenAnAddedSourceIsGone(t *testing.T) {
	t.Parallel()
	h, vercel, shared := bothToolsHarness(t)
	h.writeLock(h.lockPath(), map[string]lockEntry{
		"alpha": {Source: "owner/repo", SourceType: "github", SourceURL: vercel.url, SkillPath: "skills/alpha"},
	})
	h.mustRun("source", "add", shared.url)
	if err := os.RemoveAll(shared.gitDir); err != nil {
		t.Fatal(err)
	}
	version := readVersion(t, h)

	out := h.run("--json", "skill", "import", "--from", "skills-cli", "--from", "skillshare", "--all")

	equal(t, "exit", out.exit, exitSource.exit)
	equal(t, "version", readVersion(t, h), version+1)
	equal(t, "the adoption events", adoptionsOf(h, out.stdout), "alpha vercel adopted, beta skillshare refused")
	equal(t, "branches", h.storeGit("for-each-ref", "--format=%(refname:short)", "refs/heads/"), "managed/alpha")
}
