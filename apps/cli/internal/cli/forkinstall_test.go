package cli

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// TestInstallAForkFromTheAccount lists on machine b, which installed
// nothing and added no source, the forks machine a published, and installs
// one. skill list --remote fetches the account remote and lists alpha,
// beta and the greenfield notes as installable, each with the fork id and
// the upstream its own commits record, notes with none. Two branches
// pushed by hand are left out with a warning each: one whose name is
// outside the fork name grammar, and zeta, whose history records no fork
// id, which an install refuses too. Installing alpha creates b's branch at
// a's commit, so the fork keeps its id and its history, tracking the
// account remote's branch, where a pull of it before was exit 5 naming the
// install; checks its worktree out with the .gitignore a commit put beside
// the skill directory, so git status there is clean; links the library to
// it and places it into the enabled configuration. It adds the fork's
// upstream source, which b lacked, so that b's update check finds the
// source's next version for alpha. Listed again, the account remote holds
// two forks b has not installed, and alpha is refused as installed
// already, with skill place named once its worktree and library entry are
// gone.
func TestInstallAForkFromTheAccount(t *testing.T) {
	t.Parallel()
	a, b, s, remote := accountHomes(t)
	rootA := filepath.Join(a.agentx, "worktrees", "alpha")
	writeFile(t, filepath.Join(rootA, ".gitignore"), "*.log\n")
	gitIn(t, a, rootA, "add", ".gitignore")
	gitIn(t, a, rootA, "-c", "user.name=Grace Hopper", "-c", "user.email=grace@example.com", "commit", "-q", "-m", "Beside the skill")
	a.mustRun("skill", "new", "notes")
	a.mustRun("publish", "--all")
	tip := a.ref(lineage.ForkRef("alpha"))
	handMade := a.accountGit("-c", "user.name=Grace Hopper", "-c", "user.email=grace@example.com", "commit-tree", "-m", "By hand", tip+"^{tree}")
	a.accountGit("push", "-q", remote, handMade+":refs/heads/skills/zeta", handMade+":refs/heads/skills/Bad_Name")

	out := b.mustRun("--json", "skill", "list", "--remote")
	listed := map[string]jsonEvent{}
	for _, ev := range b.eventsOfType(out.stdout, "installable_fork") {
		listed[ev["name"].(string)] = ev
	}
	equal(t, "installable", strings.Join(slices.Sorted(maps.Keys(listed)), " "), "alpha beta notes")
	contains(t, "the warnings", out.stderr, "skills/Bad_Name is not listed, since it cannot be installed")
	contains(t, "the warnings", out.stderr, "skills/zeta is not listed, since its history records no fork id")
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

	out = b.run("--json", "skill", "add", "--from-account", "zeta")
	equal(t, "a branch with no fork id: exit", out.exit, 6)
	contains(t, "its error", b.one(out.stdout, "error")["message"].(string), "records no fork id")

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
		accountRemoteName(t, b)+" refs/heads/skills/alpha")
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
	out = b.mustRun("--json", "skill", "check-updates")
	equal(t, "the update b's check finds", b.updateOf(out.stdout, "alpha")["kind"], lineage.KindFork)

	out = b.mustRun("--json", "skill", "list", "--remote")
	equal(t, "installable once alpha is installed", len(b.eventsOfType(out.stdout, "installable_fork")), 2)
	out = b.run("--json", "skill", "add", "--from-account", "alpha")
	equal(t, "installing alpha again: exit", out.exit, 6)
	contains(t, "its error", b.one(out.stdout, "error")["message"].(string), "alpha is already installed on this machine")

	// Without its worktree and library entry, the branch is the fork's
	// alone, and skill place lays it out.
	if err := os.Remove(filepath.Join(b.library, "alpha")); err != nil {
		t.Fatal(err)
	}
	b.accountGit("worktree", "remove", "--force", "--force", filepath.Join(b.agentx, "worktrees", "alpha"))
	out = b.run("--json", "skill", "add", "--from-account", "alpha")
	equal(t, "an unplaced fork: exit", out.exit, 6)
	contains(t, "its hint", b.one(out.stdout, "error")["hint"].(string), "agentx skill place alpha")
}

// TestFromAccountSupersedesAnUnmodifiedCopy installs alpha from the
// account remote on machine b, which installed the same upstream version
// of alpha from the source first, removed it from Cursor, left a .DS_Store
// in it, and had a check pin the source's next version for it. The copy
// holds its base version, so the fork takes its place: the import branch,
// its update candidate and the library directory go, the library entry is
// the symlink into the fork's worktree, which holds the .DS_Store, and the
// placements are as they were, Claude Code's leading to the fork and
// Cursor still without one, though the install was given --to cursor,
// which a warning says places nothing. A pull of the managed alpha before
// names the install, not a fork of b's own.
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
	b.mustRun("skill", "check-updates")
	if b.ref(lineage.CandidateRef("alpha")) == "" {
		t.Fatal("the check pinned no candidate for alpha")
	}
	claude := filepath.Join(b.home, ".claude", "skills", "alpha")
	before, _ := os.Readlink(claude)
	pulled := b.run("--json", "pull", "alpha")
	equal(t, "a pull of the managed alpha: exit", pulled.exit, 6)
	contains(t, "its hint", b.one(pulled.stdout, "error")["hint"].(string), "agentx skill add --from-account alpha")

	out := b.mustRun("--json", "skill", "add", "--from-account", "alpha", "--to", "cursor")
	contains(t, "the warning", out.stderr, "so --to and --copy place nothing")
	equal(t, "the import branch", b.ref(lineage.ManagedRef("alpha")), "")
	equal(t, "the candidate", b.ref(lineage.CandidateRef("alpha")), "")
	if state, err := home.State(filepath.Join(b.library, "alpha")); err != nil || !home.IsLink(state) {
		t.Errorf("the library entry is not the symlink into the fork's worktree: %q, %v", state, err)
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

// TestFromAccountRefusesAndKeepsLocal installs from the account remote on
// machine b over what its library holds. Each refusal changes nothing, the
// settings included, which lack the forks' source: a fork the account
// remote does not hold, exit 5; an unmanaged directory at alpha's library
// path, exit 6, and with --keep-local, while it holds no SKILL.md, exit 5;
// a symlink at beta's, exit 6 with --keep-local too, and a pull of it
// names the link to remove; a directory where alpha's worktree goes, exit
// 6; a managed beta whose library directory is gone, exit 6; an edited
// managed copy of beta's upstream whose update merge is pending, exit 4,
// and once it is aborted, exit 6; --keep-local without --from-account, and
// --from-account without a name, are exit 1. With --keep-local, the
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
		{[]string{"--from-account", "alpha", "--keep-local"}, 5, "holds no SKILL.md, so alpha was not installed and nothing was changed"},
		{[]string{"--from-account", "beta", "--keep-local"}, 6, "a symlink, so beta was not installed"},
		{[]string{s.url, "--keep-local"}, 1, "--keep-local applies to --from-account only"},
		{[]string{"--from-account", ""}, 1, "--from-account needs the name of a fork"},
	} {
		out := b.run(append([]string{"--json", "skill", "add"}, tc.args...)...)
		equal(t, strings.Join(tc.args, " ")+": exit", out.exit, tc.exit)
		contains(t, strings.Join(tc.args, " ")+": error", b.one(out.stdout, "error")["message"].(string), tc.says)
	}
	unchanged.check(t, b, "the refusals", 0)
	writeFile(t, filepath.Join(alphaLib, "SKILL.md"), "---\nname: alpha\ndescription: Mine\n---\n")
	out := b.run("--json", "pull", "beta")
	equal(t, "a pull of the linked beta: exit", out.exit, 6)
	contains(t, "its hint", b.one(out.stdout, "error")["hint"].(string), "remove the link")
	rootA := filepath.Join(b.agentx, "worktrees", "alpha")
	if err := os.MkdirAll(rootA, 0o755); err != nil {
		t.Fatal(err)
	}
	out = b.run("--json", "skill", "add", "--from-account", "alpha", "--keep-local")
	equal(t, "a directory in the worktree's way: exit", out.exit, 6)
	contains(t, "its error", b.one(out.stdout, "error")["message"].(string), "already exists, so alpha was not installed")
	if err := os.Remove(rootA); err != nil {
		t.Fatal(err)
	}

	b.mustRun("skill", "add", "--from-account", "alpha", "--keep-local")
	equal(t, "b's alpha", b.ref(lineage.ForkRef("alpha")), a.ref(lineage.ForkRef("alpha")))
	equal(t, "alpha's uncommitted edits", gitIn(t, b, rootA, "status", "--porcelain"), " M alpha/SKILL.md\n D alpha/notes.md\n")
	equal(t, "the kept SKILL.md", fileBody(t, filepath.Join(rootA, "alpha", "SKILL.md")), "---\nname: alpha\ndescription: Mine\n---\n")
	equal(t, "the ignored .DS_Store", fileBody(t, filepath.Join(rootA, "alpha", ".DS_Store")), "finder data\n")
	equal(t, "alpha's state", b.listed("alpha")["state"], stateModified)

	if err := os.Remove(betaLib); err != nil {
		t.Fatal(err)
	}
	b.mustRun("skill", "add", s.url, "--skill", "beta")
	aside := filepath.Join(b.home, "beta-aside")
	if err := os.Rename(betaLib, aside); err != nil {
		t.Fatal(err)
	}
	out = b.run("--json", "skill", "add", "--from-account", "beta")
	equal(t, "a managed beta the library lost: exit", out.exit, 6)
	contains(t, "its error", b.one(out.stdout, "error")["message"].(string), "the library no longer holds it")
	if err := os.Rename(aside, betaLib); err != nil {
		t.Fatal(err)
	}
	mine := forkNotes("seven", "seven, mine")
	writeFile(t, filepath.Join(betaLib, "notes.md"), mine)
	// An update merge pending comes first: --keep-local does not get past
	// it.
	s.skill("skills/beta", "beta", "The second skill", map[string]string{"notes.md": forkNotes("seven", "seven, upstream")})
	s.commit("second version")
	b.mustRun("skill", "check-updates")
	if out := b.run("skill", "update", "beta"); out.exit != 4 {
		t.Fatalf("beta's update: exit %d, want a conflict\n%s", out.exit, out.stderr)
	}
	out = b.run("--json", "skill", "add", "--from-account", "beta")
	equal(t, "a pending merge: exit", out.exit, 4)
	contains(t, "its error", b.one(out.stdout, "error")["message"].(string), "replaced by the fork")
	b.mustRun("skill", "update", "beta", "--abort")
	out = b.run("--json", "skill", "add", "--from-account", "beta")
	equal(t, "an edited copy: exit", out.exit, 6)
	contains(t, "its error", b.one(out.stdout, "error")["message"].(string), "beta holds edits at")
	contains(t, "its hint", b.one(out.stdout, "error")["hint"].(string), "--keep-local")
	b.mustRun("skill", "add", "--from-account", "beta", "--keep-local")
	equal(t, "beta's import branch", b.ref(lineage.ManagedRef("beta")), "")
	equal(t, "beta's update candidate", b.ref(lineage.CandidateRef("beta")), "")
	equal(t, "beta's uncommitted edits", gitIn(t, b, filepath.Join(b.agentx, "worktrees", "beta"), "status", "--porcelain"), " M beta/notes.md\n")
	equal(t, "the kept notes", fileBody(t, filepath.Join(b.forkDir("beta", "beta"), "notes.md")), mine)
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
			equal(t, "the branch's tracking", b.accountGit("config", "--get", "branch.skills/alpha.remote"), accountRemoteName(t, b))
			for _, dir := range []string{b.library, filepath.Join(b.agentx, "worktrees")} {
				equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
			}
		})
	}
}
