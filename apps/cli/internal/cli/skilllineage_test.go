package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// nastySubpaths are skill directories a source may perfectly well hold and
// an import commit cannot record: the subpath is one line of a trailer, and
// the reader takes that line with the whitespace around it trimmed off. A
// control character is a line the reader refuses outright; a newline is a
// line that ends early, so the trailer would be read as the directory above;
// a leading or trailing space is a line read back as a neighbouring
// directory, without anything going wrong at all.
var nastySubpaths = []struct {
	name    string // the frontmatter name, which is the library directory
	subpath string
}{
	{"escaped", "na\x1b[31msty"},
	{"broken", "tools/od\nd"},
	{"opens", " leading"},
	{"closes", "trailing "},
}

// TestInstallRefusesASubpathItCannotRecord is the install side of the rule
// the trailer reader keeps. Installing such a skill used to succeed and
// then list as managed with no source, no subpath, no upstream commit and
// no state, or with a subpath naming another directory of the same source,
// and nothing said so: the import commit exists to be the base version
// every later update, revert and fork merge works from, and one no reader
// accepts leaves a skill that can never be updated or reverted again.
func TestInstallRefusesASubpathItCannotRecord(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("subpaths", true)
	for _, c := range nastySubpaths {
		s.skill(c.subpath, c.name, "A skill under a directory no trailer carries", nil)
	}
	s.skill("plain", "plain", "A skill under a directory a trailer carries", nil)
	s.commit("skill directories a trailer cannot carry")
	if out := h.run("source", "add", s.url); out.exit != 0 {
		t.Fatalf("source add: exit %d\n%s", out.exit, out.stderr)
	}

	for _, c := range nastySubpaths {
		t.Run(c.name, func(t *testing.T) {
			out := h.run("--json", "skill", "add", s.url, "--skill", c.name)
			equal(t, "exit", out.exit, exitRefused.exit)
			ev := lastError(t, h.events(out.stdout))
			contains(t, "the refusal", ev["message"].(string), "is not a directory the account repo can record")

			// Nothing was written for it: no import branch, no library
			// directory, and so nothing for a later command to trip over.
			if _, err := h.accountGitErr("rev-parse", "--verify", "refs/heads/managed/"+c.name); err == nil {
				t.Errorf("%s has an import branch after a refused install", c.name)
			}
			if _, err := os.Lstat(filepath.Join(h.library, c.name)); err == nil {
				t.Errorf("%s has a library directory after a refused install", c.name)
			}
		})
	}
}

// TestARefusedSubpathCostsOnlyItsOwnSkill holds the refusal to the rule
// every other one of an install keeps: a skill that cannot be installed
// costs itself and not the run.
func TestARefusedSubpathCostsOnlyItsOwnSkill(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("subpaths", true)
	for _, c := range nastySubpaths {
		s.skill(c.subpath, c.name, "A skill under a directory no trailer carries", nil)
	}
	s.skill("plain", "plain", "A skill under a directory a trailer carries", nil)
	s.commit("skill directories a trailer cannot carry")
	if out := h.run("source", "add", s.url); out.exit != 0 {
		t.Fatalf("source add: exit %d\n%s", out.exit, out.stderr)
	}

	out := h.run("skill", "add", s.url, "--all")
	equal(t, "exit", out.exit, exitRefused.exit)
	for _, c := range nastySubpaths {
		contains(t, "the warnings", out.stderr, c.name+": ")
	}
	if _, err := os.Stat(filepath.Join(h.library, "plain", "SKILL.md")); err != nil {
		t.Fatalf("the skill the run could record was not installed: %v", err)
	}

	// What the run did install reads back whole: the account repo records
	// the lineage and the listing gives it again, which is the agreement
	// the refusal exists to keep.
	list := h.run("--json", "skill", "list")
	equal(t, "exit", list.exit, 0)
	ev := h.one(list.stdout, "library_skill")
	equal(t, "name", ev["name"], "plain")
	equal(t, "kind", ev["kind"], "managed")
	equal(t, "source", ev["source"], s.url)
	equal(t, "subpath", ev["subpath"], "plain")
	equal(t, "state", ev["state"], stateCurrent)
	if ev["upstream_commit"] == nil || ev["base_hash"] == nil {
		t.Errorf("the skill lists without its lineage: %v", ev)
	}
}

// TestAdoptRefusesASubpathItCannotRecord is the same rule on the other
// route that writes import commits. The lock file of another tool names the
// directory, and that tool has no trailer to fit it on one line of, so it
// may perfectly well name one agentx cannot record.
func TestAdoptRefusesASubpathItCannotRecord(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("padded", true)
	// A directory whose name ends in a space, holding a real skill: the
	// lineage of it would be written as a trailer the reader hands back
	// trimmed, naming a directory this source does not have.
	const padded = "skills/alpha "
	s.skill(padded, "alpha", "The first skill", map[string]string{"notes.md": "alpha notes\n"})
	v1 := s.commit("the version the other tool installed")
	vercelInstall(t, h, s, padded, "alpha")
	lock := h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
		Source: "owner/repo", SourceType: "github", SourceURL: s.url,
		SkillPath: padded, SkillFolderHash: s.treeAt(v1, padded),
		InstalledAt: "2026-01-01T00:00:00.000Z", UpdatedAt: "2026-01-01T00:00:00.000Z",
	}})

	out := h.run("--json", "adopt", "--all")
	equal(t, "exit", out.exit, exitRefused.exit)
	contains(t, "the refusal", lastError(t, h.events(out.stdout))["message"].(string), "is not a directory of "+s.url)
	h.lockUnchanged(h.lockPath(), lock)
	if _, err := h.accountGitErr("rev-parse", "--verify", "refs/heads/managed/alpha"); err == nil {
		t.Error("alpha has an import branch after a refused adoption")
	}
}

// TestTheRefusalNamesTheDirectoryAsItIs checks that the message shows what
// it is about. A path is the one thing a reader has to see exactly: the
// space around a name is invisible on a terminal and a control character is
// obeyed by one, so the directory is quoted rather than printed, and the
// quoting is what puts the padding and the escape on the page.
func TestTheRefusalNamesTheDirectoryAsItIs(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("quoted", true)
	s.skill(" leading", "opens", "A skill under a padded directory", nil)
	s.skill("na\x1b[31msty", "escaped", "A skill under an escaped directory", nil)
	s.commit("directories a message has to quote")
	if out := h.run("source", "add", s.url); out.exit != 0 {
		t.Fatalf("source add: exit %d\n%s", out.exit, out.stderr)
	}
	for _, c := range []struct{ skill, quoted string }{
		{"opens", `" leading"`},
		{"escaped", `"na\x1b[31msty"`},
	} {
		out := h.run("skill", "add", s.url, "--skill", c.skill)
		equal(t, "exit", out.exit, exitRefused.exit)
		contains(t, "the refusal", out.stderr, c.quoted)
		// Nothing is painted into a pipe, so an escape on this stream
		// could only be one the source wrote.
		if strings.ContainsRune(out.stderr, 0x1b) {
			t.Errorf("the refusal carries an escape of the source's: %q", out.stderr)
		}
	}
}

// TestTheDirectoryIsRefusedBeforeTheEntriesUnderIt holds the order of the
// two path rules of an install: the skill's directory is held to the
// trailer reader first, and only a directory that passes has its entries
// checked, so the refusal of a directory the import commit cannot record
// is the one given even when an entry below it would be refused too, and
// the refusal of an entry never names a directory it cannot print.
func TestTheDirectoryIsRefusedBeforeTheEntriesUnderIt(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("both", true)
	s.skill("skills/evil ", "evil", "A padded directory with an entry that climbs out", nil)
	s.commit("a padded skill")
	// A ".." entry under the padded directory, which a checkout never
	// writes and git mktree takes.
	evil := s.mktree(append(strings.Split(s.bare("ls-tree", "HEAD:skills/evil "), "\n"), "040000 tree "+s.tree("skills/evil ")+"\t..")...)
	skills := s.mktree(s.replaced("HEAD:skills", "evil ", evil)...)
	root := s.mktree(s.replaced("HEAD^{tree}", "skills", skills)...)
	s.bare("update-ref", "refs/heads/main", s.bare("commit-tree", root, "-p", "HEAD", "-m", "an entry that climbs out"))
	contains(t, "the source tree", s.bare("ls-tree", "-r", "HEAD"), "skills/evil /../SKILL.md")
	equal(t, "source add", h.run("source", "add", s.url).exit, 0)

	out := h.run("skill", "add", s.url, "--skill", "evil")
	equal(t, "exit", out.exit, exitRefused.exit)
	contains(t, "the refusal", out.stderr, `evil comes from "skills/evil ", which is not a directory the account repo can record`)
	if strings.Contains(out.stderr, "will not lay out") {
		t.Errorf("the entry was refused before the directory:\n%s", out.stderr)
	}
}
