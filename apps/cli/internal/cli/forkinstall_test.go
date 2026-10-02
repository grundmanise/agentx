package cli

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// TestInstallAForkFromTheAccount lists on machine b, which installed
// nothing and added no source, the forks machine a published, and installs
// one. skill list --remote fetches the account remote and lists alpha,
// beta and the greenfield notes as installable, each with the fork id and
// the upstream its own commits record, notes with none. Installing alpha
// creates b's branch at a's commit, so the fork keeps its id and its
// history, tracking the account remote's branch, where a pull of it before
// was exit 5 naming the install; checks its worktree out
// with the .gitignore a commit put beside the skill directory, so git
// status there is clean; links the library to it and places it into the
// enabled configuration. It adds the fork's upstream source, which b
// lacked, so that b's update check finds the source's next version for
// alpha. Listed again, the account remote holds two forks b has not
// installed, and alpha is refused as installed already.
func TestInstallAForkFromTheAccount(t *testing.T) {
	t.Parallel()
	a, b, s, _ := accountHomes(t)
	rootA := filepath.Join(a.agentx, "worktrees", "alpha")
	writeFile(t, filepath.Join(rootA, ".gitignore"), "*.log\n")
	gitIn(t, a, rootA, "add", ".gitignore")
	gitIn(t, a, rootA, "-c", "user.name=Grace Hopper", "-c", "user.email=grace@example.com", "commit", "-q", "-m", "Beside the skill")
	a.mustRun("skill", "new", "notes")
	a.mustRun("publish", "--all")
	tip := a.ref(lineage.ForkRef("alpha"))

	out := b.mustRun("--json", "skill", "list", "--remote")
	listed := map[string]jsonEvent{}
	for _, ev := range b.eventsOfType(out.stdout, "installable_fork") {
		listed[ev["name"].(string)] = ev
	}
	equal(t, "installable", strings.Join(sortedKeys(listed), " "), "alpha beta notes")
	alpha := listed["alpha"]
	equal(t, "alpha's commit", alpha["commit"], tip)
	equal(t, "alpha's fork id", alpha["fork_id"], a.listed("alpha")["fork_id"])
	equal(t, "alpha's source", alpha["source"], s.url)
	equal(t, "alpha's subpath", alpha["subpath"], "skills/alpha")
	equal(t, "alpha's upstream commit", alpha["upstream_commit"], a.listed("alpha")["upstream_commit"])
	equal(t, "alpha's base hash", alpha["base_hash"], a.listed("alpha")["base_hash"])
	if _, ok := listed["notes"]["source"]; ok {
		t.Errorf("the greenfield notes is listed with a source: %v", listed["notes"])
	}
	text := b.mustRun("skill", "list", "--remote").stdout
	contains(t, "the text listing", text, "3 installable forks on the account remote")
	contains(t, "the text listing", text, "agentx skill add --from-account <name>")

	out = b.run("--json", "pull", "alpha")
	equal(t, "a pull of a fork not installed: exit", out.exit, 5)
	contains(t, "its hint", b.one(out.stdout, "error")["hint"].(string), "install it with 'agentx skill add --from-account alpha'")

	out = b.mustRun("--json", "skill", "add", "--from-account", "alpha")
	equal(t, "b's alpha", b.ref(lineage.ForkRef("alpha")), tip)
	ev := b.librarySkill(out.stdout, "alpha")
	equal(t, "kind", ev["kind"], lineage.KindFork)
	equal(t, "fork id", ev["fork_id"], alpha["fork_id"])
	equal(t, "state", ev["state"], stateCurrent)
	equal(t, "source", ev["source"], s.url)
	equal(t, "the source it added", b.one(out.stdout, "source")["url"], s.url)
	equal(t, "tracking", b.accountGit("config", "--get", "branch.skills/alpha.remote")+" "+b.accountGit("config", "--get", "branch.skills/alpha.merge"),
		"origin refs/heads/skills/alpha")
	rootB := filepath.Join(b.agentx, "worktrees", "alpha")
	equal(t, "the .gitignore beside the skill", fileBody(t, filepath.Join(rootB, ".gitignore")), "*.log\n")
	writeFile(t, filepath.Join(rootB, "alpha", "build.log"), "ignored\n")
	equal(t, "git status in the worktree", gitIn(t, b, rootB, "status", "--porcelain"), "")
	if target, _ := os.Readlink(filepath.Join(b.home, ".claude", "skills", "alpha")); target != filepath.Join(b.library, "alpha") {
		t.Errorf("claude-code's placement points at %q", target)
	}
	if real, err := filepath.EvalSymlinks(filepath.Join(b.library, "alpha")); err != nil || filepath.Base(filepath.Dir(real)) != "alpha" || !strings.Contains(real, "worktrees") {
		t.Errorf("the library entry leads to %q, %v", real, err)
	}
	equal(t, "the result", b.one(out.stdout, "result")["summary"], "installed alpha from the account remote in 1 configuration")

	s.write("skills/alpha/notes.md", forkNotes("seven", "seven, upstream"))
	s.commit("second version")
	out = b.mustRun("--json", "skill", "check")
	equal(t, "the update b's check finds", b.updateOf(out.stdout, "alpha")["kind"], lineage.KindFork)

	out = b.mustRun("--json", "skill", "list", "--remote")
	equal(t, "installable once alpha is installed", len(b.eventsOfType(out.stdout, "installable_fork")), 2)
	out = b.run("--json", "skill", "add", "--from-account", "alpha")
	equal(t, "installing alpha again: exit", out.exit, 6)
	contains(t, "its error", b.one(out.stdout, "error")["message"].(string), "alpha is already installed on this machine")
}

// sortedKeys is the keys of m in order.
func sortedKeys(m map[string]jsonEvent) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestFromAccountSupersedesAnUnmodifiedCopy installs alpha from the
// account remote on machine b, which installed the same upstream version
// of alpha from the source first, removed it from Cursor, left a .DS_Store
// in it, and had a check pin the source's next version for it. The copy
// holds its base version, so the fork takes its place: the import branch,
// its update candidate and the library directory go, the library entry is
// the symlink into the fork's worktree, which holds the .DS_Store, and the
// placements are as they were, Claude Code's leading to the fork and
// Cursor still without one.
func TestFromAccountSupersedesAnUnmodifiedCopy(t *testing.T) {
	t.Parallel()
	_, b, s, _ := accountHomes(t)
	if err := os.MkdirAll(filepath.Join(b.home, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	b.mustRun("skill", "add", s.url, "--skill", "alpha")
	b.mustRun("skill", "remove", "alpha", "--from", "cursor")
	writeFile(t, filepath.Join(b.library, "alpha", ".DS_Store"), "finder data\n")
	s.write("skills/alpha/notes.md", forkNotes("seven", "seven, upstream"))
	s.commit("second version")
	b.mustRun("skill", "check")
	if b.ref(lineage.CandidateRef("alpha")) == "" {
		t.Fatal("the check pinned no candidate for alpha")
	}
	claude := filepath.Join(b.home, ".claude", "skills", "alpha")
	before, _ := os.Readlink(claude)

	out := b.mustRun("--json", "skill", "add", "--from-account", "alpha")
	equal(t, "the import branch", b.ref(lineage.ManagedRef("alpha")), "")
	equal(t, "the candidate", b.ref(lineage.CandidateRef("alpha")), "")
	if !home.IsLink(stateOf(t, filepath.Join(b.library, "alpha"))) {
		t.Error("the library entry is not the symlink into the fork's worktree")
	}
	skillDir := b.forkDir("alpha", "alpha")
	equal(t, "the carried .DS_Store", fileBody(t, filepath.Join(skillDir, ".DS_Store")), "finder data\n")
	equal(t, "git status in the worktree", gitIn(t, b, filepath.Dir(skillDir), "status", "--porcelain"), "")
	after, _ := os.Readlink(claude)
	equal(t, "claude-code's placement", after, before)
	if lexists(filepath.Join(b.home, ".cursor", "skills", "alpha")) {
		t.Error("the install placed alpha into cursor, which it had been removed from")
	}
	ev := b.librarySkill(out.stdout, "alpha")
	equal(t, "kind", ev["kind"], lineage.KindFork)
	equal(t, "state", ev["state"], stateCurrent)
	contains(t, "the result", b.one(out.stdout, "result")["summary"].(string), "the fork replaces the managed skill wherever it was")
	equal(t, "what is left beside the library", strings.Join(hiddenEntries(t, b.library), " "), "")
}

// stateOf is what the journal reads at path.
func stateOf(t *testing.T, path string) string {
	t.Helper()
	state, err := home.State(path)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

// TestFromAccountRefusesAndKeepsLocal installs from the account remote on
// machine b over what its library holds. Each refusal changes nothing: a
// fork the account remote does not hold, exit 5; an unmanaged directory at
// alpha's library path, exit 6; a symlink at beta's, exit 6 with
// --keep-local too; and an edited managed copy of beta's upstream, exit 6;
// --keep-local without --from-account is exit 1. With --keep-local, the
// unmanaged directory is moved into alpha's worktree and the edited copy
// into beta's, with its import branch gone: each content is uncommitted
// edits of the fork at the remote tip, nothing discarded, and a file git
// ignores stays in the directory without being one.
func TestFromAccountRefusesAndKeepsLocal(t *testing.T) {
	t.Parallel()
	a, b, s, _ := accountHomes(t)
	alphaLib, betaLib := filepath.Join(b.library, "alpha"), filepath.Join(b.library, "beta")
	if err := os.MkdirAll(alphaLib, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(alphaLib, "SKILL.md"), "---\nname: alpha\ndescription: Mine\n---\n")
	writeFile(t, filepath.Join(alphaLib, ".DS_Store"), "finder data\n")
	if err := os.Symlink(alphaLib, betaLib); err != nil {
		t.Fatal(err)
	}
	unchanged := b.unchangedHome()
	for _, tc := range []struct {
		args []string
		exit int
		says string
	}{
		{[]string{"--from-account", "gamma"}, 5, "the account remote holds no fork called gamma"},
		{[]string{"--from-account", "alpha"}, 6, "the library already holds"},
		{[]string{"--from-account", "beta", "--keep-local"}, 6, "a symlink, so beta was not installed"},
		{[]string{s.url, "--keep-local"}, 1, "--keep-local applies to --from-account only"},
	} {
		out := b.run(append([]string{"--json", "skill", "add"}, tc.args...)...)
		equal(t, strings.Join(tc.args, " ")+": exit", out.exit, tc.exit)
		contains(t, strings.Join(tc.args, " ")+": error", b.one(out.stdout, "error")["message"].(string), tc.says)
	}
	unchanged.check(t, b, "the refusals", 0)

	b.mustRun("skill", "add", "--from-account", "alpha", "--keep-local")
	rootA := filepath.Join(b.agentx, "worktrees", "alpha")
	equal(t, "b's alpha", b.ref(lineage.ForkRef("alpha")), a.ref(lineage.ForkRef("alpha")))
	equal(t, "alpha's uncommitted edits", gitIn(t, b, rootA, "status", "--porcelain"), " M alpha/SKILL.md\n D alpha/notes.md\n")
	equal(t, "the kept SKILL.md", fileBody(t, filepath.Join(rootA, "alpha", "SKILL.md")), "---\nname: alpha\ndescription: Mine\n---\n")
	equal(t, "the ignored .DS_Store", fileBody(t, filepath.Join(rootA, "alpha", ".DS_Store")), "finder data\n")
	equal(t, "alpha's state", b.listed("alpha")["state"], stateModified)

	if err := os.Remove(betaLib); err != nil {
		t.Fatal(err)
	}
	b.mustRun("skill", "add", s.url, "--skill", "beta")
	writeFile(t, filepath.Join(betaLib, "notes.md"), "mine\n")
	out := b.run("--json", "skill", "add", "--from-account", "beta")
	equal(t, "an edited copy: exit", out.exit, 6)
	contains(t, "its error", b.one(out.stdout, "error")["message"].(string), "beta holds edits at")
	contains(t, "its hint", b.one(out.stdout, "error")["hint"].(string), "--keep-local")
	b.mustRun("skill", "add", "--from-account", "beta", "--keep-local")
	equal(t, "beta's import branch", b.ref(lineage.ManagedRef("beta")), "")
	equal(t, "beta's uncommitted edits", gitIn(t, b, filepath.Join(b.agentx, "worktrees", "beta"), "status", "--porcelain"), " M beta/notes.md\n")
	equal(t, "the kept notes", fileBody(t, filepath.Join(b.forkDir("beta", "beta"), "notes.md")), "mine\n")
}

// TestFromAccountPlan is what an install from the account remote does
// with what the library path holds, with and without --keep-local.
func TestFromAccountPlan(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		holds     libraryHolds
		keepLocal bool
		want      accountAction
		refusal   string // a word of the refusal, "" for none
	}{
		{holdsNothing, false, installFresh, ""},
		{holdsNothing, true, installFresh, ""},
		{holdsCopy, false, installSupersede, ""},
		{holdsCopy, true, installSupersede, ""},
		{holdsEditedCopy, false, 0, "holds edits at"},
		{holdsEditedCopy, true, installKeepLocal, ""},
		{holdsDirectory, false, 0, "the library already holds"},
		{holdsDirectory, true, installKeepLocal, ""},
		{holdsLink, false, 0, "a symlink"},
		{holdsLink, true, 0, "a symlink"},
		{holdsFile, false, 0, "neither a directory nor a symlink"},
		{holdsFile, true, 0, "neither a directory nor a symlink"},
	} {
		got, f := fromAccountPlan("notes", "/lib/notes", tc.holds, tc.keepLocal)
		switch {
		case tc.refusal == "" && f != nil:
			t.Errorf("%d, keep %v: refused: %s", tc.holds, tc.keepLocal, f.message)
		case tc.refusal != "" && f == nil:
			t.Errorf("%d, keep %v: not refused, want %q", tc.holds, tc.keepLocal, tc.refusal)
		case f != nil:
			contains(t, "the refusal", f.message, tc.refusal)
			equal(t, "exit", f.status.exit, 6)
		default:
			equal(t, "the action", got, tc.want)
		}
	}
}

// TestFromAccountRecoversWhereItWasKilled kills an install from the
// account remote with SIGKILL at two of its boundaries: once its journal
// is on disk, before anything changed, and right after git added the
// worktree. Every boundary in between is the journal's, which the home
// package replays without git. The next command finishes it: the branch at
// the remote tip, the worktree on it and clean, the library symlink and
// the placement, nothing staged left behind.
func TestFromAccountRecoversWhereItWasKilled(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, script string }{
		{"once the journal is on disk", killedUpdateScript},
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
			a, b, _, _ := accountHomes(t)
			b.mustRun("source", "add", a.listed("alpha")["source"].(string))
			out := killedChild(t, b, "TestInstallChildProcess", installChildEnv, "--from-account\nalpha", tc.script)
			_, kinds := journalKinds(t, b)
			equal(t, "the journal's steps", kinds, "ref, worktree, publish, link, link")
			if got := b.run("skill", "list"); got.exit != 0 {
				t.Fatalf("the command after the killed install: exit %d\n%s\nthe killed run:\n%s", got.exit, got.stderr, out)
			}
			equal(t, "journals after recovery", journalCount(t, b), 0)
			equal(t, "b's alpha", b.ref(lineage.ForkRef("alpha")), a.ref(lineage.ForkRef("alpha")))
			root := filepath.Join(b.agentx, "worktrees", "alpha")
			equal(t, "git status in the worktree", gitIn(t, b, root, "status", "--porcelain"), "")
			if target, _ := os.Readlink(filepath.Join(b.home, ".claude", "skills", "alpha")); target != filepath.Join(b.library, "alpha") {
				t.Errorf("claude-code's placement points at %q", target)
			}
			equal(t, "kind", b.listed("alpha")["kind"], lineage.KindFork)
			for _, dir := range []string{b.library, filepath.Join(b.agentx, "worktrees")} {
				equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
			}
		})
	}
}
