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
// nothing and added no source, the skills machine a published, and
// installs one by name, then the rest with --all. skill list --remote
// fetches the account remote and lists alpha, beta and notes, made by
// skill new, as installable, each with the fork id and the upstream its own
// commits record, notes with none. Two branches pushed by hand are left
// out with a warning each: one whose name is outside the fork name
// grammar, and zeta, whose history records no fork id, which an install
// refuses too. Installing alpha creates b's branch at a's commit, so the
// fork keeps its id and its history, tracking the account remote's
// branch, where a publish of it before was exit 5 naming the install;
// checks its worktree out with the .gitignore a commit put beside the
// skill directory, so git status there is clean; links the library to it
// and places it into the enabled configuration. It adds the fork's
// upstream source, which b lacked, so that b's update check finds the
// source's next version for alpha. Listed again, the account remote holds
// two skills b has not installed. With an unmanaged directory at notes'
// library path, skill add --all installs beta and refuses notes, exit 6,
// with a warning and an error naming it; once the directory is gone, the
// next --all installs notes, with one fetch of the account remote and
// both hand-pushed branches left out with a warning each, and a third
// finds nothing left to install. alpha is refused as installed already,
// with skill place named once its worktree and library entry are gone.
func TestInstallAForkFromTheAccount(t *testing.T) {
	t.Parallel()
	a, b, s, remote := accountHomes(t)
	rootA := filepath.Join(a.agentx, "worktrees", "alpha")
	writeFile(t, filepath.Join(rootA, ".gitignore"), "*.log\n")
	gitIn(t, a, rootA, "add", ".gitignore")
	gitIn(t, a, rootA, "-c", "user.name=Grace Hopper", "-c", "user.email=grace@example.com", "commit", "-q", "-m", "Beside the skill")
	a.mustRun("skill", "new", "notes")
	a.mustRun("skill", "publish")
	tip := a.ref(lineage.ForkRef("alpha"))
	handMade := a.accountGit("-c", "user.name=Grace Hopper", "-c", "user.email=grace@example.com", "commit-tree", "-m", "By hand", tip+"^{tree}")
	a.accountGit("push", "-q", remote, handMade+":refs/heads/skills/zeta", handMade+":refs/heads/skills/Bad_Name")

	out := b.mustRun("--json", "skill", "list", "--remote")
	listed := map[string]jsonEvent{}
	for _, ev := range b.eventsOfType(out.stdout, "installable_skill") {
		listed[ev["name"].(string)] = ev
	}
	equal(t, "installable", strings.Join(slices.Sorted(maps.Keys(listed)), " "), "alpha beta notes")
	contains(t, "the warnings", out.stderr, "skills/Bad_Name is left out, since it cannot be installed")
	contains(t, "the warnings", out.stderr, "skills/zeta is left out, since its history records no fork id")
	alpha := listed["alpha"]
	equal(t, "alpha's commit", alpha["commit"], tip)
	equal(t, "alpha's fork id", alpha["fork_id"], a.listed("alpha")["fork_id"])
	equal(t, "alpha's source", alpha["source"], "file://"+remote)
	equal(t, "alpha's upstream", alpha["upstream"], s.url)
	equal(t, "alpha's upstream subpath", alpha["upstream_subpath"], "skills/alpha")
	equal(t, "alpha's upstream commit", alpha["upstream_commit"], a.listed("alpha")["upstream_commit"])
	equal(t, "alpha's base hash", alpha["base_hash"], a.listed("alpha")["base_hash"])
	equal(t, "notes' source", listed["notes"]["source"], "file://"+remote)
	if _, ok := listed["notes"]["upstream"]; ok {
		t.Errorf("notes, made by skill new, is listed with an upstream: %v", listed["notes"])
	}
	text := b.mustRun("skill", "list", "--remote").stdout
	contains(t, "the text listing", text, "3 installable skills on the account remote")
	contains(t, "the text listing", text, "agentx skill add --name <name>")

	out = b.run("--json", "skill", "add", "--name", "zeta")
	equal(t, "a branch with no fork id: exit", out.exit, 6)
	contains(t, "its error", b.one(out.stdout, "error")["message"].(string), "records no fork id")

	out = b.run("--json", "skill", "publish", "alpha")
	equal(t, "a publish of a fork not installed: exit", out.exit, 5)
	contains(t, "its hint", b.one(out.stdout, "error")["hint"].(string), "install it with 'agentx skill add --name alpha'")

	out = b.mustRun("--json", "skill", "add", "--name", "alpha")
	equal(t, "b's alpha", b.ref(lineage.ForkRef("alpha")), tip)
	ev := b.librarySkill(out.stdout, "alpha")
	equal(t, "kind", ev["kind"], lineage.KindManaged)
	equal(t, "fork id", ev["fork_id"], alpha["fork_id"])
	equal(t, "state", ev["state"], stateCurrent)
	equal(t, "source", ev["source"], "file://"+remote)
	equal(t, "upstream", ev["upstream"], s.url)
	equal(t, "upstream subpath", ev["upstream_subpath"], "skills/alpha")
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
	update := b.updateOf(out.stdout, "alpha")
	equal(t, "the update b's check finds", update["kind"], lineage.KindManaged)
	equal(t, "its source", update["source"], "file://"+remote)
	equal(t, "its upstream", update["upstream"], s.url)

	out = b.mustRun("--json", "skill", "list", "--remote")
	equal(t, "installable once alpha is installed", len(b.eventsOfType(out.stdout, "installable_skill")), 2)
	notesLib := filepath.Join(b.library, "notes")
	if err := os.MkdirAll(notesLib, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(notesLib, "SKILL.md"), "---\nname: notes\ndescription: Mine\n---\n")
	out = b.run("--json", "skill", "add", "--all")
	equal(t, "--all with notes refused: exit", out.exit, 6)
	contains(t, "its warning", out.stderr, "notes: the library already holds")
	var some []string
	for _, ev := range b.eventsOfType(out.stdout, "library_skill") {
		some = append(some, ev["name"].(string))
	}
	equal(t, "what --all installed", strings.Join(some, " "), "beta")
	contains(t, "its error", b.one(out.stdout, "error")["message"].(string), "1 of 2 skills could not be installed: notes: the library already holds")
	if err := os.RemoveAll(notesLib); err != nil {
		t.Fatal(err)
	}
	out = b.mustRun("--json", "skill", "add", "--all")
	fetches := 0
	for _, ev := range b.eventsOfType(out.stdout, "progress") {
		if ev["phase"] == "fetch" {
			fetches++
		}
	}
	equal(t, "fetches of the account remote", fetches, 1)
	var all []string
	for _, ev := range b.eventsOfType(out.stdout, "library_skill") {
		all = append(all, ev["name"].(string))
	}
	equal(t, "what --all installed", strings.Join(all, " "), "notes")
	contains(t, "the warnings", out.stderr, "skills/zeta is left out, since its history records no fork id")
	contains(t, "the warnings", out.stderr, "skills/Bad_Name is left out, since it cannot be installed")
	contains(t, "the result", b.one(out.stdout, "result")["summary"].(string), "installed notes from the account remote")
	contains(t, "--all with nothing left", b.mustRun("skill", "add", "--all").stdout, "holds no skill this machine lacks")
	out = b.run("--json", "skill", "add", "--name", "alpha")
	equal(t, "installing alpha again: exit", out.exit, 6)
	e := b.one(out.stdout, "error")
	contains(t, "its error", e["message"].(string), "alpha is already installed on this machine")
	contains(t, "its hint", e["hint"].(string), "run 'agentx skill update alpha' to take in what the account remote holds of it")

	// Without its worktree and library entry, the branch is the fork's
	// alone, and skill place lays it out.
	if err := os.Remove(filepath.Join(b.library, "alpha")); err != nil {
		t.Fatal(err)
	}
	b.accountGit("worktree", "remove", "--force", "--force", filepath.Join(b.agentx, "worktrees", "alpha"))
	out = b.run("--json", "skill", "add", "--name", "alpha")
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
// which a warning says places nothing.
func TestFromAccountSupersedesAnUnmodifiedCopy(t *testing.T) {
	t.Parallel()
	_, b, s, _ := accountHomes(t)
	if err := os.MkdirAll(filepath.Join(b.home, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	b.mustRun("skill", "add", s.url, "--name", "alpha")
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
	out := b.mustRun("--json", "skill", "add", "--name", "alpha", "--to", "cursor")
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
	equal(t, "kind", ev["kind"], lineage.KindManaged)
	equal(t, "state", ev["state"], stateCurrent)
	contains(t, "the result", b.one(out.stdout, "result")["summary"].(string), "it replaces the managed skill wherever it was")
	equal(t, "what is left beside the library", strings.Join(hiddenEntries(t, b.library), " "), "")
}

// TestAddFromAccountRefuses installs from the account remote on machine
// b over what its library holds. Each refusal changes nothing, the
// settings included, which lack the skills' source: a name agentx cannot
// use for a skill, exit 6; a skill the account remote does not hold,
// exit 5; an unmanaged directory at alpha's library path, exit 6, and a
// publish of it says to move it aside; a symlink at beta's, exit 6;
// --all over both, exit 6 naming each; an --except naming nothing to
// install, exit 5, and one that leaves nothing, exit 1; a bare skill add, exit 1 with a hint naming
// skill list --remote, and --fetch with no source, exit 1; and a
// directory where alpha's worktree goes, exit 6. Then a managed beta
// whose library directory is gone, exit 6; an edited managed copy of
// beta's upstream whose update merge is pending, exit 4, and once it is
// aborted, exit 6, with a hint that keeps the edits out of harm's way.
func TestAddFromAccountRefuses(t *testing.T) {
	t.Parallel()
	_, b, s, _ := accountHomes(t)
	alphaLib, betaLib := filepath.Join(b.library, "alpha"), filepath.Join(b.library, "beta")
	if err := os.MkdirAll(alphaLib, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(alphaLib, "SKILL.md"), "---\nname: alpha\ndescription: Mine\n---\n")
	if err := os.Symlink(alphaLib, betaLib); err != nil {
		t.Fatal(err)
	}
	unchanged := b.unchangedHome()
	for _, tc := range []struct {
		args []string
		exit int
		says string
	}{
		{[]string{"--name", "Bad_Name"}, 6, "is not a valid skill name"},
		{[]string{"--name", "gamma"}, 5, "the account remote holds no skill called gamma"},
		{[]string{"--name", "alpha"}, 6, "the library already holds"},
		{[]string{"--name", "beta"}, 6, "a symlink, so beta was not installed"},
		{[]string{"--all"}, 6, "2 of 2 skills could not be installed"},
		{[]string{"--all", "--except", "gamma"}, 5, `holds no skill this machine lacks called "gamma"`},
		{[]string{"--all", "--except", "alpha", "--except", "beta"}, 1, "--except left no skill to install"},
		{nil, 1, "skill add needs a source, or --name or --all"},
		{[]string{"--name", "alpha", "--fetch"}, 1, "--fetch applies to a source"},
	} {
		out := b.run(append([]string{"--json", "skill", "add"}, tc.args...)...)
		equal(t, strings.Join(tc.args, " ")+": exit", out.exit, tc.exit)
		e := b.one(out.stdout, "error")
		contains(t, strings.Join(tc.args, " ")+": error", e["message"].(string), tc.says)
		if tc.args == nil {
			contains(t, "a bare skill add: hint", e["hint"].(string), "agentx skill list --remote")
		}
	}
	out := b.run("--json", "skill", "publish", "alpha")
	equal(t, "a publish of the unmanaged alpha: exit", out.exit, 6)
	contains(t, "its hint", b.one(out.stdout, "error")["hint"].(string), "aside and install the one you published")
	unchanged.check(t, b, "the refusals", 0)
	rootA := filepath.Join(b.agentx, "worktrees", "alpha")
	if err := os.MkdirAll(rootA, 0o755); err != nil {
		t.Fatal(err)
	}
	out = b.run("--json", "skill", "add", "--name", "alpha")
	equal(t, "a directory in the worktree's way: exit", out.exit, 6)
	contains(t, "its error", b.one(out.stdout, "error")["message"].(string), "already exists, so alpha was not installed")

	if err := os.Remove(betaLib); err != nil {
		t.Fatal(err)
	}
	b.mustRun("skill", "add", s.url, "--name", "beta")
	aside := filepath.Join(b.home, "beta-aside")
	if err := os.Rename(betaLib, aside); err != nil {
		t.Fatal(err)
	}
	out = b.run("--json", "skill", "add", "--name", "beta")
	equal(t, "a managed beta the library lost: exit", out.exit, 6)
	contains(t, "its error", b.one(out.stdout, "error")["message"].(string), "the library no longer holds it")
	if err := os.Rename(aside, betaLib); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(betaLib, "notes.md"), forkNotes("seven", "seven, mine"))
	// An update merge pending comes first: the library directory is the
	// merge's until it is resolved or given up.
	s.skill("skills/beta", "beta", "The second skill", map[string]string{"notes.md": forkNotes("seven", "seven, upstream")})
	s.commit("second version")
	b.mustRun("skill", "check-updates")
	if out := b.run("skill", "update", "beta"); out.exit != 4 {
		t.Fatalf("beta's update: exit %d, want a conflict\n%s", out.exit, out.stderr)
	}
	out = b.run("--json", "skill", "add", "--name", "beta")
	equal(t, "a pending merge: exit", out.exit, 4)
	contains(t, "its error", b.one(out.stdout, "error")["message"].(string), "replaced by the account remote's skill")
	b.mustRun("skill", "update", "beta", "--abort")
	out = b.run("--json", "skill", "add", "--name", "beta")
	equal(t, "an edited copy: exit", out.exit, 6)
	e := b.one(out.stdout, "error")
	contains(t, "its error", e["message"].(string), "the library already holds")
	equal(t, "its hint", e["hint"], "move it aside, or run 'agentx skill remove beta', then run 'agentx skill add --name beta' again")
}

// TestFromAccountPlan is what an install from the account remote does
// with what the library path holds.
func TestFromAccountPlan(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		holds   libraryHolds
		want    accountAction
		refusal string // a word of the refusal, "" for none
	}{
		{holdsNothing, installFresh, ""},
		{holdsCopy, installSupersede, ""},
		{holdsDirectory, 0, "the library already holds"},
		{holdsLink, 0, "a symlink"},
		{holdsFile, 0, "neither a directory nor a symlink"},
	} {
		got, f := fromAccountPlan("notes", "/lib/notes", tc.holds)
		switch {
		case tc.refusal == "" && f != nil:
			t.Errorf("%d: refused: %s", tc.holds, f.message)
		case tc.refusal != "" && f == nil:
			t.Errorf("%d: not refused, want %q", tc.holds, tc.refusal)
		case f != nil:
			contains(t, "the refusal", f.message, tc.refusal)
			equal(t, "exit", f.status.exit, 6)
		default:
			equal(t, "the action", got, tc.want)
		}
	}
}

// TestHeldUnderAnotherName: an account remote branch is a skill this
// machine holds under another name when a skill of the same fork id holds
// its tip, as the renamed skill does of its old name's branch; one whose
// tip is ahead of that skill's, or of another fork id, is not.
func TestHeldUnderAnotherName(t *testing.T) {
	t.Parallel()
	history := map[string]bool{"old new": true} // old is in new's history
	ancestor := func(a, b string) bool { return history[a+" "+b] }
	for _, tc := range []struct {
		name  string
		local map[string][]string // the tips of the skills here, by fork id
		tip   string              // the tip of the branch, whose fork id is id-1
		want  bool
	}{
		{"the same id, its tip in the local tip's history", map[string][]string{"id-1": {"new"}}, "old", true},
		{"the same id, its tip the local tip", map[string][]string{"id-1": {"new"}}, "new", true},
		{"the same id, its tip ahead", map[string][]string{"id-1": {"old"}}, "new", false},
		{"another id", map[string][]string{"id-2": {"new"}}, "old", false},
	} {
		equal(t, tc.name, heldUnderAnotherName(tc.local, "id-1", tc.tip, ancestor), tc.want)
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
			out := killedChild(t, b, "TestInstallChildProcess", installChildEnv, "--name\nalpha", tc.script)
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
			equal(t, "kind", b.listed("alpha")["kind"], lineage.KindManaged)
			equal(t, "the branch's tracking", b.accountGit("config", "--get", "branch.skills/alpha.remote"), accountRemoteName(t, b))
			for _, dir := range []string{b.library, filepath.Join(b.agentx, "worktrees")} {
				equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
			}
		})
	}
}
