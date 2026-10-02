package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
)

// TestSkillForkConvertsManagedSkills forks two managed skills in their
// place, one as it was installed and one edited beside a file the system
// file list names. Each import branch becomes the fork's: the tip is a
// creation commit of its own, with a new fork id, whose parent is the
// import and whose tree is the import's for the unedited skill and holds
// exactly the edit for the other. The library directory moved, the ignored
// file with it, into the worktree, whose status is clean, and the library
// holds a relative symlink to it; every placement is as it was, no client
// that reads the library got a symlink, the skill directory holds no .git,
// and the fork reads current, with the upstream it came from.
// The update candidate a check pinned stays; the upstream-removed marker
// goes.
func TestSkillForkConvertsManagedSkills(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha", "--skill", "beta")
	imports := map[string]string{"alpha": h.ref(lineage.ManagedRef("alpha")), "beta": h.ref(lineage.ManagedRef("beta"))}
	h.accountGit("update-ref", lineage.CandidateRef("alpha"), imports["beta"])
	h.accountGit("update-ref", lineage.UpstreamRemovedRef("alpha"), imports["beta"])
	writeFile(t, filepath.Join(h.library, "beta", "SKILL.md"), skill("beta", "The second skill, edited"))
	writeFile(t, filepath.Join(h.library, "beta", ".DS_Store"), "finder\n")
	placed := func(client, name string) string {
		target, _ := os.Readlink(filepath.Join(h.home, client, "skills", name))
		return target
	}
	for _, name := range []string{"alpha", "beta"} {
		before := h.listed(name)
		out := h.run("--json", "skill", "fork", name)
		equal(t, name+": exit", out.exit, 0)
		ev := h.librarySkill(out.stdout, name)
		equal(t, name+": kind", ev["kind"], lineage.KindFork)
		equal(t, name+": state", ev["state"], stateCurrent)
		for _, key := range []string{"source", "subpath", "upstream_commit", "base_hash"} {
			equal(t, name+": "+key, ev[key], before[key])
		}
		if _, err := h.accountGitErr("rev-parse", "--verify", "-q", lineage.ManagedRef(name)); err == nil {
			t.Errorf("%s: the import branch is still there", name)
		}
		tip := h.ref(lineage.ForkRef(name))
		equal(t, name+": the parent", h.accountGit("rev-list", "--parents", "-1", tip), tip+" "+imports[name])
		equal(t, name+": the fork id", ev["fork_id"], h.trailer(tip, lineage.TrailerForkID))
		equal(t, name+": the subject", h.accountGit("log", "-1", "--format=%s", tip), "Fork "+name+" from "+s.url+"/skills/"+name+" at "+short(h.trailer(imports[name], "Agentx-Upstream-Commit")))
		root := filepath.Join(h.agentx, "worktrees", name)
		equal(t, name+": git status in the worktree", gitIn(t, h, root, "status", "--porcelain"), "")
		if _, err := os.Lstat(filepath.Join(root, name, ".git")); err == nil {
			t.Errorf("%s: the skill directory holds a .git entry", name)
		}
		if link, _ := os.Readlink(filepath.Join(h.library, name)); link != "../../../agentx/worktrees/"+name+"/"+name {
			t.Errorf("%s: the library symlink is %q", name, link)
		}
		for _, client := range []string{".claude", ".cursor"} {
			equal(t, name+": "+client+"'s placement", placed(client, name), filepath.Join(h.library, name))
		}
		for _, client := range []string{".codex", ".gemini"} {
			if _, err := os.Lstat(filepath.Join(h.home, client, "skills", name)); err == nil {
				t.Errorf("%s: %s, which reads the library, got a symlink", name, client)
			}
		}
	}
	alpha, beta := h.ref(lineage.ForkRef("alpha")), h.ref(lineage.ForkRef("beta"))
	equal(t, "the unedited fork's tree", h.accountGit("rev-parse", alpha+"^{tree}"), h.accountGit("rev-parse", imports["alpha"]+"^{tree}"))
	equal(t, "what the edited fork's creation commit changes", h.accountGit("diff-tree", "-r", "--name-status", imports["beta"], beta), "M\tbeta/SKILL.md")
	if b, err := os.ReadFile(filepath.Join(h.agentx, "worktrees", "beta", "beta", ".DS_Store")); err != nil || string(b) != "finder\n" {
		t.Errorf("the ignored file did not move with the skill: %q, %v", b, err)
	}
	equal(t, "the candidate", h.ref(lineage.CandidateRef("alpha")), imports["beta"])
	if _, err := h.accountGitErr("rev-parse", "--verify", "-q", lineage.UpstreamRemovedRef("alpha")); err == nil {
		t.Error("the upstream-removed marker is still there")
	}
	for _, dir := range []string{h.library, filepath.Join(h.agentx, "worktrees")} {
		equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
	}
}

// TestForksOfOneVersionShareTheImport forks one version of a skill on two
// machines, in its place on one and beside it under a new name on the
// other, where the skill also has a copy in one client. The forks have ids
// of their own and the same import as their parent. The fork beside the
// skill writes its name into SKILL.md, keeps the upstream's directory in
// its branch and is placed as the skill is, the copy included, which
// stays as it was. Forked in its place on the second machine too, the
// skill is a third fork of the version, with an id of its own, though its
// name is the first machine's: a publish of one over the other is refused,
// see TestPublishRefusesADifferentFork.
func TestForksOfOneVersionShareTheImport(t *testing.T) {
	t.Parallel()
	a, s := installHarness(t)
	b, _ := installHarness(t)
	for _, h := range []*harness{a, b} {
		h.mustRun("skill", "add", s.url, "--skill", "alpha")
	}
	b.mustRun("skill", "place", "alpha", "--to", "cursor", "--copy")
	a.mustRun("skill", "fork", "alpha")
	out := b.mustRun("skill", "fork", "alpha", "--name", "my-alpha")
	contains(t, "the text output", out.stdout, "✓ forked alpha as my-alpha: 4 placements; alpha stays as it was\n")

	tipA, tipB := a.ref(lineage.ForkRef("alpha")), b.ref(lineage.ForkRef("my-alpha"))
	equal(t, "the shared import", b.accountGit("rev-parse", tipB+"^"), a.accountGit("rev-parse", tipA+"^"))
	if idA, idB := a.trailer(tipA, lineage.TrailerForkID), b.trailer(tipB, lineage.TrailerForkID); idA == idB || idB == "" {
		t.Errorf("the fork ids are %q and %q", idA, idB)
	}
	equal(t, "the original", b.ref(lineage.ManagedRef("alpha")), a.accountGit("rev-parse", tipA+"^"))
	equal(t, "the renamed fork's directory", b.accountGit("ls-tree", "--name-only", tipB), "alpha")
	equal(t, "what the rename changes", b.accountGit("diff-tree", "-r", "--name-status", tipB+"^", tipB), "M\talpha/SKILL.md")
	renamed, err := renameFrontmatter([]byte(b.accountGit("cat-file", "blob", tipB+"^:alpha/SKILL.md")+"\n"), "my-alpha")
	if err != nil {
		t.Fatal(err)
	}
	equal(t, "the renamed SKILL.md", b.accountGit("cat-file", "blob", tipB+":alpha/SKILL.md")+"\n", string(renamed))
	contains(t, "the renamed SKILL.md", string(renamed), "name: my-alpha\n")
	ev := b.listed("my-alpha")
	equal(t, "the renamed fork's upstream", ev["upstream_commit"], b.listed("alpha")["upstream_commit"])
	modes := map[string]string{}
	for _, p := range ev["placements"].([]any) {
		place := p.(map[string]any)
		modes[place["configuration"].(string)] = place["mode"].(string)
	}
	equal(t, "the renamed fork's placements", fmt.Sprint(modes), fmt.Sprint(map[string]string{"claude-code": modeSymlink, "cursor": modeCopy, "codex": modeLibrary, "gemini-cli": modeLibrary}))
	if info, err := os.Lstat(filepath.Join(b.home, ".cursor", "skills", "alpha")); err != nil || !info.IsDir() {
		t.Errorf("the original's copy is not as it was: %v", err)
	}
	if info, err := os.Lstat(filepath.Join(b.library, "alpha")); err != nil || !info.IsDir() {
		t.Errorf("the original's library directory is not as it was: %v", err)
	}

	b.mustRun("skill", "fork", "alpha")
	tipB = b.ref(lineage.ForkRef("alpha"))
	equal(t, "the import of b's alpha", b.accountGit("rev-parse", tipB+"^"), a.accountGit("rev-parse", tipA+"^"))
	if idA, idB := a.trailer(tipA, lineage.TrailerForkID), b.trailer(tipB, lineage.TrailerForkID); idA == idB {
		t.Errorf("both machines' alpha have the fork id %q", idA)
	}
}

// TestSkillForkOfAnUnmanagedSkill forks two skills the library holds that
// no branch records. One, whose directory name agentx cannot give a fork,
// is refused unless --name gives another, and then forked beside itself
// under that name, into the client it is placed in, as a branch of its own
// named after the fork. The other is forked in its place: its content is
// the first commit of a branch of its own, by the user and with a new fork
// id, its directory moves into the worktree and the library holds a
// symlink to it.
func TestSkillForkOfAnUnmanagedSkill(t *testing.T) {
	t.Parallel()
	h, _ := installHarness(t)
	h.withIdentity("Ada Lovelace", "ada@example.com")
	writeFile(t, mkdirs(t, filepath.Join(h.library, "My_Skill"), "SKILL.md"), skill("My_Skill", "Mine"))
	writeFile(t, mkdirs(t, filepath.Join(h.library, "notes"), "SKILL.md"), skill("notes", "Take notes"))
	if err := os.MkdirAll(filepath.Join(h.home, ".claude", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(h.library, "My_Skill"), filepath.Join(h.home, ".claude", "skills", "My_Skill")); err != nil {
		t.Fatal(err)
	}
	out := h.run("--json", "skill", "fork", "My_Skill")
	equal(t, "an invalid name: exit", out.exit, exitRefused.exit)
	contains(t, "an invalid name: hint", h.one(out.stdout, "error")["hint"].(string), "agentx skill fork My_Skill --name <new>")

	h.mustRun("skill", "fork", "My_Skill", "--name", "my-skill")
	tip := h.ref(lineage.ForkRef("my-skill"))
	equal(t, "the renamed fork's tree", h.accountGit("ls-tree", "-r", "--name-only", tip), "my-skill/SKILL.md")
	contains(t, "the renamed fork's SKILL.md", h.accountGit("cat-file", "blob", tip+":my-skill/SKILL.md"), "name: my-skill\n")
	if target, _ := os.Readlink(filepath.Join(h.home, ".claude", "skills", "my-skill")); target != filepath.Join(h.library, "my-skill") {
		t.Errorf("the renamed fork's claude-code placement points at %q", target)
	}
	if _, err := os.Lstat(filepath.Join(h.home, ".cursor", "skills", "my-skill")); err == nil {
		t.Error("the renamed fork was placed in cursor, where the original is not")
	}
	equal(t, "the original", h.listed("My_Skill")["kind"], lineage.KindUnmanaged)

	text := h.mustRun("skill", "fork", "notes")
	equal(t, "the text output", text.stdout, "✓ forked notes; the fork replaces it wherever it was\n")
	tip = h.ref(lineage.ForkRef("notes"))
	equal(t, "the history", h.accountGit("rev-list", "--parents", lineage.ForkRef("notes")), tip)
	equal(t, "the author", h.accountGit("log", "-1", "--format=%an <%ae>", tip), "Ada Lovelace <ada@example.com>")
	equal(t, "the tree", h.accountGit("ls-tree", "-r", "--name-only", tip), "notes/SKILL.md")
	if link, _ := os.Readlink(filepath.Join(h.library, "notes")); link != "../../../agentx/worktrees/notes/notes" {
		t.Errorf("the library symlink is %q", link)
	}
	ev := h.listed("notes")
	equal(t, "kind", ev["kind"], lineage.KindFork)
	equal(t, "the fork id", ev["fork_id"], h.trailer(tip, lineage.TrailerForkID))
	if _, ok := ev["source"]; ok {
		t.Errorf("a fork of an unmanaged skill lists an upstream: %v", ev["source"])
	}
}

// pluginHome gives h's user a plugin called formatter, which provides a
// skill called format to Claude Code and to Codex, and another, linter,
// which provides a skill of the same name to Claude Code alone. It returns
// the directories the two Claude Code plugins are installed in.
func pluginHome(t *testing.T, h *harness) []string {
	t.Helper()
	formatter := filepath.Join(h.home, ".claude", "plugins", "cache", "acme", "formatter", "1.0.0")
	linter := filepath.Join(h.home, ".claude", "plugins", "cache", "acme", "linter", "2.0.0")
	writeFile(t, mkdirs(t, filepath.Join(h.home, ".claude", "plugins"), "installed_plugins.json"),
		`{"version": 2, "plugins": {"formatter@acme": [{"installPath": "`+formatter+`", "version": "1.0.0"}], "linter@acme": [{"installPath": "`+linter+`", "version": "2.0.0"}]}}`)
	for dir, name := range map[string]string{formatter: "formatter", linter: "linter"} {
		writeFile(t, mkdirs(t, filepath.Join(dir, ".claude-plugin"), "plugin.json"), `{"name": "`+name+`", "version": "1.0.0"}`)
		writeFile(t, mkdirs(t, filepath.Join(dir, "skills", "format"), "SKILL.md"), skill("format", "Format the code with "+name))
	}
	codex := filepath.Join(h.home, ".codex", "plugins", "cache", "acme", "formatter", "1.0.0")
	writeFile(t, mkdirs(t, filepath.Join(codex, ".codex-plugin"), "plugin.json"), `{"name": "formatter", "version": "1.0.0"}`)
	writeFile(t, mkdirs(t, filepath.Join(codex, "skills", "format"), "SKILL.md"), skill("format", "Format the code with formatter"))
	writeFile(t, filepath.Join(h.home, ".codex", "config.toml"), "[plugins.\"formatter@acme\"]\nenabled = true\n")
	return []string{formatter, linter, codex}
}

// TestSkillForkOfAPluginSkill forks a skill two plugins provide under one
// name: the bare name is refused, naming both, and the plugin is named.
// The fork is a branch of its own, placed only into the configurations
// that have the plugin, and the result notes, client by client, how each
// treats the fork beside the plugin's copy, the client that reads the
// library without the plugin included. A configuration that has the
// plugin but is disabled gets neither the fork nor a note. Every plugin
// directory is left byte for byte as it was.
func TestSkillForkOfAPluginSkill(t *testing.T) {
	t.Parallel()
	h, _ := installHarness(t)
	plugins := pluginHome(t, h)
	before := map[string]string{}
	for _, dir := range plugins {
		before[dir], _ = home.Fingerprint(dir)
	}
	out := h.run("--json", "skill", "fork", "format")
	equal(t, "an ambiguous name: exit", out.exit, exitRefused.exit)
	contains(t, "an ambiguous name", h.one(out.stdout, "error")["message"].(string), "format is provided by several plugins: formatter, linter")

	out = h.run("--json", "skill", "fork", "formatter:format")
	equal(t, "exit", out.exit, 0)
	var notes []string
	for _, n := range h.eventsOfType(out.stdout, "fork_note") {
		equal(t, "the note's plugin", n["plugin"], "formatter")
		notes = append(notes, n["configuration"].(string)+": "+n["note"].(string))
	}
	equal(t, "the notes", strings.Join(notes, "\n"), strings.Join([]string{
		"claude-code: " + scan.PluginForkNote("claude-code"),
		"codex: " + scan.PluginForkNote("codex"),
		"gemini-cli: it reads the library, so it sees format whether or not it has formatter",
	}, "\n"))
	tip := h.ref(lineage.ForkRef("format"))
	equal(t, "the history", h.accountGit("rev-list", lineage.ForkRef("format")), tip)
	equal(t, "the subject", h.accountGit("log", "-1", "--format=%s", tip), "Fork format from plugin formatter")
	if target, _ := os.Readlink(filepath.Join(h.home, ".claude", "skills", "format")); target != filepath.Join(h.library, "format") {
		t.Errorf("claude-code's placement points at %q", target)
	}
	for _, client := range []string{".cursor", ".codex", ".gemini"} {
		if _, err := os.Lstat(filepath.Join(h.home, client, "skills", "format")); err == nil {
			t.Errorf("%s got a placement", client)
		}
	}
	for _, dir := range plugins {
		if after, _ := home.Fingerprint(dir); after != before[dir] {
			t.Errorf("the plugin directory %s changed", dir)
		}
	}
	h.mustRun("config", "disable", "claude-code")
	text := h.mustRun("skill", "fork", "linter:format", "--name", "lint-format")
	contains(t, "the text output", text.stdout, "• codex: it reads the library, so it sees lint-format whether or not it has linter\n")
	if strings.Contains(text.stdout, "claude-code:") {
		t.Errorf("a disabled configuration, which gets no fork, got a note:\n%s", text.stdout)
	}
	contains(t, "the text output", text.stdout, "✓ forked format from plugin linter as lint-format: 0 placements; the plugin's copy stays as it was\n")
}

// TestSkillForkOfAFork forks a fork, which only a new name can do: with
// no name or its own it is refused, and so it is while it holds an edit nobody
// committed. Once the edit is committed, the new fork's branch starts at
// the fork's tip, so its history is kept, with a creation commit of its
// own that writes the new name and leaves out the file a commit made with
// git put beside the skill directory, and the new fork reads the upstream
// its source came from and goes where its source is.
func TestSkillForkOfAFork(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha")
	h.mustRun("skill", "fork", "alpha")
	refuses := func(what string, args []string, st status, says string) {
		t.Helper()
		out := h.run(append([]string{"--json", "skill", "fork"}, args...)...)
		equal(t, what+": exit", out.exit, st.exit)
		contains(t, what+": error", h.one(out.stdout, "error")["message"].(string), says)
	}
	refuses("no new name", []string{"alpha"}, exitRefused, "alpha is already a fork")
	refuses("its own name", []string{"alpha", "--name", "alpha"}, exitRefused, "the fork is already called alpha")
	writeFile(t, filepath.Join(h.library, "alpha", "notes.md"), "edited\n")
	refuses("uncommitted edits", []string{"alpha", "--name", "alpha-two"}, exitRefused, "alpha has uncommitted edits, so it cannot be forked")
	h.mustRun("skill", "commit", "alpha")
	root := filepath.Join(h.agentx, "worktrees", "alpha")
	writeFile(t, filepath.Join(root, "README.md"), "beside\n")
	gitIn(t, h, root, "add", "README.md")
	gitIn(t, h, root, "-c", "user.name=Grace Hopper", "-c", "user.email=grace@example.com", "commit", "-q", "-m", "Beside the skill")

	source := h.ref(lineage.ForkRef("alpha"))
	h.mustRun("skill", "fork", "alpha", "--name", "alpha-two")
	tip := h.ref(lineage.ForkRef("alpha-two"))
	equal(t, "the parent", h.accountGit("rev-parse", tip+"^"), source)
	equal(t, "the kept history", h.accountGit("rev-list", "--count", tip), "5")
	if a, b := h.trailer(source+"^^", lineage.TrailerForkID), h.trailer(tip, lineage.TrailerForkID); a == b || b == "" {
		t.Errorf("the fork ids are %q and %q", a, b)
	}
	equal(t, "what the creation commit changes", h.accountGit("diff-tree", "-r", "--name-status", source, tip), "D\tREADME.md\nM\talpha/SKILL.md")
	contains(t, "SKILL.md", h.accountGit("cat-file", "blob", tip+":alpha/SKILL.md"), "name: alpha-two\n")
	equal(t, "the upstream", h.listed("alpha-two")["upstream_commit"], h.listed("alpha")["upstream_commit"])
	for _, client := range []string{".claude", ".cursor"} {
		if target, _ := os.Readlink(filepath.Join(h.home, client, "skills", "alpha-two")); target != filepath.Join(h.library, "alpha-two") {
			t.Errorf("%s's placement points at %q", client, target)
		}
	}
	equal(t, "git status in the new worktree", gitIn(t, h, filepath.Join(h.agentx, "worktrees", "alpha-two"), "status", "--porcelain"), "")
}

// TestSkillForkRefusals refuses, in one home and changing nothing, a skill
// nothing provides, a name another branch holds, a library entry that is a
// symlink of the user's, a worktree path that is taken, a skill holding a
// repository git would record as a link, a skill that is itself a
// repository, which its own .gitignore naming .git does not let move, and
// a skill whose update left a merge pending, which is asked about before
// anything is committed. skill unfork, reserved, is refused in the same
// home.
func TestSkillForkRefusals(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha", "--skill", "beta")
	writeFile(t, mkdirs(t, filepath.Join(h.library, "beta", "vendor", ".git"), "HEAD"), "ref: refs/heads/main\n")
	writeFile(t, mkdirs(t, filepath.Join(h.home, "mine", "gamma"), "SKILL.md"), skill("gamma", "Mine"))
	if err := os.Symlink(filepath.Join(h.home, "mine", "gamma"), filepath.Join(h.library, "gamma")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(h.agentx, "worktrees", "held"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, mkdirs(t, filepath.Join(h.library, "held"), "SKILL.md"), skill("held", "Held"))
	writeFile(t, mkdirs(t, filepath.Join(h.library, "cloned"), "SKILL.md"), skill("cloned", "A clone"))
	writeFile(t, mkdirs(t, filepath.Join(h.library, "cloned", ".git"), "HEAD"), "ref: refs/heads/main\n")
	writeFile(t, filepath.Join(h.library, "cloned", ".gitignore"), ".git\n")
	refs := h.accountGit("for-each-ref")
	for _, tc := range []struct {
		args []string
		exit status
		says string
	}{
		{[]string{"nothing"}, exitNotFound, `neither the library nor any plugin holds a skill called "nothing"`},
		{[]string{"alpha", "--name", "beta"}, exitRefused, "the account repo already holds beta as a managed skill"},
		{[]string{"gamma"}, exitRefused, "is a symlink to"},
		{[]string{"held"}, exitRefused, "worktrees/held already exists"},
		{[]string{"beta"}, exitRefused, "beta holds a Git repository at vendor/.git"},
		{[]string{"cloned"}, exitRefused, "cloned is itself a Git repository"},
	} {
		out := h.run(append([]string{"--json", "skill", "fork"}, tc.args...)...)
		equal(t, strings.Join(tc.args, " ")+": exit", out.exit, tc.exit.exit)
		contains(t, strings.Join(tc.args, " ")+": error", h.one(out.stdout, "error")["message"].(string), tc.says)
	}
	// The command that retires a fork in favour of its upstream is
	// reserved, and refused whatever it names.
	unfork := h.run("--json", "skill", "unfork", "alpha")
	equal(t, "unfork: exit", unfork.exit, exitRefused.exit)
	e := h.one(unfork.stdout, "error")
	equal(t, "unfork: error", e["message"], "skill unfork is reserved for a later version")
	equal(t, "unfork: hint", e["hint"], "remove the fork with 'agentx skill remove alpha' and install the upstream again with 'agentx skill add'")
	if err := os.MkdirAll(filepath.Join(h.agentx, "merges", "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	objects := h.accountGit("count-objects")
	out := h.run("--json", "skill", "fork", "alpha", "--name", "my-alpha")
	equal(t, "a merge pending: exit", out.exit, exitPendingMerge.exit)
	contains(t, "a merge pending", h.one(out.stdout, "error")["message"].(string), "alpha has a merge with its update pending, so it cannot be forked")
	equal(t, "the objects", h.accountGit("count-objects"), objects)
	equal(t, "the refs", h.accountGit("for-each-ref"), refs)
	if entries, _ := os.ReadDir(filepath.Join(h.agentx, "worktrees")); len(entries) != 1 {
		t.Errorf("a refused fork left %d entries in the worktrees directory, want the one the test made", len(entries))
	}
}

// renameCases are renameFrontmatter's edge cases.
var renameCases = []struct {
	name, md, want string
	err            error
}{
	{"a plain value", "---\nname: alpha\ndescription: d\n---\n# alpha\n", "---\nname: my-alpha\ndescription: d\n---\n# alpha\n", nil},
	{"a quoted value and a comment", "---\n# top\nname: \"alpha\" # old\ndescription: d\n---\n", "---\n# top\nname: my-alpha\ndescription: d\n---\n", nil},
	{"a value over several lines", "---\nname: >\n  alpha\n  skill\ndescription: d\n---\n", "---\nname: my-alpha\ndescription: d\n---\n", nil},
	{"a nested name is kept", "---\nmetadata:\n  name: x\nname: alpha\n---\n", "---\nmetadata:\n  name: x\nname: my-alpha\n---\n", nil},
	{"a key that starts with name", "---\nnamespace: x\n---\n", "---\nname: my-alpha\nnamespace: x\n---\n", nil},
	{"CRLF line endings", "---\r\nname: alpha\r\ndescription: d\r\n---\r\n", "---\r\nname: my-alpha\r\ndescription: d\r\n---\r\n", nil},
	{"no name key", "---\ndescription: d\n---\nbody\n", "---\nname: my-alpha\ndescription: d\n---\nbody\n", nil},
	{"no frontmatter", "# alpha\n", "---\nname: my-alpha\n---\n# alpha\n", nil},
	{"an empty file", "", "---\nname: my-alpha\n---\n", nil},
	{"a duplicate key", "---\nname: a\nname: b\n---\n", "---\nname: my-alpha\n---\n", nil},
	{"a closing line with blanks after it", "---\nname: a\n--- \n", "---\nname: my-alpha\n--- \n", nil},
	{"a block that is not closed", "---\nname: a\n", "", errUnclosedFrontmatter},
	{"bytes that are not UTF-8", "---\nname: \xff\n---\n", "", errNotText},
}

func TestRenameFrontmatter(t *testing.T) {
	t.Parallel()
	for _, tc := range renameCases {
		got, err := renameFrontmatter([]byte(tc.md), "my-alpha")
		if err != tc.err || string(got) != tc.want {
			t.Errorf("%s: renameFrontmatter = %q, %v, want %q, %v", tc.name, got, err, tc.want, tc.err)
		}
	}
	// A name YAML would read as something other than a string is quoted.
	if got, _ := renameFrontmatter([]byte("---\nname: a\n---\n"), "null"); string(got) != "---\nname: \"null\"\n---\n" {
		t.Errorf("a name YAML reads as null is written %q", got)
	}
	got, _ := renameFrontmatter([]byte("---\nname: >-\n  a\n  b\ndescription: d\n---\n"), "my-alpha")
	if name, description, err := scan.SkillFrontmatter(string(got)); name != "my-alpha" || description != "d" || err != nil {
		t.Errorf("the scan reads the rewritten frontmatter as %q, %q, %v", name, description, err)
	}
}

// TestPluginOccurrence picks the plugin's skill a name means among what a
// scan found, refusing a name nothing or several plugins provide.
func TestPluginOccurrence(t *testing.T) {
	t.Parallel()
	occ := func(plugin, config, path, scope string) scan.Occurrence {
		return scan.Occurrence{Plugin: plugin, Configuration: config, Path: path, ResolvedPath: path, Scope: scope}
	}
	snap := scan.Snapshot{Skills: []scan.Skill{
		{Occurrences: []scan.Occurrence{occ("fmt", "codex", "/c/fmt/skills/format", "user"), occ("fmt", "claude-code", "/a/fmt/skills/format", "user")}},
		{Occurrences: []scan.Occurrence{occ("lint", "claude-code", "/a/lint/skills/check", "user"), occ("", "claude-code", "/u/skills/check", "user")}},
		{Occurrences: []scan.Occurrence{occ("style", "claude-code", "/a/style/skills/check", "user"), occ("proj", "claude-code", "/p/proj/skills/only", "project")}},
	}}
	for _, tc := range []struct {
		arg, path string
		exit      int
	}{
		{"format", "/a/fmt/skills/format", 0},
		{"fmt:format", "/a/fmt/skills/format", 0},
		{"check", "", exitRefused.exit},
		{"lint:check", "/a/lint/skills/check", 0},
		{"lint:format", "", exitNotFound.exit},
		{"only", "", exitNotFound.exit},
	} {
		o, err := pluginOccurrence(snap, tc.arg)
		code := 0
		if f := (*failure)(nil); errors.As(err, &f) {
			code = f.status.exit
		}
		if code != tc.exit || o.Path != tc.path {
			t.Errorf("%s: pluginOccurrence = %q, exit %d, want %q, exit %d", tc.arg, o.Path, code, tc.path, tc.exit)
		}
	}
}

// TestPluginForkNotes notes every configuration with the plugin by what
// its client does, one nothing is known of included, and every client that
// reads the library without the plugin, sorted by configuration.
func TestPluginForkNotes(t *testing.T) {
	t.Parallel()
	notes := pluginForkNotes("format", "fmt", []string{"windsurf", "claude-code", "codex"}, []string{"codex", "gemini-cli"})
	var got []string
	for _, n := range notes {
		got = append(got, n.Configuration+": "+n.Note)
		if n.Name != "format" || n.Plugin != "fmt" || n.Type != "fork_note" {
			t.Errorf("a note is %+v", n)
		}
	}
	equal(t, "the notes", strings.Join(got, "\n"), strings.Join([]string{
		"claude-code: " + scan.PluginForkNote("claude-code"),
		"codex: " + scan.PluginForkNote("codex"),
		"gemini-cli: it reads the library, so it sees format whether or not it has fmt",
		"windsurf: which of the two copies wins is not documented",
	}, "\n"))
}

// forkChildEnv marks the process TestSkillForkRecoversWhereItWasKilled
// starts, which forks the skill it names and is killed part way.
const forkChildEnv = "AGENTX_TEST_FORK_CHILD"

// TestForkChildProcess is not a test: it is the body of that process. It
// does nothing when the variable that marks it is not set.
func TestForkChildProcess(t *testing.T) {
	name := os.Getenv(forkChildEnv)
	if name == "" {
		t.Skip("not the skill fork child process")
	}
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	os.Exit(Run(context.Background(), []string{"skill", "fork", name}, env, strings.NewReader(""), os.Stdout, os.Stderr))
}

// TestSkillForkRecoversWhereItWasKilled kills a fork of a managed skill in
// its place with SIGKILL at two of its boundaries: once its journal is on
// disk and before anything was applied, and right after git added the
// worktree. Every boundary between is the journal's, which
// home.TestConversionRecoversFromEveryBoundary replays without git. The
// next command finishes it, and the skill's directory, which only moved,
// is in the worktree with its edit and its ignored file.
func TestSkillForkRecoversWhereItWasKilled(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, script string
	}{
		{"once the journal is on disk", `
for f in %MUTATIONS%/*.json; do
	if [ -e "$f" ]; then
		kill -9 $PPID
		exit 1
	fi
done
exec %GIT% "$@"
`},
		{"after the worktree was added", `
case " $* " in
*" worktree add "*)
	%GIT% "$@"
	kill -9 $PPID
	exit 1 ;;
esac
exec %GIT% "$@"
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h, s := installHarness(t)
			h.mustRun("skill", "add", s.url, "--skill", "alpha")
			writeFile(t, filepath.Join(h.library, "alpha", "notes.md"), "my notes\n")
			writeFile(t, filepath.Join(h.library, "alpha", ".DS_Store"), "finder\n")
			out := killedChild(t, h, "TestForkChildProcess", forkChildEnv, "alpha", tc.script)
			_, kinds := journalKinds(t, h)
			equal(t, "the journal's steps", kinds, "ref, ref, worktree, move, link")
			if got := h.run("config", "set", "label", "recovered"); got.exit != 0 {
				t.Fatalf("the command after the killed fork: exit %d\n%s\nthe killed run:\n%s", got.exit, got.stderr, out)
			}
			equal(t, "journals after recovery", journalCount(t, h), 0)
			root := filepath.Join(h.agentx, "worktrees", "alpha")
			if !home.WorktreeAt(root, "skills/alpha") {
				t.Error("the worktree is not on the branch after recovery")
			}
			if _, err := h.accountGitErr("rev-parse", "--verify", "-q", lineage.ManagedRef("alpha")); err == nil {
				t.Error("the import branch is still there")
			}
			equal(t, "git status in the worktree", gitIn(t, h, root, "status", "--porcelain"), "")
			for file, want := range map[string]string{"notes.md": "my notes\n", ".DS_Store": "finder\n"} {
				if b, err := os.ReadFile(filepath.Join(h.library, "alpha", file)); err != nil || string(b) != want {
					t.Errorf("%s through the library symlink is %q, %v", file, b, err)
				}
			}
			equal(t, "kind", h.listed("alpha")["kind"], lineage.KindFork)
		})
	}
}
