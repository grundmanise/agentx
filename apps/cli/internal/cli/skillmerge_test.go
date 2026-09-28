package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// editedNotes is the edit pendingMerge makes to alpha's notes.md, which the
// second version of alpha changes too.
const editedNotes = "alpha notes, edited here\n"

// pendingMerge leaves alpha of updateHarness with a merge pending: a check
// pins its second version, which changes notes.md, the library's notes.md
// is edited too, and the update conflicts.
func pendingMerge(t *testing.T, h *harness, s *sourceRepo) {
	t.Helper()
	checked(t, h, s)
	editLibrary(t, h, "alpha", "notes.md", editedNotes)
	if out := h.run("skill", "update", "alpha"); out.exit != 4 {
		t.Fatalf("the update that should conflict: exit %d\n%s%s", out.exit, out.stdout, out.stderr)
	}
}

// resolvedWithGit resolves the one file alpha's pending merge conflicts
// in, notes.md, to the update's side with plain git in the skill's
// directory in the checkout, as the hint says, and commits the merge too
// when commit is true.
func resolvedWithGit(t *testing.T, h *harness, commit bool) {
	t.Helper()
	skillDir := filepath.Join(pendingCheckout(h, "alpha"), "alpha-dir")
	gitIn(t, h, skillDir, "checkout", "--theirs", "notes.md")
	gitIn(t, h, skillDir, "add", "notes.md")
	if commit {
		gitIn(t, h, skillDir, "commit", "--quiet", "--no-edit")
	}
}

// conflictPaths is the paths of the files a conflict event names, in
// order.
func conflictPaths(ev jsonEvent) string {
	var paths []string
	for _, f := range ev["files"].([]any) {
		paths = append(paths, f.(map[string]any)["path"].(string))
	}
	return strings.Join(paths, ",")
}

// fileOf is the file at path of a conflict event.
func fileOf(t *testing.T, ev jsonEvent, path string) map[string]any {
	t.Helper()
	for _, f := range ev["files"].([]any) {
		if file := f.(map[string]any); file["path"] == path {
			return file
		}
	}
	t.Fatalf("the conflict names no %s: %v", path, ev["files"])
	return nil
}

// pendingCheckout is the checkout of the merge pending for the skill
// called name.
func pendingCheckout(h *harness, name string) string {
	return filepath.Join(h.agentx, "merges", name)
}

// checkoutGit runs git in the checkout of the merge pending for the skill
// called name, as agentx runs it there, and returns stdout as is.
func checkoutGit(t *testing.T, h *harness, name string, args ...string) string {
	t.Helper()
	return gitIn(t, h, pendingCheckout(h, name), args...)
}

// gitIn runs git in dir, a checkout or a directory in one, as agentx runs
// it in a checkout, and returns stdout as is.
func gitIn(t *testing.T, h *harness, dir string, args ...string) string {
	t.Helper()
	r := gitx.New(h.env, false, func(string, ...any) {})
	out, err := r.InCheckout(context.Background(), dir, args...)
	if err != nil {
		t.Fatalf("git %s in %s: %v", strings.Join(args, " "), dir, err)
	}
	return out
}

// mergeState is git's state of the merge pending for the skill called
// name: its HEAD, its MERGE_HEAD and its MERGE_MSG.
func mergeState(t *testing.T, h *harness, name string) (head, mergeHead, msg string) {
	t.Helper()
	head = strings.TrimSpace(checkoutGit(t, h, name, "rev-parse", "HEAD"))
	mergeHead = strings.TrimSpace(checkoutGit(t, h, name, "rev-parse", "MERGE_HEAD"))
	return head, mergeHead, fileBody(t, strings.TrimSpace(checkoutGit(t, h, name, "rev-parse", "--git-path", "MERGE_MSG")))
}

// unmerged is what the index of the checkout of the merge pending for the
// skill called name lists as unmerged: one line per stage, the stage and
// the path.
func unmerged(t *testing.T, h *harness, name string) string {
	t.Helper()
	return strings.TrimSpace(checkoutGit(t, h, name, "ls-files", "-u", "--format=%(stage) %(path)"))
}

// noCheckout fails the test when a merge is pending for the skill called
// name, by its checkout or by git's registration of one.
func noCheckout(t *testing.T, h *harness, name string) {
	t.Helper()
	nothingAt(t, "the checkout of "+name, pendingCheckout(h, name))
	if list := h.accountGit("worktree", "list", "--porcelain"); strings.Contains(list, pendingReason) {
		t.Errorf("git still knows a checkout of a pending merge:\n%s", list)
	}
}

// crlf is text with every line ending in CRLF, as git writes a file whose
// attributes ask for it.
func crlf(text string) string { return strings.ReplaceAll(text, "\n", "\r\n") }

// TestSkillUpdateMergesANonOverlappingEdit: a skill edited where its update
// changes nothing merges cleanly. The import branch moves to the candidate
// and the candidate goes, as for any update; the library directory holds
// the new version with the edits on top of it, every file the update
// added, deleted or made executable included, so the skill stays modified,
// and skill diff shows the edits and nothing else. A .DS_Store and a
// directory of the user's whose own ignore file names everything in it are
// files git ignores, not edits: they are not merged, and the library keeps
// them. Of the two copies, the one that still held the version installed
// is refreshed with the merged version, and none of the library's ignored
// files, and the one edited where it is kept with its warning. No merge is
// left pending.
func TestSkillUpdateMergesANonOverlappingEdit(t *testing.T) {
	t.Parallel()
	h, s, first := updateHarness(t)
	claude := filepath.Join(h.home, ".claude", "skills", "alpha")
	cursor := filepath.Join(h.home, ".cursor", "skills", "alpha")
	editCopy(t, cursor)
	edited := libraryTree(t, cursor)
	lib := filepath.Join(h.library, "alpha")
	skillMD := fileBody(t, filepath.Join(lib, "SKILL.md")) + "\nA line of my own.\n"
	editLibrary(t, h, "alpha", "SKILL.md", skillMD)
	editLibrary(t, h, "alpha", "mine.md", "a file of my own\n")
	private := filepath.Join(lib, "private")
	writeFile(t, mkdirs(t, private, ".gitignore"), "*\n")
	writeFile(t, filepath.Join(private, "notes.md"), "notes git would ignore\n")
	writeFile(t, filepath.Join(lib, ".DS_Store"), "finder data\n")
	second := newVersion(t, s)
	merged := secondTree(t, s)
	merged["SKILL.md"], merged["mine.md"] = skillMD, "a file of my own\n"
	want := withFile(withFile(withFile(merged, ".DS_Store", "finder data\n"),
		filepath.Join("private", ".gitignore"), "*\n"), filepath.Join("private", "notes.md"), "notes git would ignore\n")
	h.mustRun("skill", "check")
	tip, candidate := h.ref(lineage.ManagedRef("alpha")), h.ref(lineage.CandidateRef("alpha"))

	out := h.mustRun("--json", "skill", "update", "alpha")
	equal(t, "summary", h.one(out.stdout, "result")["summary"],
		"updated alpha from "+first[:7]+" to "+second[:7]+" and merged its edits cleanly, 1 copy placement refreshed, 1 placement skipped")
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), editedCopyWarning(cursor))
	equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), candidate)
	equal(t, "the candidate ref", h.ref(lineage.CandidateRef("alpha")), "")
	noCheckout(t, h, "alpha")
	contains(t, "the branch's reflog", h.accountGit("reflog", "show", "--format=%H", lineage.ManagedRef("alpha")), tip)
	sameTree(t, "the library directory", libraryTree(t, lib), want)
	if !executable(t, filepath.Join(lib, "scripts", "run.sh")) {
		t.Error("scripts/run.sh is not executable after the merge")
	}
	nothingAt(t, "old.md", filepath.Join(lib, "old.md"))
	sameTree(t, "claude's copy", libraryTree(t, claude), merged)
	sameTree(t, "cursor's copy", libraryTree(t, cursor), edited)
	ev := h.one(out.stdout, "library_skill")
	equal(t, "state", ev["state"], stateModified)
	equal(t, "upstream_commit", ev["upstream_commit"], second)
	for _, field := range []string{"candidate", "pending_merge"} {
		if _, ok := ev[field]; ok {
			t.Errorf("the merged skill carries %s: %v", field, ev[field])
		}
	}
	for _, dir := range []string{h.library, filepath.Dir(claude), filepath.Dir(cursor)} {
		equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
	}
	equal(t, "journals", journalCount(t, h), 0)

	sameEvent(t, "the update's library_skill and skill list's", ev, h.listed("alpha"))
	var diffs []string
	for _, d := range h.eventsOfType(h.mustRun("--json", "skill", "diff", "alpha").stdout, "diff") {
		diffs = append(diffs, d["path"].(string)+" "+d["status"].(string))
	}
	equal(t, "what skill diff shows", strings.Join(diffs, ", "), "SKILL.md modified, mine.md added")
}

// TestSkillUpdateLeavesAConflictPending: an edit the update overlaps
// conflicts. The update exits 4 and changes nothing agents read: the
// library directory and every copy are byte for byte as they were, and the
// import branch and the candidate hold what they held. The conflict event
// names the three versions and the stage blobs of the file, and the hint
// names the skill's directory in the checkout to resolve it in with git. The merge is an ordinary
// git merge in progress in the skill's checkout, read back with plain git:
// locked with agentx's reason, at mine, the library directory committed on
// the base version, with the candidate as MERGE_HEAD, the completion's
// message in MERGE_MSG, the three stages of the file in the index and
// zdiff3 markers in the file, which git wrote with the line endings the
// update's own .gitattributes asks for, as it writes any file of a
// checkout. A later invocation sees the merge pending in skill list, and
// the update run again before the file is resolved reports the same
// conflict, read from the checkout, in the same words, and changes
// nothing.
func TestSkillUpdateLeavesAConflictPending(t *testing.T) {
	t.Parallel()
	h, s, first := updateHarness(t)
	second := newVersion(t, s)
	h.mustRun("skill", "check")
	editLibrary(t, h, "alpha", "notes.md", editedNotes)
	tip, candidate := h.ref(lineage.ManagedRef("alpha")), h.ref(lineage.CandidateRef("alpha"))
	library, home := onDisk(t, h.library), onDisk(t, h.home)
	before := mutationVersion(t, h)

	out := h.run("--json", "skill", "update", "alpha")
	equal(t, "exit", out.exit, 4)
	e := h.one(out.stdout, "error")
	equal(t, "code", e["code"], "pending_merge")
	equal(t, "message", e["message"], "alpha conflicts with its update in 1 file, so the merge is pending and the library directory was left as it is")
	equal(t, "hint", e["hint"], "resolve it with git in "+filepath.Join(pendingCheckout(h, "alpha"), "alpha-dir")+" ('git add' each file you resolved, or 'git checkout --ours|--theirs <file>' then 'git add'; 'git commit' is optional), "+
		"then run 'agentx skill update alpha' again to apply it, or 'agentx skill update alpha --abort' to give it up")
	if got := h.eventsOfType(out.stdout, "library_skill"); len(got) != 0 {
		t.Errorf("an update that left a merge pending reported %v", got)
	}
	ev := h.one(out.stdout, "conflict")
	equal(t, "name", ev["name"], "alpha")
	equal(t, "kind", ev["kind"], "managed")
	equal(t, "base", ev["base"], tip)
	equal(t, "theirs", ev["theirs"], candidate)
	equal(t, "files", conflictPaths(ev), "notes.md")
	file := fileOf(t, ev, "notes.md")
	equal(t, "the base blob", file["base"], h.accountGit("rev-parse", tip+":alpha-dir/notes.md"))
	equal(t, "the theirs blob", file["theirs"], h.accountGit("rev-parse", candidate+":alpha-dir/notes.md"))
	equal(t, "the mine blob", file["mine"], h.accountGit("hash-object", filepath.Join(h.library, "alpha", "notes.md")))

	mine := ev["mine"].(string)
	head, mergeHead, msg := mergeState(t, h, "alpha")
	equal(t, "the checkout's HEAD", head, mine)
	equal(t, "the checkout's MERGE_HEAD", mergeHead, candidate)
	equal(t, "the checkout's MERGE_MSG", msg,
		"update alpha from "+first[:7]+" to "+second[:7]+", keeping its edits\n")
	equal(t, "mine's parent", h.accountGit("rev-parse", mine+"^@"), tip)
	equal(t, "mine's notes.md", h.accountGit("cat-file", "blob", mine+":alpha-dir/notes.md"), strings.TrimSuffix(editedNotes, "\n"))
	contains(t, "the checkout's lock", h.accountGit("worktree", "list", "--porcelain"), "locked "+pendingReason)
	equal(t, "the unmerged paths", unmerged(t, h, "alpha"), "1 alpha-dir/notes.md\n2 alpha-dir/notes.md\n3 alpha-dir/notes.md")
	merged := fileBody(t, filepath.Join(pendingCheckout(h, "alpha"), "alpha-dir", "notes.md"))
	contains(t, "the checkout's notes.md", merged, crlf("<<<<<<< "+mine+"\n"+editedNotes+"||||||| "))
	contains(t, "the checkout's notes.md", merged, crlf("\nalpha notes\n=======\nalpha notes, revised upstream\n>>>>>>> "+candidate+"\n"))
	equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), tip)
	equal(t, "the candidate ref", h.ref(lineage.CandidateRef("alpha")), candidate)
	equal(t, "the library", onDisk(t, h.library), library)
	equal(t, "the clients' directories", onDisk(t, h.home), home)
	equal(t, "mutations", mutationVersion(t, h), before+1)
	equal(t, "journals", journalCount(t, h), 0)

	listed := h.listed("alpha")
	equal(t, "state", listed["state"], stateModified)
	equal(t, "pending_merge", listed["pending_merge"], true)
	if listed["candidate"] == nil {
		t.Error("the skill no longer lists the update it has not applied")
	}
	contains(t, "skill list", h.mustRun("skill", "list").stdout, "modified, "+updateAvailable+", "+mergePending+"  ")

	again := h.run("--json", "skill", "update", "alpha")
	equal(t, "exit of the update run again", again.exit, 4)
	sameEvent(t, "the conflict of the update run again", h.one(again.stdout, "conflict"), ev)
	text := h.run("skill", "update", "alpha")
	equal(t, "exit of the update run again in text", text.exit, 4)
	equal(t, "stdout of the update run again", text.stdout, "alpha conflicts with its update from "+first[:7]+" to "+second[:7]+" in 1 file\nnotes.md: both modified\n")
	equal(t, "stderr of the update run again", text.stderr, "error: "+e["message"].(string)+"\nhint: "+e["hint"].(string)+"\n")
	equal(t, "the library once run again", onDisk(t, h.library), library)
	equal(t, "the import branch once run again", h.ref(lineage.ManagedRef("alpha")), tip)
	equal(t, "the unmerged paths once run again", unmerged(t, h, "alpha"), "1 alpha-dir/notes.md\n2 alpha-dir/notes.md\n3 alpha-dir/notes.md")
	equal(t, "journals once run again", journalCount(t, h), 0)
}

// TestSkillUpdateAppliesAMergeResolvedWithGit: once the file that
// conflicts is merged by hand in the checkout and staged with plain git,
// committed too, the update run again applies the merge as it applies a
// clean one: the import branch moves to the update and the candidate goes,
// the library directory and the copy that still held the version installed
// hold the user's resolution rather than the update, the library keeping
// the .DS_Store git ignores in it, the copy edited where it is is kept with
// its warning, and the checkout and git's registration of it are gone,
// nothing left behind.
func TestSkillUpdateAppliesAMergeResolvedWithGit(t *testing.T) {
	t.Parallel()
	for _, commit := range []bool{false, true} {
		t.Run(fmt.Sprintf("committed %v", commit), func(t *testing.T) {
			t.Parallel()
			h, s, first := updateHarness(t)
			claude := filepath.Join(h.home, ".claude", "skills", "alpha")
			cursor := filepath.Join(h.home, ".cursor", "skills", "alpha")
			editCopy(t, cursor)
			edited := libraryTree(t, cursor)
			editLibrary(t, h, "alpha", ".DS_Store", "finder data\n")
			pendingMerge(t, h, s)
			candidate := h.ref(lineage.CandidateRef("alpha"))
			skillDir := filepath.Join(pendingCheckout(h, "alpha"), "alpha-dir")
			writeFile(t, filepath.Join(skillDir, "notes.md"), "alpha notes, merged by hand\n")
			gitIn(t, h, skillDir, "add", "notes.md")
			if commit {
				gitIn(t, h, skillDir, "commit", "--quiet", "--no-edit")
			}

			out := h.mustRun("--json", "skill", "update", "alpha")
			equal(t, "summary", h.one(out.stdout, "result")["summary"],
				"updated alpha from "+first[:7]+" to "+short(s.run("rev-parse", "HEAD"))+" with the merge you resolved, 1 copy placement refreshed, 1 placement skipped")
			equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), editedCopyWarning(cursor))
			equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), candidate)
			equal(t, "the candidate ref", h.ref(lineage.CandidateRef("alpha")), "")
			noCheckout(t, h, "alpha")
			want := secondTree(t, s)
			want["notes.md"] = "alpha notes, merged by hand\n"
			sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "alpha")), withFile(want, ".DS_Store", "finder data\n"))
			sameTree(t, "claude's copy", libraryTree(t, claude), want)
			sameTree(t, "cursor's copy", libraryTree(t, cursor), edited)
			for _, dir := range []string{h.library, filepath.Dir(claude), filepath.Dir(cursor), filepath.Join(h.agentx, "merges")} {
				equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
			}
			equal(t, "journals", journalCount(t, h), 0)
			ev := h.one(out.stdout, "library_skill")
			equal(t, "state", ev["state"], stateModified)
			if _, ok := ev["pending_merge"]; ok {
				t.Errorf("the skill still carries pending_merge: %v", ev["pending_merge"])
			}
		})
	}
}

// TestSkillUpdateFromInsideTheCheckout: a run started in the skill's
// directory in the checkout, as one is once the merge is resolved there
// with git, applies the merge or gives it up although that removes the
// directory it runs in.
func TestSkillUpdateFromInsideTheCheckout(t *testing.T) {
	// Not parallel: it changes the process's working directory.
	for _, abort := range []bool{false, true} {
		t.Run(fmt.Sprintf("abort %v", abort), func(t *testing.T) {
			h, s, _ := updateHarness(t)
			pendingMerge(t, h, s)
			resolvedWithGit(t, h, false)
			t.Chdir(filepath.Join(pendingCheckout(h, "alpha"), "alpha-dir"))
			want := "alpha notes, revised upstream\n"
			args := []string{"--json", "skill", "update", "alpha"}
			if abort {
				want, args = editedNotes, append(args, "--abort")
			}
			h.mustRun(args...)
			noCheckout(t, h, "alpha")
			equal(t, "notes.md", fileBody(t, filepath.Join(h.library, "alpha", "notes.md")), want)
		})
	}
}

// TestSkillUpdateAbortGivesTheMergeUp: a merge given up inside its
// checkout with git merge --abort is no merge in progress, and the update
// run again reports it pending with no file left to resolve until it is
// given up. skill update --abort gives it up whatever state it is in: git
// removes the checkout and its registration, and the library, every copy,
// the import branch and the candidate are byte for byte as they were. A
// second --abort finds no merge pending, as does one on a name the library
// does not hold, --abort takes no --all, and the update then conflicts
// again as it did.
func TestSkillUpdateAbortGivesTheMergeUp(t *testing.T) {
	t.Parallel()
	h, s, _ := updateHarness(t)
	pendingMerge(t, h, s)
	tip, candidate := h.ref(lineage.ManagedRef("alpha")), h.ref(lineage.CandidateRef("alpha"))
	library, placed, before := onDisk(t, h.library), onDisk(t, h.home), mutationVersion(t, h)
	checkoutGit(t, h, "alpha", "merge", "--abort")

	again := h.run("--json", "skill", "update", "alpha")
	equal(t, "exit of the update run again", again.exit, 4)
	ev := h.one(again.stdout, "conflict")
	equal(t, "files left once git gave the merge up", conflictPaths(ev), "")
	equal(t, "theirs", ev["theirs"], candidate)
	equal(t, "code", h.one(again.stdout, "error")["code"], "pending_merge")

	out := h.run("--json", "skill", "update", "alpha", "--abort")
	equal(t, "exit", out.exit, 0)
	equal(t, "summary", h.one(out.stdout, "result")["summary"], "gave up the merge of alpha; the library directory is as it was")
	if _, ok := h.one(out.stdout, "library_skill")["pending_merge"]; ok {
		t.Error("the skill still carries pending_merge")
	}
	noCheckout(t, h, "alpha")
	equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), tip)
	equal(t, "the candidate", h.ref(lineage.CandidateRef("alpha")), candidate)
	equal(t, "the library", onDisk(t, h.library), library)
	equal(t, "the placements", onDisk(t, h.home), placed)
	equal(t, "mutations", mutationVersion(t, h), before+1)
	equal(t, "journals", journalCount(t, h), 0)
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), "")

	second := h.run("--json", "skill", "update", "alpha", "--abort")
	equal(t, "exit of a second abort", second.exit, 6)
	equal(t, "message of a second abort", h.one(second.stdout, "error")["message"], "alpha has no merge pending")
	equal(t, "exit of --abort on a name the library does not hold", h.run("skill", "update", "nosuch", "--abort").exit, 6)
	equal(t, "exit of --abort with --all", h.run("skill", "update", "--all", "--abort").exit, 1)
	equal(t, "the update once given up conflicts again", h.run("skill", "update", "alpha").exit, 4)
}

// TestSkillUpdateRefusesToApplyAMergeItCannot: a merge resolved with git
// is not applied, exit code 6 with nothing written and the checkout kept
// for --abort, when the import branch was moved outside agentx while it
// was pending, or when the library directory was edited meanwhile, since
// applying the merge would replace an edit nobody merged.
func TestSkillUpdateRefusesToApplyAMergeItCannot(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, message, hint string
		change              func(t *testing.T, h *harness)
	}{
		{
			name:    "an import branch moved outside agentx",
			message: "the import branch refs/heads/managed/alpha moved outside agentx while the merge was pending, so the merge cannot be applied",
			hint:    "run 'agentx skill update alpha --abort' to give it up",
			change: func(t *testing.T, h *harness) {
				tip := h.ref(lineage.ManagedRef("alpha"))
				moved := h.accountGit("commit-tree", tip+"^{tree}", "-m", "written again", "-m", h.accountGit("log", "-1", "--format=%B", tip))
				h.accountGit("update-ref", lineage.ManagedRef("alpha"), moved, tip)
			},
		},
		{
			name:    "a library directory edited meanwhile",
			message: "alpha was edited while its merge was pending, so the merge cannot be applied",
			hint:    "run 'agentx skill update alpha --abort' and update again",
			change: func(t *testing.T, h *harness) {
				editLibrary(t, h, "alpha", "mine.md", "a file of my own\n")
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, s, _ := updateHarness(t)
			pendingMerge(t, h, s)
			resolvedWithGit(t, h, false)
			c.change(t, h)
			refs := h.accountGit("for-each-ref", "--format=%(refname) %(objectname)")
			library, placed := onDisk(t, h.library), onDisk(t, h.home)
			_, mergeHead, _ := mergeState(t, h, "alpha")

			out := h.run("--json", "skill", "update", "alpha")
			equal(t, "exit", out.exit, 6)
			e := h.one(out.stdout, "error")
			equal(t, "message", e["message"], c.message)
			equal(t, "hint", e["hint"], c.hint)
			equal(t, "the refs", h.accountGit("for-each-ref", "--format=%(refname) %(objectname)"), refs)
			equal(t, "the library", onDisk(t, h.library), library)
			equal(t, "the placements", onDisk(t, h.home), placed)
			equal(t, "MERGE_HEAD", strings.TrimSpace(checkoutGit(t, h, "alpha", "rev-parse", "MERGE_HEAD")), mergeHead)
			equal(t, "journals", journalCount(t, h), 0)

			h.mustRun("skill", "update", "alpha", "--abort")
			noCheckout(t, h, "alpha")
		})
	}
}

// TestAPendingMergeCheckoutIsAnOrdinaryMergeInProgress: the checkout of a
// pending merge is a merge git knows how to finish. git status there lists
// the unmerged path, and once the file is resolved as the hint says, in the
// skill's directory in the checkout and by the path the conflict lists, a
// plain git commit makes the merge commit, mine and the candidate its
// parents and the message MERGE_MSG holds its message. The library
// directory is as it was all along.
func TestAPendingMergeCheckoutIsAnOrdinaryMergeInProgress(t *testing.T) {
	t.Parallel()
	h, s, _ := updateHarness(t)
	pendingMerge(t, h, s)
	library := onDisk(t, h.library)
	head, candidate, msg := mergeState(t, h, "alpha")

	contains(t, "git status", checkoutGit(t, h, "alpha", "status", "--porcelain"), "UU alpha-dir/notes.md")
	skillDir := filepath.Join(pendingCheckout(h, "alpha"), "alpha-dir")
	gitIn(t, h, skillDir, "checkout", "--theirs", "notes.md")
	gitIn(t, h, skillDir, "add", "notes.md")
	checkoutGit(t, h, "alpha", "commit", "--no-edit", "--quiet")
	equal(t, "the commit's notes.md", checkoutGit(t, h, "alpha", "rev-parse", "HEAD:alpha-dir/notes.md"), h.accountGit("rev-parse", candidate+":alpha-dir/notes.md")+"\n")
	equal(t, "the commit's parents", strings.TrimSpace(checkoutGit(t, h, "alpha", "rev-parse", "HEAD^@")), head+"\n"+candidate)
	equal(t, "the commit's message", checkoutGit(t, h, "alpha", "log", "-1", "--format=%B"), msg+"\n")
	equal(t, "the library", onDisk(t, h.library), library)
}

// TestSkillUpdateConflictsOfEveryKind merges edits that conflict with an
// update in each way git tells apart: a line changed on both sides; a file
// added on both sides, which has no base; a file the update deletes and
// the library changed, and one the library deleted and the update changed,
// each with the missing side null; and a file moved to one place in the
// library and to another by the update, each of its three paths with the
// one version that has a file there. Every path is relative to the skill's
// directory, which the upstream calls otherwise, and the text says of each
// file what conflicts in it in git status's own words.
func TestSkillUpdateConflictsOfEveryKind(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("kinds", true)
	s.skill("skills/kinds-dir", "kinds", "Conflicts of every kind", map[string]string{
		"notes.md": "one\ntwo\nthree\n",
		"gone.md":  "gone\n",
		"kept.md":  "kept\n",
		"moved.md": "1\n2\n3\n4\n5\n6\n7\n8\n",
	})
	first := s.commit("first version")
	h.mustRun("skill", "add", s.url)
	s.write("skills/kinds-dir/notes.md", "one\ntwo, upstream\nthree\n")
	s.write("skills/kinds-dir/both.md", "added upstream\n")
	s.run("rm", "--quiet", "skills/kinds-dir/gone.md")
	s.write("skills/kinds-dir/kept.md", "kept, upstream\n")
	s.run("mv", "skills/kinds-dir/moved.md", "skills/kinds-dir/moved-upstream.md")
	second := s.commit("second version")
	h.mustRun("skill", "check")
	lib := filepath.Join(h.library, "kinds")
	editLibrary(t, h, "kinds", "notes.md", "one\ntwo, mine\nthree\n")
	editLibrary(t, h, "kinds", "both.md", "added here\n")
	editLibrary(t, h, "kinds", "gone.md", "gone, edited here\n")
	remove(t, filepath.Join(lib, "kept.md"))
	if err := os.Rename(filepath.Join(lib, "moved.md"), filepath.Join(lib, "moved-here.md")); err != nil {
		t.Fatal(err)
	}
	library := onDisk(t, h.library)

	out := h.run("--json", "skill", "update", "kinds")
	equal(t, "exit", out.exit, 4)
	ev := h.one(out.stdout, "conflict")
	equal(t, "files", conflictPaths(ev), "both.md,gone.md,kept.md,moved-here.md,moved-upstream.md,moved.md,notes.md")
	for _, c := range []struct {
		path               string
		base, mine, theirs bool
	}{
		{path: "both.md", mine: true, theirs: true},
		{path: "gone.md", base: true, mine: true},
		{path: "kept.md", base: true, theirs: true},
		{path: "moved-here.md", mine: true},
		{path: "moved-upstream.md", theirs: true},
		{path: "moved.md", base: true},
		{path: "notes.md", base: true, mine: true, theirs: true},
	} {
		f := fileOf(t, ev, c.path)
		for _, side := range []struct {
			name string
			want bool
		}{{"base", c.base}, {"mine", c.mine}, {"theirs", c.theirs}} {
			if got := f[side.name] != nil; got != side.want {
				t.Errorf("%s: %s is %v", c.path, side.name, f[side.name])
			}
		}
	}
	equal(t, "the library", onDisk(t, h.library), library)
	equal(t, "pending_merge", h.listed("kinds")["pending_merge"], true)

	// The same conflicts in text, once the merge is given up.
	h.accountGit("worktree", "remove", "-f", "-f", pendingCheckout(h, "kinds"))
	text := h.run("skill", "update", "kinds")
	equal(t, "exit in text", text.exit, 4)
	equal(t, "the text", text.stdout, "kinds conflicts with its update from "+first[:7]+" to "+second[:7]+" in 7 files\n"+
		"both.md: both added\n"+
		"gone.md: deleted by them\n"+
		"kept.md: deleted by us\n"+
		"moved-here.md: added by us\n"+
		"moved-upstream.md: added by them\n"+
		"moved.md: both deleted\n"+
		"notes.md: both modified\n")
	equal(t, "stderr in text", text.stderr,
		"error: kinds conflicts with its update in 7 files, so the merge is pending and the library directory was left as it is\n"+
			"hint: resolve it with git in "+filepath.Join(pendingCheckout(h, "kinds"), "kinds-dir")+" ('git add' each file you resolved, or 'git checkout --ours|--theirs <file>' then 'git add'; 'git commit' is optional), "+
			"then run 'agentx skill update kinds' again to apply it, or 'agentx skill update kinds --abort' to give it up\n")
}

// TestSkillUpdateMergesASkillAtTheRootOfItsSource: a skill at the root of
// its source, whose import tree holds it under the repository's name, merges
// as any other: an edit the update leaves alone is kept, and one it
// overlaps conflicts at a path relative to the skill's directory.
func TestSkillUpdateMergesASkillAtTheRootOfItsSource(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("rooted", true)
	s.skill("", "rooted", "A skill at the root", map[string]string{"notes.md": "root notes\n", "usage.md": "root usage\n"})
	s.commit("first version")
	h.mustRun("skill", "add", s.url)
	s.write("notes.md", "root notes, revised\n")
	s.commit("second version")
	h.mustRun("skill", "check")
	editLibrary(t, h, "rooted", "usage.md", "root usage, edited here\n")

	h.mustRun("skill", "update", "rooted")
	sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "rooted")), map[string]string{
		"SKILL.md": fileBody(t, filepath.Join(s.work, "SKILL.md")), "notes.md": "root notes, revised\n", "usage.md": "root usage, edited here\n",
	})
	equal(t, "state", h.listed("rooted")["state"], stateModified)

	s.write("usage.md", "root usage, revised\n")
	s.commit("third version")
	h.mustRun("skill", "check")
	out := h.run("--json", "skill", "update", "rooted")
	equal(t, "exit", out.exit, 4)
	ev := h.one(out.stdout, "conflict")
	equal(t, "files", conflictPaths(ev), "usage.md")
}

// TestAPendingMergeBlocksWhatWouldReplaceOrRemoveTheSkill: while a merge
// is pending, a revert and a removal of the whole skill, with no --from
// or with --from universal, exit 4 with the hint that gives the merge up,
// and change nothing; so does the removal of what is left of the skill
// once its library directory is gone, while a removal of it from one
// configuration is refused as it is for any skill whose library directory
// is gone. Taking one placement away and putting placements back, with
// --keep-library too, move neither the library directory nor the import
// branch, leave the checkout's merge as it was, and are not blocked.
func TestAPendingMergeBlocksWhatWouldReplaceOrRemoveTheSkill(t *testing.T) {
	t.Parallel()
	h, s, _ := updateHarness(t)
	pendingMerge(t, h, s)
	head, mergeHead, _ := mergeState(t, h, "alpha")
	refs := h.accountGit("for-each-ref", "--format=%(refname) %(objectname)")
	library := onDisk(t, h.library)
	hint := "run 'agentx skill update alpha --abort' to give the merge up; the library directory stays as it is"
	for _, c := range []struct {
		args []string
		what string
	}{
		{[]string{"skill", "revert", "alpha"}, "reverted"},
		{[]string{"skill", "remove", "alpha"}, "removed"},
		{[]string{"skill", "remove", "alpha", "--from", "universal"}, "removed"},
	} {
		out := h.run(append([]string{"--json"}, c.args...)...)
		what := strings.Join(c.args, " ")
		equal(t, what+": exit", out.exit, 4)
		e := h.one(out.stdout, "error")
		equal(t, what+": message", e["message"], "alpha has a merge with its update pending, so it cannot be "+c.what+" until the merge is resolved or given up")
		equal(t, what+": hint", e["hint"], hint)
		equal(t, what+": the refs", h.accountGit("for-each-ref", "--format=%(refname) %(objectname)"), refs)
		equal(t, what+": the library", onDisk(t, h.library), library)
		equal(t, what+": journals", journalCount(t, h), 0)
	}

	h.mustRun("skill", "remove", "alpha", "--from", "cursor")
	h.mustRun("skill", "place", "alpha")
	h.mustRun("skill", "place", "alpha", "--keep-library")
	gotHead, gotMergeHead, _ := mergeState(t, h, "alpha")
	equal(t, "the checkout's HEAD after a placement was removed and put back", gotHead, head)
	equal(t, "the checkout's MERGE_HEAD after a placement was removed and put back", gotMergeHead, mergeHead)
	equal(t, "the library after a placement was removed and put back", onDisk(t, h.library), library)

	remove(t, filepath.Join(h.library, "alpha"))
	out := h.run("--json", "skill", "remove", "alpha")
	equal(t, "exit of the removal of what is left", out.exit, 4)
	equal(t, "hint of the removal of what is left", h.one(out.stdout, "error")["hint"], hint)
	if _, err := os.Lstat(pendingCheckout(h, "alpha")); err != nil {
		t.Errorf("the checkout after that: %v", err)
	}
	equal(t, "the import branch after that", h.ref(lineage.ManagedRef("alpha")) != "", true)
	out = h.run("--json", "skill", "remove", "alpha", "--from", "claude")
	equal(t, "exit of the removal of what is left from one configuration", out.exit, 6)
	equal(t, "message of the removal of what is left from one configuration", h.one(out.stdout, "error")["message"],
		"the library holds no skill directory for alpha, so it cannot be removed from claude alone")
}

// realGit is the git on the test process's PATH, for a wrapper to hand
// calls to.
func realGit(t *testing.T) string {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	return real
}

// TestSkillCheckDuringAPendingMergeMovesOnlyTheCandidate: a check that
// finds a newer version while a merge is pending moves the candidate and
// leaves the merge as it is, still merging the version it merged; the
// skill lists both the newer update and the merge pending. Once resolved,
// the merge moves the import branch to the version it merged, and the
// newer candidate stays as the skill's next update.
func TestSkillCheckDuringAPendingMergeMovesOnlyTheCandidate(t *testing.T) {
	t.Parallel()
	h, s, _ := updateHarness(t)
	pendingMerge(t, h, s)
	head, _, _ := mergeState(t, h, "alpha")
	candidate := h.ref(lineage.CandidateRef("alpha"))
	library := onDisk(t, h.library)
	s.write("skills/alpha-dir/notes.md", "alpha notes, revised again upstream\n")
	third := s.commit("third version")

	h.mustRun("skill", "check")
	moved := h.ref(lineage.CandidateRef("alpha"))
	if moved == "" || moved == candidate {
		t.Fatalf("the check left the candidate at %q, from %q", moved, candidate)
	}
	gotHead, mergeHead, _ := mergeState(t, h, "alpha")
	equal(t, "the checkout's HEAD", gotHead, head)
	equal(t, "the version the merge merges", mergeHead, candidate)
	equal(t, "the library", onDisk(t, h.library), library)
	listed := h.listed("alpha")
	equal(t, "the update listed", listed["candidate"].(map[string]any)["upstream_commit"], third)
	equal(t, "pending_merge", listed["pending_merge"], true)

	resolvedWithGit(t, h, false)
	h.mustRun("skill", "update", "alpha")
	equal(t, "the import branch once applied", h.ref(lineage.ManagedRef("alpha")), candidate)
	equal(t, "the candidate once applied", h.ref(lineage.CandidateRef("alpha")), moved)
	noCheckout(t, h, "alpha")
	listed = h.listed("alpha")
	equal(t, "the update listed once applied", listed["candidate"].(map[string]any)["upstream_commit"], third)
}

// TestSkillUpdateOfASkillAdoptedWithItsEdits: a skill another tool
// installed and the user edited, adopted with its edits kept at the version
// it was installed from, is modified from the start. Its update merges
// those edits: an edit the update leaves alone is kept on the new version,
// and one it overlaps conflicts, leaving the directory as it was. The other
// tool's lock file is never written.
func TestSkillUpdateOfASkillAdoptedWithItsEdits(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, file, content string
		conflicts           bool
	}{
		{name: "an edit the update leaves alone", file: "mine.md", content: "a file of my own\n"},
		{name: "an edit the update overlaps", file: "notes.md", content: "alpha notes, mine\n", conflicts: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, s, v1, _ := adoptHarness(t)
			editLibrary(t, h, "alpha", c.file, c.content)
			lock := h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
				Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/alpha", SkillFolderHash: "",
			}})
			h.mustRun("adopt", "--skill", "alpha", "--base", v1)
			equal(t, "state once adopted", h.listed("alpha")["state"], stateModified)
			h.mustRun("skill", "check")
			candidate := h.ref(lineage.CandidateRef("alpha"))
			library := onDisk(t, h.library)

			out := h.run("--json", "skill", "update", "alpha")
			if c.conflicts {
				equal(t, "exit", out.exit, 4)
				equal(t, "files", conflictPaths(h.one(out.stdout, "conflict")), "notes.md")
				equal(t, "the library", onDisk(t, h.library), library)
				equal(t, "the candidate", h.ref(lineage.CandidateRef("alpha")), candidate)
			} else {
				equal(t, "exit", out.exit, 0)
				equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), candidate)
				equal(t, "notes.md", fileBody(t, filepath.Join(h.library, "alpha", "notes.md")), "alpha notes, revised\n")
				equal(t, c.file, fileBody(t, filepath.Join(h.library, "alpha", c.file)), c.content)
				equal(t, "state", h.listed("alpha")["state"], stateModified)
			}
			h.lockUnchanged(h.lockPath(), lock)
		})
	}
}

// TestSkillUpdateMergeRecoversAtEveryBoundary kills a clean merge with
// SIGKILL once its journal is on disk, then leaves the machine as a
// process killed after each later step would: the import branch moved, the
// library directory retained, the merged version published, the copy
// retained, the copy refreshed, and the candidate deleted with the journal
// not yet told. A last case is killed for real right after that deletion.
// The next command recovers each one, and the update is then whole: the
// branch at the candidate, the candidate gone, the library and the unedited
// copy holding the new version with the edits on it, the edited copy kept,
// the library's .DS_Store, which git ignores, still there, no merge
// pending and nothing staged or retained left behind.
func TestSkillUpdateMergeRecoversAtEveryBoundary(t *testing.T) {
	t.Parallel()
	const steps = 6 // the branch, the library's remove and publish, the copy's, then the candidate
	for stop := 0; stop <= steps+1; stop++ {
		name := fmt.Sprintf("after %d steps", stop)
		if stop > steps {
			name = "killed after its last live write"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h, s, _ := updateHarness(t)
			claude := filepath.Join(h.home, ".claude", "skills", "alpha")
			cursor := filepath.Join(h.home, ".cursor", "skills", "alpha")
			editCopy(t, cursor)
			edited := libraryTree(t, cursor)
			editLibrary(t, h, "alpha", "mine.md", "a file of my own\n")
			editLibrary(t, h, "alpha", ".DS_Store", "finder data\n")
			checked(t, h, s)
			want := secondTree(t, s)
			want["mine.md"] = "a file of my own\n"
			tip, candidate := h.ref(lineage.ManagedRef("alpha")), h.ref(lineage.CandidateRef("alpha"))

			if stop > steps {
				out := killedChild(t, h, "TestUpdateChildProcess", updateChildEnv, "alpha", lastWriteScript)
				if h.ref(lineage.CandidateRef("alpha")) != "" {
					t.Fatalf("the update was not killed after it deleted the candidate:\n%s", out)
				}
				equal(t, "journals the killed update left", journalCount(t, h), 1)
			} else {
				killedChild(t, h, "TestUpdateChildProcess", updateChildEnv, "alpha", killedUpdateScript)
				journal := readJournal(t, h)
				var kinds []string
				for _, s := range journal {
					kinds = append(kinds, s.Kind)
				}
				equal(t, "the journal's steps", strings.Join(kinds, ", "), "ref, remove, publish, remove, publish, ref")
				equal(t, "the import branch when the update was killed", h.ref(lineage.ManagedRef("alpha")), tip)
				nothingAt(t, "new.md when the update was killed", filepath.Join(h.library, "alpha", "new.md"))
				applyUpdateSteps(t, h, journal, stop)
			}

			if got := h.run("config", "set", "label", "recovered"); got.exit != 0 {
				t.Fatalf("the command after the killed update: exit %d\n%s", got.exit, got.stderr)
			}
			equal(t, "journals after recovery", journalCount(t, h), 0)
			equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), candidate)
			equal(t, "the candidate ref", h.ref(lineage.CandidateRef("alpha")), "")
			noCheckout(t, h, "alpha")
			sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "alpha")), withFile(want, ".DS_Store", "finder data\n"))
			sameTree(t, "claude's copy", libraryTree(t, claude), want)
			sameTree(t, "cursor's copy", libraryTree(t, cursor), edited)
			for _, dir := range []string{h.library, filepath.Dir(claude), filepath.Dir(cursor)} {
				equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
			}
			equal(t, "state", h.listed("alpha")["state"], stateModified)
		})
	}
}

// TestSkillUpdateAppliesAResolvedMergeRecoversAtEveryBoundary kills the
// update that applies a merge resolved with git once its journal is on
// disk, then leaves the machine as a process killed after each later step
// would: the import branch moved, the library directory retained, the
// merge published, the copy retained, the copy refreshed, the checkout
// retained, and the candidate deleted with the journal not yet told. A
// last case is killed for real right after that deletion. The next command
// recovers each one, and the merge is then applied whole: the branch at
// the update, the candidate gone, the library and the unedited copy
// holding the merge, the edited copy kept, no checkout and no registration
// of one, and nothing staged or retained left behind.
func TestSkillUpdateAppliesAResolvedMergeRecoversAtEveryBoundary(t *testing.T) {
	t.Parallel()
	const kinds = "ref, remove, publish, remove, publish, remove, ref"
	steps := len(strings.Split(kinds, ", "))
	for stop := 0; stop <= steps+1; stop++ {
		name := fmt.Sprintf("after %d steps", stop)
		if stop > steps {
			name = "killed after its last live write"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h, s, _ := updateHarness(t)
			claude := filepath.Join(h.home, ".claude", "skills", "alpha")
			cursor := filepath.Join(h.home, ".cursor", "skills", "alpha")
			editCopy(t, cursor)
			edited := libraryTree(t, cursor)
			pendingMerge(t, h, s)
			resolvedWithGit(t, h, false)
			candidate := h.ref(lineage.CandidateRef("alpha"))

			if stop > steps {
				out := killedChild(t, h, "TestUpdateChildProcess", updateChildEnv, "alpha", lastWriteScript)
				if h.ref(lineage.CandidateRef("alpha")) != "" {
					t.Fatalf("the update was not killed after it deleted the candidate:\n%s", out)
				}
				equal(t, "journals the killed update left", journalCount(t, h), 1)
			} else {
				killedChild(t, h, "TestUpdateChildProcess", updateChildEnv, "alpha", killedUpdateScript)
				journal := readJournal(t, h)
				var got []string
				for _, s := range journal {
					got = append(got, s.Kind)
				}
				equal(t, "the journal's steps", strings.Join(got, ", "), kinds)
				applyUpdateSteps(t, h, journal, stop)
			}

			if got := h.run("config", "set", "label", "recovered"); got.exit != 0 {
				t.Fatalf("the command after the killed update: exit %d\n%s", got.exit, got.stderr)
			}
			want := secondTree(t, s)
			equal(t, "journals after recovery", journalCount(t, h), 0)
			equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), candidate)
			equal(t, "the candidate ref", h.ref(lineage.CandidateRef("alpha")), "")
			noCheckout(t, h, "alpha")
			sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "alpha")), want)
			sameTree(t, "claude's copy", libraryTree(t, claude), want)
			sameTree(t, "cursor's copy", libraryTree(t, cursor), edited)
			for _, dir := range []string{h.library, filepath.Dir(claude), filepath.Dir(cursor), filepath.Join(h.agentx, "merges")} {
				equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
			}
			equal(t, "state", h.listed("alpha")["state"], stateCurrent)
		})
	}
}

// TestSkillUpdateConflictRecoversAtEveryBoundary kills an update whose
// merge conflicts with SIGKILL while it sets the merge up: once git added
// the checkout, and once git wrote the conflicts into its index. Such an
// update writes no journal, and what it left is a merge pending as any
// command sees one: the next update of the skill exits 4 with the hint
// that resolves the merge or gives it up, and the library, the copies, the
// import branch and the candidate are as they were.
func TestSkillUpdateConflictRecoversAtEveryBoundary(t *testing.T) {
	t.Parallel()
	for _, after := range []string{"worktree add", "update-index"} {
		t.Run("killed after git "+after, func(t *testing.T) {
			t.Parallel()
			h, s, _ := updateHarness(t)
			checked(t, h, s)
			editLibrary(t, h, "alpha", "notes.md", editedNotes)
			tip, candidate := h.ref(lineage.ManagedRef("alpha")), h.ref(lineage.CandidateRef("alpha"))
			library, home := onDisk(t, h.library), onDisk(t, h.home)

			out := killedChild(t, h, "TestUpdateChildProcess", updateChildEnv, "alpha", `
case " $* " in
*" `+after+` "*)
	%GIT% "$@"
	status=$?
	kill -9 $PPID
	exit $status
	;;
esac
exec %GIT% "$@"
`)
			if _, err := os.Lstat(pendingCheckout(h, "alpha")); err != nil || strings.Contains(out, "conflicts with its update") {
				t.Fatalf("the update was not killed after git %s:\n%s", after, out)
			}
			equal(t, "journals the killed update left", journalCount(t, h), 0)

			next := h.run("--json", "skill", "update", "alpha")
			equal(t, "exit of the next update", next.exit, 4)
			contains(t, "hint of the next update", h.one(next.stdout, "error")["hint"].(string), "or 'agentx skill update alpha --abort' to give it up")
			equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), tip)
			equal(t, "the candidate ref", h.ref(lineage.CandidateRef("alpha")), candidate)
			equal(t, "the library", onDisk(t, h.library), library)
			equal(t, "the clients' directories", onDisk(t, h.home), home)
			equal(t, "pending_merge", h.listed("alpha")["pending_merge"], true)
		})
	}
}

// TestSkillUpdateRefusesAMergeWhoseLibraryChangedBeforeTheLock: what a
// merge merged is the library directory as the update read it. An edit
// made while git adds the directory to the index mine is written from, or
// while git merges it, before the lock, is found under the lock, whether
// the file edited is one the base version holds too or the user's own: the
// update refuses with exit code 6, whether its merge was clean or
// conflicted: no journal, no merge left pending, the edit where it was
// made, the import branch and the candidate as they were, and the version
// file not bumped.
func TestSkillUpdateRefusesAMergeWhoseLibraryChangedBeforeTheLock(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, file, content string
		during, changed     string // the git call the edit is made during, and the file it edits
	}{
		{name: "a clean merge", file: "mine.md", content: "a file of my own\n", during: "merge-tree", changed: "usage.md"},
		{name: "a merge that conflicts", file: "notes.md", content: editedNotes, during: "merge-tree", changed: "usage.md"},
		{name: "a directory edited while git adds it", file: "mine.md", content: "a file of my own\n", during: "add", changed: "SKILL.md"},
		{name: "a file of the user's edited while git adds it", file: "mine.md", content: "a file of my own\n", during: "add", changed: "mine.md"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, s, _ := updateHarness(t)
			checked(t, h, s)
			editLibrary(t, h, "alpha", c.file, c.content)
			tip, candidate := h.ref(lineage.ManagedRef("alpha")), h.ref(lineage.CandidateRef("alpha"))
			before := mutationVersion(t, h)
			usage := filepath.Join(h.library, "alpha", c.changed)
			stubGit(t, h, `#!/bin/sh
case " $* " in
*" `+c.during+` "*) printf 'an edit made meanwhile\n' > `+shellWord(usage)+` || exit 1 ;;
esac
exec `+realGit(t)+` "$@"
`)
			out := h.run("--json", "skill", "update", "alpha")
			equal(t, "exit", out.exit, 6)
			e := h.one(out.stdout, "error")
			equal(t, "message", e["message"], "alpha changed while it was being updated, so nothing was changed")
			equal(t, "hint", e["hint"], "run 'agentx skill update alpha' again to update it as it is now")
			if got := h.eventsOfType(out.stdout, "conflict"); len(got) != 0 {
				t.Errorf("a refused update reported %v", got)
			}
			equal(t, "the edit made meanwhile", fileBody(t, usage), "an edit made meanwhile\n")
			if c.file != c.changed {
				equal(t, "the edit made before", fileBody(t, filepath.Join(h.library, "alpha", c.file)), c.content)
			}
			equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), tip)
			equal(t, "the candidate", h.ref(lineage.CandidateRef("alpha")), candidate)
			noCheckout(t, h, "alpha")
			equal(t, "journals", journalCount(t, h), 0)
			equal(t, "mutations", mutationVersion(t, h), before)
			equal(t, "what is left beside the library", strings.Join(hiddenEntries(t, h.library), " "), "")
		})
	}
}

// TestSkillUpdateStoppedWhileItMergesChangesNothing stops an update with
// SIGTERM while git merges the edits with the update: while merge-tree
// merges, before the lock, and while update-index sets the merge up in
// the checkout, under it. Either way the run answers with exit code 9 and
// leaves the machine as it was, no checkout and no registration of one
// left, and the next update leaves the merge pending.
func TestSkillUpdateStoppedWhileItMergesChangesNothing(t *testing.T) {
	t.Parallel()
	for _, sub := range []string{"merge-tree", "update-index"} {
		t.Run("stopped during "+sub, func(t *testing.T) {
			t.Parallel()
			h, s, _ := updateHarness(t)
			checked(t, h, s)
			editLibrary(t, h, "alpha", "notes.md", editedNotes)
			tip, candidate := h.ref(lineage.ManagedRef("alpha")), h.ref(lineage.CandidateRef("alpha"))
			library := onDisk(t, h.library)
			before := mutationVersion(t, h)
			ready := filepath.Join(t.TempDir(), "merging")
			path := h.env["PATH"]
			hangingGit(t, h, sub, ready)

			code, stderr := signalled(t, h, stopRun{ready: ready, sigs: []syscall.Signal{syscall.SIGTERM},
				args: []string{"skill", "update", "alpha", "--color", "off"}})
			killLeftover(t, ready)
			h.env["PATH"] = path

			equal(t, "the exit code of a stopped update", code, exitInterrupted.exit)
			contains(t, "stderr", stderr, "error: interrupted")
			equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), tip)
			equal(t, "the candidate", h.ref(lineage.CandidateRef("alpha")), candidate)
			noCheckout(t, h, "alpha")
			equal(t, "the library", onDisk(t, h.library), library)
			equal(t, "journals", journalCount(t, h), 0)
			equal(t, "mutations", mutationVersion(t, h), before)

			equal(t, "exit of the next update", h.run("skill", "update", "alpha").exit, 4)
			if _, err := os.Lstat(pendingCheckout(h, "alpha")); err != nil {
				t.Errorf("the next update left no merge pending: %v", err)
			}
		})
	}
}

// TestSkillUpdateMergesWithNoAttributesOfTheUsers: git attributes of the
// user's own, a merge driver that joins both sides of every Markdown file
// say, decide nothing about how an update merges: an edit the update
// overlaps still conflicts, the merge is left pending with the file
// unmerged in the checkout, and the library directory is as it was.
func TestSkillUpdateMergesWithNoAttributesOfTheUsers(t *testing.T) {
	t.Parallel()
	h, s, _ := updateHarness(t)
	writeFile(t, mkdirs(t, filepath.Join(h.config, "git"), "attributes"), "*.md merge=union\n")
	checked(t, h, s)
	editLibrary(t, h, "alpha", "notes.md", editedNotes)
	library := onDisk(t, h.library)

	out := h.run("--json", "skill", "update", "alpha")
	equal(t, "exit", out.exit, 4)
	ev := h.one(out.stdout, "conflict")
	equal(t, "files", conflictPaths(ev), "notes.md")
	equal(t, "the unmerged paths", unmerged(t, h, "alpha"), "1 alpha-dir/notes.md\n2 alpha-dir/notes.md\n3 alpha-dir/notes.md")
	equal(t, "the library", onDisk(t, h.library), library)
}

// TestSkillUpdateConflictsInTheCheckoutAsMergeTreeFoundIt: the checkout
// holds the conflicts merge-tree found, which the update was judged by. A
// union merge driver the skill's own .gitattributes names for a file does
// not merge it there, so the file's three versions are in the index, and
// the library, the import branch and the candidate are as they were.
func TestSkillUpdateConflictsInTheCheckoutAsMergeTreeFoundIt(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("kit", true)
	s.skill("skills/union", "union", "Joins its notes", map[string]string{".gitattributes": "notes.md merge=union\n", "notes.md": "notes\n"})
	s.commit("first version")
	h.mustRun("source", "add", s.url)
	h.mustRun("skill", "add", s.url, "--skill", "union")
	s.write("skills/union/notes.md", "notes, revised upstream\n")
	s.commit("second version")
	h.mustRun("skill", "check")
	editLibrary(t, h, "union", "notes.md", "notes, edited here\n")
	tip, candidate := h.ref(lineage.ManagedRef("union")), h.ref(lineage.CandidateRef("union"))
	library := onDisk(t, h.library)

	out := h.run("--json", "skill", "update", "union")
	equal(t, "exit", out.exit, 4)
	equal(t, "files", conflictPaths(h.one(out.stdout, "conflict")), "notes.md")
	_, mergeHead, _ := mergeState(t, h, "union")
	equal(t, "MERGE_HEAD", mergeHead, candidate)
	equal(t, "the unmerged paths", unmerged(t, h, "union"), "1 union/notes.md\n2 union/notes.md\n3 union/notes.md")
	equal(t, "the import branch", h.ref(lineage.ManagedRef("union")), tip)
	equal(t, "the candidate ref", h.ref(lineage.CandidateRef("union")), candidate)
	equal(t, "the library", onDisk(t, h.library), library)
	equal(t, "journals", journalCount(t, h), 0)
}

// TestSkillUpdateKeepsAFileAddedInADirectoryTheUpdateRenamed: a file the
// user added inside a directory the update renames merges cleanly and
// stays where the user put it, beside the renamed directory; git is not
// left to guess that it should move with the rest. When a line both sides
// changed makes the update conflict, the merge left pending conflicts in
// that file alone, and its checkout keeps the user's file where the user
// put it too.
func TestSkillUpdateKeepsAFileAddedInADirectoryTheUpdateRenamed(t *testing.T) {
	t.Parallel()
	for _, conflicting := range []bool{false, true} {
		name := "merged cleanly"
		if conflicting {
			name = "left pending"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			h.build(t, fixture{dirs: []string{".claude"}})
			s := h.newSourceRepo("renamed", true)
			s.skill("skills/renamed", "renamed", "A directory renamed", map[string]string{"docs/a.md": "a\n", "docs/b.md": "b\n", "notes.md": "notes\n"})
			s.commit("first version")
			h.mustRun("skill", "add", s.url)
			s.run("mv", "skills/renamed/docs", "skills/renamed/guide")
			if conflicting {
				s.write("skills/renamed/notes.md", "notes, upstream\n")
			}
			s.commit("second version")
			h.mustRun("skill", "check")
			lib := filepath.Join(h.library, "renamed")
			editLibrary(t, h, "renamed", filepath.Join("docs", "mine.md"), "a file of my own\n")
			candidate := h.ref(lineage.CandidateRef("renamed"))

			if conflicting {
				editLibrary(t, h, "renamed", "notes.md", "notes, mine\n")
				out := h.run("--json", "skill", "update", "renamed")
				equal(t, "exit", out.exit, 4)
				equal(t, "files", conflictPaths(h.one(out.stdout, "conflict")), "notes.md")
				equal(t, "the unmerged paths", unmerged(t, h, "renamed"), "1 renamed/notes.md\n2 renamed/notes.md\n3 renamed/notes.md")
				equal(t, "the user's file in the checkout", fileBody(t, filepath.Join(pendingCheckout(h, "renamed"), "renamed", "docs", "mine.md")), "a file of my own\n")
				nothingAt(t, "the user's file moved", filepath.Join(pendingCheckout(h, "renamed"), "renamed", "guide", "mine.md"))
				return
			}
			out := h.mustRun("--json", "skill", "update", "renamed")
			contains(t, "summary", h.one(out.stdout, "result")["summary"].(string), " and merged its edits cleanly")
			equal(t, "the import branch", h.ref(lineage.ManagedRef("renamed")), candidate)
			noCheckout(t, h, "renamed")
			sameTree(t, "the library directory", libraryTree(t, lib), map[string]string{
				"SKILL.md":                       fileBody(t, filepath.Join(s.work, "skills", "renamed", "SKILL.md")),
				"notes.md":                       "notes\n",
				filepath.Join("guide", "a.md"):   "a\n",
				filepath.Join("guide", "b.md"):   "b\n",
				filepath.Join("docs", "mine.md"): "a file of my own\n",
			})
			equal(t, "state", h.listed("renamed")["state"], stateModified)
		})
	}
}

// TestSkillUpdateMergeRefreshesACopyThatHoldsTheEditedLibrary: a copy
// placed after the library directory was edited holds the edits, which is
// what agentx put there, so a clean merge refreshes it with the merged
// version, as it refreshes a copy that still holds the version installed;
// neither is warned about or skipped.
func TestSkillUpdateMergeRefreshesACopyThatHoldsTheEditedLibrary(t *testing.T) {
	t.Parallel()
	h, s, _ := updateHarness(t)
	claude := filepath.Join(h.home, ".claude", "skills", "alpha")
	cursor := filepath.Join(h.home, ".cursor", "skills", "alpha")
	lib := filepath.Join(h.library, "alpha")
	editLibrary(t, h, "alpha", "mine.md", "a file of my own\n")
	remove(t, cursor)
	h.mustRun("skill", "place", "alpha", "--to", "cursor", "--copy")
	sameTree(t, "cursor's copy before the update", libraryTree(t, cursor), libraryTree(t, lib))
	checked(t, h, s)
	want := secondTree(t, s)
	want["mine.md"] = "a file of my own\n"

	out := h.mustRun("--json", "skill", "update", "alpha")
	summary := h.one(out.stdout, "result")["summary"].(string)
	if !strings.HasSuffix(summary, " and merged its edits cleanly, 2 copy placements refreshed") {
		t.Errorf("summary = %q", summary)
	}
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), "")
	sameTree(t, "the library directory", libraryTree(t, lib), want)
	sameTree(t, "claude's copy", libraryTree(t, claude), want)
	sameTree(t, "cursor's copy", libraryTree(t, cursor), want)
}

// TestAnIgnoredFileIsNotMineAndSurvivesTheMerge: a file git ignores in the
// library directory is no edit, so it is never merged. A clean merge
// carries a .DS_Store and a log the skill's own .gitignore names over into
// the merged version, and a log of the user's the new version ships is the
// new version's, as git checkout leaves it. A merge that conflicts commits
// neither into mine, and the library keeps both.
func TestAnIgnoredFileIsNotMineAndSurvivesTheMerge(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("kit", true)
	s.skill("skills/logs", "logs", "Keeps its logs", map[string]string{".gitignore": "*.log\n", "notes.md": "notes\n", "usage.md": "usage\n"})
	s.commit("first version")
	h.mustRun("source", "add", s.url)
	h.mustRun("skill", "add", s.url, "--skill", "logs")
	s.write("skills/logs/notes.md", "notes, revised upstream\n")
	s.write("skills/logs/run.log", "the log the new version ships\n")
	s.run("add", "--force", "skills/logs/run.log") // the source's own .gitignore names it
	s.commit("second version")
	h.mustRun("skill", "check")
	lib := filepath.Join(h.library, "logs")
	editLibrary(t, h, "logs", "usage.md", "usage, edited here\n")
	editLibrary(t, h, "logs", ".DS_Store", "finder data\n")
	editLibrary(t, h, "logs", "run.log", "a log of my own\n")
	editLibrary(t, h, "logs", "debug.log", "another log of my own\n")

	out := h.mustRun("--json", "skill", "update", "logs")
	contains(t, "summary", h.one(out.stdout, "result")["summary"].(string), " and merged its edits cleanly")
	equal(t, "usage.md", fileBody(t, filepath.Join(lib, "usage.md")), "usage, edited here\n")
	equal(t, "notes.md", fileBody(t, filepath.Join(lib, "notes.md")), "notes, revised upstream\n")
	equal(t, "run.log", fileBody(t, filepath.Join(lib, "run.log")), "the log the new version ships\n")
	equal(t, "debug.log", fileBody(t, filepath.Join(lib, "debug.log")), "another log of my own\n")
	equal(t, ".DS_Store", fileBody(t, filepath.Join(lib, ".DS_Store")), "finder data\n")
	equal(t, "state", h.listed("logs")["state"], stateModified)

	s.write("skills/logs/usage.md", "usage, revised upstream\n")
	s.commit("third version")
	h.mustRun("skill", "check")
	library := onDisk(t, h.library)
	out = h.run("--json", "skill", "update", "logs")
	equal(t, "exit of the update that conflicts", out.exit, 4)
	mine := h.one(out.stdout, "conflict")["mine"].(string)
	equal(t, "what mine holds", h.accountGit("ls-tree", "-r", "--name-only", mine),
		"logs/.gitignore\nlogs/SKILL.md\nlogs/notes.md\nlogs/run.log\nlogs/usage.md")
	equal(t, "the library", onDisk(t, h.library), library)
}

// TestRecoveryPrunesStaleMergeCheckouts: what an interrupted command left
// of a pending merge goes with the next command that changes anything,
// once its journals are finished: a directory under the merges directory
// that is no checkout, one whose checkout's registration is gone, and a
// registration of a checkout there whose directory is gone. The checkout
// of a merge that is pending stays, and still lists as pending.
func TestRecoveryPrunesStaleMergeCheckouts(t *testing.T) {
	t.Parallel()
	h, s, _ := updateHarness(t)
	pendingMerge(t, h, s)
	writeFile(t, mkdirs(t, pendingCheckout(h, "ghost"), "notes.md"), "left behind\n")
	writeFile(t, mkdirs(t, pendingCheckout(h, "orphan"), ".git"), "gitdir: "+filepath.Join(h.agentx, "account.git", "worktrees", "orphan")+"\n")
	h.accountGit("worktree", "add", "--quiet", "--detach", "--lock", "--reason", pendingReason,
		pendingCheckout(h, "beta"), h.ref(lineage.ManagedRef("beta")))
	remove(t, pendingCheckout(h, "beta"))
	head, mergeHead, _ := mergeState(t, h, "alpha")

	h.mustRun("config", "set", "label", "pruned")
	entries, err := os.ReadDir(filepath.Join(h.agentx, "merges"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	equal(t, "the merges directory", strings.Join(names, " "), "alpha")
	list := h.accountGit("worktree", "list", "--porcelain")
	if strings.Contains(list, filepath.Join("merges", "beta")) {
		t.Errorf("the registration of beta's checkout is still there:\n%s", list)
	}
	gotHead, gotMergeHead, _ := mergeState(t, h, "alpha")
	equal(t, "alpha's HEAD", gotHead, head)
	equal(t, "alpha's MERGE_HEAD", gotMergeHead, mergeHead)
	equal(t, "alpha's pending_merge", h.listed("alpha")["pending_merge"], true)
	if pending, ok := h.listed("beta")["pending_merge"]; ok {
		t.Errorf("beta lists a pending merge: %v", pending)
	}
}
