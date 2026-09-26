package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
)

// legacyHarness is a machine with Claude Code and Cursor and a source whose
// skill nc is stored with mode 100664, a file at the top and one in a
// directory below, which git mktree takes and ls-tree reads back as 100644:
// the tree keeps an id of its own that no directory on disk has. canonical
// is the id git writes today for the same files. nc is installed and placed
// in both configurations.
func legacyHarness(t *testing.T) (h *harness, s *sourceRepo, canonical string) {
	t.Helper()
	h = newHarness(t)
	h.build(t, fixture{dirs: []string{".claude", ".cursor"}})
	s = h.newSourceRepo("legacy", true)
	s.skill("skills/nc", "nc", "Stored with a legacy mode", map[string]string{"a.md": "a\n", "sub/b.md": "b\n"})
	s.commit("nc")
	sub := s.mktree("100664 blob " + s.run("rev-parse", "HEAD:skills/nc/sub/b.md") + "\tb.md")
	nc := s.mktree(
		"100644 blob "+s.run("rev-parse", "HEAD:skills/nc/SKILL.md")+"\tSKILL.md",
		"100664 blob "+s.run("rev-parse", "HEAD:skills/nc/a.md")+"\ta.md",
		"040000 tree "+sub+"\tsub")
	skills := s.mktree(s.replaced("HEAD:skills", "nc", nc)...)
	root := s.mktree(s.replaced("HEAD^{tree}", "skills", skills)...)
	canonical = s.tree("skills/nc")
	s.bare("update-ref", "refs/heads/main", s.bare("commit-tree", root, "-p", "HEAD", "-m", "legacy modes"))
	if s.tree("skills/nc") == canonical {
		t.Fatal("the rewritten tree has the canonical id; the fixture proves nothing")
	}
	contains(t, "the source listing", s.bare("ls-tree", "HEAD:skills/nc"), "100644 blob "+s.run("rev-parse", "HEAD:skills/nc/a.md")+"\ta.md")
	h.mustRun("source", "add", s.url)
	h.mustRun("skill", "add", s.url, "--skill", "nc")
	return h, s, canonical
}

// storeInOlderForm makes the import branch of nc what an agentx that reused
// a source's own tree whole wrote for the version it holds: the commit the
// branch holds, author, dates and message and all, over the source's tree
// with its legacy modes. It returns that commit and the one the branch
// held, which is the commit an install of the version writes today.
func (h *harness) storeInOlderForm(t *testing.T, s *sourceRepo) (legacy, canonical string) {
	t.Helper()
	ctx := context.Background()
	r := gitx.New(h.env, false, func(string, ...any) {})
	repo := gitx.AccountRepoPath(h.agentx)
	canonical = h.accountGit("rev-parse", "refs/heads/managed/nc")
	wrapped, err := r.IsolatedInput(ctx, repo, strings.NewReader("040000 tree "+s.tree("skills/nc")+"\tnc\n"), "mktree")
	if err != nil {
		t.Fatal(err)
	}
	tree := "tree " + h.accountGit("rev-parse", canonical+"^{tree}") + "\n"
	body := h.accountGit("cat-file", "commit", canonical) + "\n"
	if !strings.HasPrefix(body, tree) {
		t.Fatalf("the import commit does not start with its tree:\n%s", body)
	}
	body = "tree " + strings.TrimSpace(wrapped) + "\n" + strings.TrimPrefix(body, tree)
	out, err := r.IsolatedInput(ctx, repo, strings.NewReader(body), "hash-object", "-t", "commit", "-w", "--stdin")
	if err != nil {
		t.Fatal(err)
	}
	legacy = strings.TrimSpace(out)
	h.accountGit("update-ref", "refs/heads/managed/nc", legacy, canonical)
	equal(t, "the state over a branch stored in an older form", h.listed("nc")["state"], stateModified)
	return legacy, canonical
}

// ncShort is the short upstream commit nc's base version is named by.
func ncShort(h *harness) string {
	return h.accountGit("log", "-1", "--format=%(trailers:key=Agentx-Upstream-Commit,valueonly)", "refs/heads/managed/nc")[:7]
}

// TestASkillStoredWithALegacyModeInstallsCurrent: the import writes the
// tree git writes today for a source that stores a legacy mode, so the
// skill lists as current straight after the install, the diff agrees, and
// an edit reverts back to current.
//
// A branch an earlier agentx wrote holds the source's own tree instead,
// which no directory is current against. A revert of an edit lays out the
// version it holds and stores the branch again, at the commit an install
// writes today, so the skill is current afterwards and every command says
// so.
func TestASkillStoredWithALegacyModeInstallsCurrent(t *testing.T) {
	t.Parallel()
	h, s, canonical := legacyHarness(t)
	equal(t, "state after the install", h.listed("nc")["state"], stateCurrent)
	equal(t, "the import tree", h.accountGit("rev-parse", "refs/heads/managed/nc:nc"), canonical)
	short := ncShort(h)
	matches := "nc matches its base version at " + short + "\n"
	equal(t, "the diff", h.mustRun("skill", "diff", "nc").stdout, matches)

	file := filepath.Join(h.library, "nc", "a.md")
	writeFile(t, file, "an edit\n")
	equal(t, "state after an edit", h.listed("nc")["state"], stateModified)
	contains(t, "the revert", h.mustRun("skill", "revert", "nc").stdout, "✓ reverted nc to its base version at "+short+"\n")
	equal(t, "a.md after the revert", fileBody(t, file), "a\n")
	equal(t, "state after the revert", h.listed("nc")["state"], stateCurrent)

	_, tip := h.storeInOlderForm(t, s)
	writeFile(t, file, "another edit\n")
	diffs := h.eventsOfType(h.mustRun("--json", "skill", "diff", "nc").stdout, "diff")
	if len(diffs) != 1 || diffs[0]["path"] != "a.md" {
		t.Errorf("the diff over the older branch = %v, want a.md alone", diffs)
	}
	contains(t, "the revert over the older branch", h.mustRun("skill", "revert", "nc").stdout, "✓ reverted nc to its base version at "+short+"\n")
	equal(t, "a.md after that revert", fileBody(t, file), "a\n")
	equal(t, "the import branch after that revert", h.accountGit("rev-parse", "refs/heads/managed/nc"), tip)
	equal(t, "the staging refs left", h.accountGit("for-each-ref", "refs/agentx/importing/"), "")
	equal(t, "state after that revert", h.listed("nc")["state"], stateCurrent)
	equal(t, "the diff after that revert", h.mustRun("skill", "diff", "nc").stdout, matches)
	equal(t, "a second revert", h.mustRun("skill", "revert", "nc").stdout, "nc already matches its base version at "+short+"; nothing was reverted\n")
}

// TestABranchStoredInAnOlderFormIsStoredAgain: over a branch an earlier
// agentx stored with the source's legacy modes, a library directory that
// holds every file of its base version lists as modified all the same,
// since no directory is current against that tree. The diff says the one
// difference is where the version is stored and names the revert that
// stores it again; the revert then moves the branch alone, to the commit an
// install writes today, and touches no file, even through a library entry
// that is a symlink, which a revert that lays the base out refuses.
func TestABranchStoredInAnOlderFormIsStoredAgain(t *testing.T) {
	t.Parallel()
	h, s, _ := legacyHarness(t)
	lib := filepath.Join(h.library, "nc")
	own := filepath.Join(t.TempDir(), "nc")
	if err := os.Rename(lib, own); err != nil {
		t.Fatal(err)
	}
	link(t, own, lib)
	before := libraryTree(t, own)
	legacy, tip := h.storeInOlderForm(t, s)
	short := ncShort(h)

	stored := "nc differs from its base version at " + short + " only in how the account repo stores it;" +
		" run 'agentx skill revert nc' to store it as git writes it today, which changes no file"
	equal(t, "the diff", h.mustRun("skill", "diff", "nc").stdout, stored+"\n")
	out := h.mustRun("--json", "skill", "diff", "nc").stdout
	equal(t, "diff events", len(h.eventsOfType(out, "diff")), 0)
	equal(t, "the diff's result", h.one(out, "result")["summary"], stored)
	equal(t, "the import branch after the diff", h.accountGit("rev-parse", "refs/heads/managed/nc"), legacy)

	equal(t, "the revert", h.mustRun("skill", "revert", "nc").stdout,
		"✓ nc already matches its base version at "+short+"; nothing was reverted,"+
			" and its import branch now stores that version as git writes it today\n")
	equal(t, "the import branch", h.accountGit("rev-parse", "refs/heads/managed/nc"), tip)
	equal(t, "the staging refs left", h.accountGit("for-each-ref", "refs/agentx/importing/"), "")
	if target, err := os.Readlink(lib); err != nil || target != own {
		t.Errorf("the library entry after the revert: %q, %v; want the link to %s", target, err, own)
	}
	sameTree(t, "the directory the link leads to", libraryTree(t, own), before)
	equal(t, "what is left beside the library", strings.Join(hiddenEntries(t, h.library), " "), "")
	equal(t, "state after the revert", h.listed("nc")["state"], stateCurrent)
	equal(t, "the diff after the revert", h.mustRun("skill", "diff", "nc").stdout, "nc matches its base version at "+short+"\n")
	equal(t, "a second revert", h.mustRun("skill", "revert", "nc").stdout, "nc already matches its base version at "+short+"; nothing was reverted\n")
}

// TestInstallingTheSameVersionStoresAnOlderBranchAgain: installing the
// version a branch an earlier agentx stored in an older form holds is the
// same version installed again, told by its four trailers rather than by
// the commit: the branch moves, from the commit it holds, to the one the
// install writes today, and the skill is current. With the library
// directory gone as well, the install lays it out again.
func TestInstallingTheSameVersionStoresAnOlderBranchAgain(t *testing.T) {
	t.Parallel()
	h, s, _ := legacyHarness(t)
	_, tip := h.storeInOlderForm(t, s)

	out := h.run("--json", "skill", "add", s.url, "--skill", "nc")
	if out.exit != 0 {
		t.Fatalf("add: exit %d\n%s", out.exit, out.stderr)
	}
	contains(t, "the summary", h.one(out.stdout, "result")["summary"].(string), "adopted nc from "+s.url)
	equal(t, "the import branch", h.accountGit("rev-parse", "refs/heads/managed/nc"), tip)
	equal(t, "state", h.listed("nc")["state"], stateCurrent)

	h.storeInOlderForm(t, s)
	remove(t, filepath.Join(h.library, "nc"))
	h.mustRun("skill", "add", s.url, "--skill", "nc")
	equal(t, "the import branch with the directory gone", h.accountGit("rev-parse", "refs/heads/managed/nc"), tip)
	equal(t, "a.md", fileBody(t, filepath.Join(h.library, "nc", "a.md")), "a\n")
	equal(t, "state with the directory gone", h.listed("nc")["state"], stateCurrent)
	equal(t, "journals", journalCount(t, h), 0)
}

// TestSkillRemoveOfAnAbsentSkillJudgesACopyByItsBaseFiles: with the library
// directory gone, a recorded copy is compared with the base version the
// branch holds, and over a branch stored in an older form, which no
// directory is current against, a copy holding every file of that version
// goes without a word. A copy edited where it is still goes with the
// warning.
func TestSkillRemoveOfAnAbsentSkillJudgesACopyByItsBaseFiles(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, edit, warning string
	}{
		{"unchanged", "", ""},
		{"edited", "an edit in the copy\n", "cursor's copy of nc was different from its base version; removing it deleted those changes (%s)"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, s, _ := legacyHarness(t)
			h.mustRun("skill", "remove", "nc", "--from", "cursor")
			h.mustRun("skill", "place", "nc", "--to", "cursor", "--copy")
			cursor := filepath.Join(h.home, ".cursor", "skills", "nc")
			if c.edit != "" {
				writeFile(t, filepath.Join(cursor, "a.md"), c.edit)
			}
			h.storeInOlderForm(t, s)
			remove(t, filepath.Join(h.library, "nc"))

			out := h.run("--json", "skill", "remove", "nc")
			if out.exit != 0 {
				t.Fatalf("remove: exit %d\n%s", out.exit, out.stderr)
			}
			want := c.warning
			if want != "" {
				want = fmt.Sprintf(want, cursor)
			}
			equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), want)
			nothingAt(t, "cursor's copy", cursor)
			equal(t, "the import branch", refValue(t, h, "refs/heads/managed/nc"), "")
		})
	}
}

// TestSkillRevertOfAnOlderBranchRecovers kills a revert that stores its
// branch again at each durable boundary: once its journal is on disk,
// before the branch moved, and right after the branch moved, before any
// path changed; and then, the branch moved, after each path step. A
// revert whose library already holds the base's files has the branch
// alone to move. The next command recovers each one, and the revert is
// then whole: the library and the copy hold the base, the branch holds the
// commit an install writes today, and the skill is current.
func TestSkillRevertOfAnOlderBranchRecovers(t *testing.T) {
	t.Parallel()
	const journalOnDisk = `
for f in %MUTATIONS%/*.json; do
	if [ -e "$f" ]; then
		kill -9 $PPID
		exit 1
	fi
done
exec %GIT% "$@"
`
	const branchMoved = `
case " $* " in
*" update-ref "*)
	%GIT% "$@"
	status=$?
	kill -9 $PPID
	exit $status
	;;
esac
exec %GIT% "$@"
`
	type revertCase struct {
		name   string
		edit   bool   // the library and the copy hold an edit the revert discards
		script string // where the revert is killed
		moved  bool   // the branch moved before the kill
		stop   int    // the path steps applied after the kill, the branch moved first
	}
	cases := []revertCase{
		{name: "the branch alone, before it moved", script: journalOnDisk},
		{name: "the branch alone, after it moved", script: branchMoved, moved: true},
		{name: "before the branch moved", edit: true, script: journalOnDisk},
		{name: "after the branch moved", edit: true, script: branchMoved, moved: true},
	}
	for stop := 1; stop <= 4; stop++ { // the library's remove and publish, then the copy's
		cases = append(cases, revertCase{name: fmt.Sprintf("after %d path steps", stop), edit: true, script: journalOnDisk, stop: stop})
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, s, _ := legacyHarness(t)
			lib := filepath.Join(h.library, "nc")
			cursor := filepath.Join(h.home, ".cursor", "skills", "nc")
			base := libraryTree(t, lib)
			if c.edit {
				editLibrary(t, h, "nc", "a.md", "an edit\n")
			}
			h.mustRun("skill", "remove", "nc", "--from", "cursor")
			h.mustRun("skill", "place", "nc", "--to", "cursor", "--copy")
			legacy, tip := h.storeInOlderForm(t, s)

			out := killedRevertBy(t, h, "nc", c.script)
			steps := readJournal(t, h)
			var kinds []string
			for _, s := range steps {
				kinds = append(kinds, s.Kind)
			}
			want := "ref"
			if c.edit {
				want = "ref, remove, publish, remove, publish"
			}
			equal(t, "the journal's steps", strings.Join(kinds, ", "), want)
			branch := legacy
			if c.moved {
				branch = tip
			}
			equal(t, "the import branch when the revert was killed", h.accountGit("rev-parse", "refs/heads/managed/nc"), branch)
			if c.stop > 0 {
				h.accountGit("update-ref", "refs/heads/managed/nc", tip, legacy)
				applySteps(t, steps, c.stop)
			}

			if got := h.run("config", "set", "label", "recovered"); got.exit != 0 {
				t.Fatalf("the command after the killed revert: exit %d\n%s\nthe killed run:\n%s", got.exit, got.stderr, out)
			}
			equal(t, "journals after recovery", journalCount(t, h), 0)
			equal(t, "the import branch", h.accountGit("rev-parse", "refs/heads/managed/nc"), tip)
			sameTree(t, "the library directory", libraryTree(t, lib), base)
			sameTree(t, "cursor's copy", libraryTree(t, cursor), base)
			for _, dir := range []string{h.library, filepath.Dir(cursor)} {
				equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
			}
			equal(t, "state", h.listed("nc")["state"], stateCurrent)
		})
	}
}

// TestARefusedRevertOfAnOlderBranchLeavesNoStagingRef: a revert over a
// branch stored in an older form writes the commit an install writes today
// before it takes the lock, held by a staging ref of its own. A git wrapper
// moves the branch, makes a fork of the name, or edits the library while
// the revert reads the base version, and the revert refuses under the
// lock, before it writes a journal. Nothing names the staging ref then, so
// the revert drops it: no ref is left under refs/agentx/importing/, the
// branch holds what it held or what the other writer wrote, and the edit
// is still there.
func TestARefusedRevertOfAnOlderBranchLeavesNoStagingRef(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		ref  string // the ref the wrapper writes; none edits the library instead
		fork bool   // the ref written is a fork of the name, at the commit the branch holds
	}{
		{name: "the branch moves", ref: "refs/heads/managed/nc"},
		{name: "a fork appears", ref: "refs/heads/skills/nc", fork: true},
		{name: "the library changes"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, s, _ := legacyHarness(t)
			file := filepath.Join(h.library, "nc", "a.md")
			writeFile(t, file, "an edit\n")
			legacy, _ := h.storeInOlderForm(t, s)
			real, err := exec.LookPath("git")
			if err != nil {
				t.Fatal(err)
			}
			repo := gitx.AccountRepoPath(h.agentx)
			branch, edit := legacy, "an edit\n"
			action := `printf 'an edit made meanwhile\n' > ` + shellWord(file)
			message := "nc changed while it was being reverted, so nothing was discarded"
			if c.ref != "" {
				written := legacy
				if !c.fork {
					written = h.accountGit("commit-tree", legacy+"^{tree}", "-p", legacy, "-m", "moved")
					branch = written
				}
				action = real + ` --git-dir=` + shellWord(repo) + ` update-ref ` + c.ref + ` ` + written + ` || exit 1`
				message = "the import branch refs/heads/managed/nc moved while nc was being reverted, so nothing was discarded"
			} else {
				edit = "an edit made meanwhile\n"
			}
			stubGit(t, h, `#!/bin/sh
case " $* " in
*" ls-tree "*) `+action+` ;;
esac
exec `+real+` "$@"
`)
			out := h.run("--json", "skill", "revert", "nc")
			equal(t, "exit", out.exit, 6)
			equal(t, "message", h.one(out.stdout, "error")["message"], message)
			equal(t, "a.md", fileBody(t, file), edit)
			equal(t, "the import branch", h.accountGit("rev-parse", "refs/heads/managed/nc"), branch)
			if c.fork {
				equal(t, "the fork written meanwhile", h.accountGit("rev-parse", c.ref), legacy)
			}
			equal(t, "journals", journalCount(t, h), 0)
			equal(t, "what is left beside the library", strings.Join(hiddenEntries(t, h.library), " "), "")
			equal(t, "the staging refs left", h.accountGit("for-each-ref", "refs/agentx/importing/"), "")
		})
	}
}
