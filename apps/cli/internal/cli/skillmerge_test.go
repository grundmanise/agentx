package cli

import (
	"context"
	"encoding/json"
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

// conflictFiles is what a conflict event says of its files, in order: the
// path of each and its number of hunks, or how it conflicts whole.
func conflictFiles(ev jsonEvent) string {
	var files []string
	for _, f := range ev["files"].([]any) {
		file := f.(map[string]any)
		hunks := len(file["hunks"].([]any))
		switch {
		case file["binary"] == true:
			files = append(files, fmt.Sprintf("%s:binary", file["path"]))
		case hunks == 0:
			files = append(files, fmt.Sprintf("%s:whole", file["path"]))
		default:
			files = append(files, fmt.Sprintf("%s:%d", file["path"], hunks))
		}
	}
	return strings.Join(files, ",")
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

// hunkOf is hunk i, counted from 1, of the file at path of a conflict
// event, as mine|base|theirs.
func hunkOf(t *testing.T, ev jsonEvent, path string, i int) string {
	t.Helper()
	hunks := fileOf(t, ev, path)["hunks"].([]any)
	if i > len(hunks) {
		t.Fatalf("%s has %d hunks, not %d", path, len(hunks), i)
	}
	h := hunks[i-1].(map[string]any)
	if h["index"] != float64(i) {
		t.Errorf("hunk %d of %s is numbered %v", i, path, h["index"])
	}
	return fmt.Sprintf("%s|%s|%s", h["mine"], h["base"], h["theirs"])
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
	r := gitx.New(h.env, false, func(string, ...any) {})
	out, err := r.InCheckout(context.Background(), pendingCheckout(h, name), args...)
	if err != nil {
		t.Fatalf("git %s in the checkout of %s: %v", strings.Join(args, " "), name, err)
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

// fakePendingMerge is a shell command that leaves a merge pending for the
// skill called name as any command sees one, unless one is there already:
// its checkout, added at commit and locked, as an update adds it.
func fakePendingMerge(t *testing.T, h *harness, name, commit string) string {
	t.Helper()
	path := shellWord(pendingCheckout(h, name))
	return "[ -e " + path + " ] || " + realGit(t) + " --git-dir=" + shellWord(gitx.AccountRepoPath(h.agentx)) +
		" worktree add --quiet --detach --lock --reason " + shellWord(pendingReason) + " " + path + " " + commit
}

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

// TestSkillUpdateOfAMergeWhosePathsDifferInCase: git merges paths byte for
// byte, so readme.md, a file the user added to alpha, and README.md, one
// its update adds, merge cleanly. A library that tells paths apart by case
// takes both. One that does not, as macOS's by default does not, cannot
// hold the version merged: the update of alpha refuses before the lock
// with exit code 6, naming the user's file as the one to rename, and
// changes nothing, and update --all updates beta, which nobody edited, all
// the same.
func TestSkillUpdateOfAMergeWhosePathsDifferInCase(t *testing.T) {
	t.Parallel()
	h, s, _ := updateHarness(t)
	lib := filepath.Join(h.library, "alpha")
	editLibrary(t, h, "alpha", "readme.md", "my own readme\n")
	s.write("skills/alpha-dir/README.md", "the upstream readme\n")
	s.write("skills/beta/notes.md", "beta notes, revised\n")
	checked(t, h, s)
	tip, candidate := h.ref(lineage.ManagedRef("alpha")), h.ref(lineage.CandidateRef("alpha"))
	betaCandidate := h.ref(lineage.CandidateRef("beta"))

	if !foldsCaseAt(t, filepath.Join(lib, "SKILL.md")) {
		h.mustRun("skill", "update", "alpha")
		equal(t, "readme.md", fileBody(t, filepath.Join(lib, "readme.md")), "my own readme\n")
		equal(t, "README.md", fileBody(t, filepath.Join(lib, "README.md")), "the upstream readme\n")
		equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), candidate)
		equal(t, "state", h.listed("alpha")["state"], stateModified)
		return
	}
	library := onDisk(t, h.library)
	before := mutationVersion(t, h)
	refusal := "the update of alpha merged with its edits holds both README.md and readme.md, which this file system cannot keep apart, so nothing was changed"
	hint := "rename readme.md in " + lib + ", then run 'agentx skill update alpha' again"

	out := h.run("--json", "skill", "update", "alpha")
	equal(t, "exit", out.exit, 6)
	e := h.one(out.stdout, "error")
	equal(t, "code", e["code"], "refused")
	equal(t, "message", e["message"], refusal)
	equal(t, "hint", e["hint"], hint)
	equal(t, "the library", onDisk(t, h.library), library)
	equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), tip)
	equal(t, "the candidate", h.ref(lineage.CandidateRef("alpha")), candidate)
	equal(t, "journals", journalCount(t, h), 0)
	equal(t, "mutations", mutationVersion(t, h), before)

	all := h.run("--json", "skill", "update", "--all")
	equal(t, "exit of --all", all.exit, 6)
	events := h.eventsOfType(all.stdout, "library_skill")
	if len(events) != 1 || events[0]["name"] != "beta" {
		t.Fatalf("library_skill events %v, want beta's", events)
	}
	equal(t, "warnings of --all", strings.Join(warnings(h, all.stderr), "\n"), "alpha: "+refusal)
	e = h.one(all.stdout, "error")
	equal(t, "message of --all", e["message"], "1 of 2 skills could not be updated: alpha: "+refusal)
	equal(t, "hint of --all", e["hint"], hint)
	equal(t, "beta's notes", fileBody(t, filepath.Join(h.library, "beta", "notes.md")), "beta notes, revised\n")
	equal(t, "beta's branch", h.ref(lineage.ManagedRef("beta")), betaCandidate)
	equal(t, "alpha's readme", fileBody(t, filepath.Join(lib, "readme.md")), "my own readme\n")
	equal(t, "alpha's branch", h.ref(lineage.ManagedRef("alpha")), tip)
	equal(t, "alpha's candidate", h.ref(lineage.CandidateRef("alpha")), candidate)
}

// TestSkillUpdateLeavesAConflictPending: an edit the update overlaps
// conflicts. The update exits 4 and changes nothing agents read: the
// library directory and every copy are byte for byte as they were, and the
// import branch and the candidate hold what they held. The conflict event
// names the three versions and every hunk, mine, base and theirs as text,
// and the stage blobs of the file. The merge is an ordinary git merge in
// progress in the skill's checkout, read back with plain git: locked with
// agentx's reason, at mine, the library directory committed on the base
// version, with the candidate as MERGE_HEAD, the completion's message with
// the trailer naming the candidate in MERGE_MSG, the three stages of the
// file in the index and zdiff3 markers in the file, which git wrote with
// the line endings the update's own .gitattributes asks for, as it writes
// any file of a checkout, and which the hunks give as the file holds them.
// Reading the checkout again gives the same files and hunks, and a later
// invocation sees the merge pending in skill list.
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
	equal(t, "hint", e["hint"], "run 'agentx skill resolve alpha' to resolve the conflicts, or 'agentx skill resolve alpha --abort' to give the merge up")
	if got := h.eventsOfType(out.stdout, "library_skill"); len(got) != 0 {
		t.Errorf("an update that left a merge pending reported %v", got)
	}
	ev := h.one(out.stdout, "conflict")
	equal(t, "name", ev["name"], "alpha")
	equal(t, "kind", ev["kind"], "managed")
	equal(t, "base", ev["base"], tip)
	equal(t, "theirs", ev["theirs"], candidate)
	equal(t, "files", conflictFiles(ev), "notes.md:1")
	equal(t, "the hunk", hunkOf(t, ev, "notes.md", 1), crlf(editedNotes)+"|alpha notes\r\n|alpha notes, revised upstream\r\n")
	file := fileOf(t, ev, "notes.md")
	equal(t, "the base blob", file["base"], h.accountGit("rev-parse", tip+":alpha-dir/notes.md"))
	equal(t, "the theirs blob", file["theirs"], h.accountGit("rev-parse", candidate+":alpha-dir/notes.md"))
	equal(t, "the mine blob", file["mine"], h.accountGit("hash-object", filepath.Join(h.library, "alpha", "notes.md")))

	mine := ev["mine"].(string)
	head, mergeHead, msg := mergeState(t, h, "alpha")
	equal(t, "the checkout's HEAD", head, mine)
	equal(t, "the checkout's MERGE_HEAD", mergeHead, candidate)
	equal(t, "the checkout's MERGE_MSG", msg,
		"update alpha from "+first[:7]+" to "+second[:7]+", keeping its edits\n\n"+lineage.TrailerBase+": "+candidate+"\n")
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

	// The checkout alone gives the conflict back, hunk for hunk.
	inv := &invocation{git: gitx.New(h.env, false, func(string, ...any) {})}
	files, err := inv.readConflicts(context.Background(), gitx.AccountRepoPath(h.agentx), pendingCheckout(h, "alpha"), "alpha-dir")
	if err != nil {
		t.Fatal(err)
	}
	// Through JSON and back, so that both sides are marshalled from maps,
	// whose keys come out in one order.
	raw, err := json.Marshal(files)
	if err != nil {
		t.Fatal(err)
	}
	var read any
	if err := json.Unmarshal(raw, &read); err != nil {
		t.Fatal(err)
	}
	again, err := json.Marshal(read)
	if err != nil {
		t.Fatal(err)
	}
	emitted, err := json.Marshal(ev["files"])
	if err != nil {
		t.Fatal(err)
	}
	equal(t, "the files read again", string(again), string(emitted))

	text := h.run("skill", "update", "alpha")
	equal(t, "exit of the update run again", text.exit, 4)
	equal(t, "stderr of the update run again", text.stderr,
		"error: alpha has a merge with its update pending, so it cannot be updated until the merge is resolved or given up\n"+
			"hint: run 'agentx skill resolve alpha --abort' to give the merge up; the library directory stays as it is\n")
}

// TestAPendingMergeCheckoutIsAnOrdinaryMergeInProgress: the checkout of a
// pending merge is a merge git knows how to finish. git status there lists
// the unmerged path, and once the file is resolved and added, a plain git
// commit makes the merge commit, mine and the candidate its parents and
// the message MERGE_MSG holds its message, trailer included. The library
// directory is as it was all along.
func TestAPendingMergeCheckoutIsAnOrdinaryMergeInProgress(t *testing.T) {
	t.Parallel()
	h, s, _ := updateHarness(t)
	pendingMerge(t, h, s)
	library := onDisk(t, h.library)
	head, candidate, msg := mergeState(t, h, "alpha")

	contains(t, "git status", checkoutGit(t, h, "alpha", "status", "--porcelain"), "UU alpha-dir/notes.md")
	writeFile(t, filepath.Join(pendingCheckout(h, "alpha"), "alpha-dir", "notes.md"), "alpha notes, resolved\n")
	checkoutGit(t, h, "alpha", "add", "alpha-dir/notes.md")
	checkoutGit(t, h, "alpha", "commit", "--no-edit", "--quiet")
	equal(t, "the commit's parents", strings.TrimSpace(checkoutGit(t, h, "alpha", "rev-parse", "HEAD^@")), head+"\n"+candidate)
	equal(t, "the commit's message", checkoutGit(t, h, "alpha", "log", "-1", "--format=%B"), msg+"\n")
	equal(t, "the commit's trailer", strings.TrimSpace(checkoutGit(t, h, "alpha", "log", "-1", "--format=%(trailers:key="+lineage.TrailerBase+",valueonly)")), candidate)
	equal(t, "the library", onDisk(t, h.library), library)
}

// TestSkillUpdateConflictsOfEveryKind merges edits that conflict with an
// update every way a file can: a line changed on both sides; a file with
// lines of its own that look like conflict markers, whose hunks are read
// with markers longer than any of them; a file added on both sides, whose
// base is empty; a file the update deletes and the library changed, and
// one the library deleted and the update changed, each conflicting whole
// with the missing side null; a file moved to one place in the library and
// to another by the update, each of the three paths whole; a binary file
// changed on both sides, whole and binary; a file added on both sides
// alike but executable on one side only, which conflicts in its mode
// alone, whole; a file the library replaced with a symlink and the update
// changed, and a symlink of the library's where the update adds a file,
// each whole, a symlink here and a file in the update; and a file with no
// newline at its end, whose hunk ends as the file does. Every path is relative to the skill's directory, which
// the upstream calls otherwise. The text shows every hunk under its file
// and number, and says of each whole file how it conflicts.
func TestSkillUpdateConflictsOfEveryKind(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("kinds", true)
	markers := "a\n<<<<<<< HEAD\nb\n=========\nc\n>>>>>>>> x\n"
	s.skill("skills/kinds-dir", "kinds", "Conflicts of every kind", map[string]string{
		"a.md":       "a\n",
		"notes.md":   "one\ntwo\nthree\n",
		"markers.md": markers,
		"gone.md":    "gone\n",
		"kept.md":    "kept\n",
		"logo.bin":   "\x00base",
		"tail.md":    "x\ny",
		"moved.md":   "1\n2\n3\n4\n5\n6\n7\n8\n",
	})
	first := s.commit("first version")
	h.mustRun("skill", "add", s.url)
	s.write("skills/kinds-dir/notes.md", "one\ntwo, upstream\nthree\n")
	s.write("skills/kinds-dir/markers.md", strings.Replace(markers, "c\n", "c, upstream\n", 1))
	s.write("skills/kinds-dir/both.md", "added upstream\n")
	s.run("rm", "--quiet", "skills/kinds-dir/gone.md")
	s.write("skills/kinds-dir/kept.md", "kept, upstream\n")
	s.write("skills/kinds-dir/logo.bin", "\x00upstream")
	s.write("skills/kinds-dir/tail.md", "x\ny, upstream")
	s.run("mv", "skills/kinds-dir/moved.md", "skills/kinds-dir/moved-upstream.md")
	s.write("skills/kinds-dir/mode.sh", "the same\n")
	s.write("skills/kinds-dir/a.md", "a, upstream\n")
	s.write("skills/kinds-dir/x", "a file upstream\n")
	second := s.commit("second version")
	h.mustRun("skill", "check")
	lib := filepath.Join(h.library, "kinds")
	editLibrary(t, h, "kinds", "notes.md", "one\ntwo, mine\nthree\n")
	editLibrary(t, h, "kinds", "markers.md", strings.Replace(markers, "c\n", "c, mine\n", 1))
	editLibrary(t, h, "kinds", "both.md", "added here\n")
	editLibrary(t, h, "kinds", "gone.md", "gone, edited here\n")
	remove(t, filepath.Join(lib, "kept.md"))
	editLibrary(t, h, "kinds", "logo.bin", "\x00mine")
	editLibrary(t, h, "kinds", "tail.md", "x\ny, mine")
	if err := os.Rename(filepath.Join(lib, "moved.md"), filepath.Join(lib, "moved-here.md")); err != nil {
		t.Fatal(err)
	}
	editLibrary(t, h, "kinds", "mode.sh", "the same\n")
	chmod(t, filepath.Join(lib, "mode.sh"), 0o755)
	remove(t, filepath.Join(lib, "a.md"))
	for _, link := range []string{"a.md", "x"} {
		if err := os.Symlink("SKILL.md", filepath.Join(lib, link)); err != nil {
			t.Fatal(err)
		}
	}
	library := onDisk(t, h.library)

	out := h.run("--json", "skill", "update", "kinds")
	equal(t, "exit", out.exit, 4)
	ev := h.one(out.stdout, "conflict")
	equal(t, "files", conflictFiles(ev),
		"a.md:whole,both.md:1,gone.md:whole,kept.md:whole,logo.bin:binary,markers.md:1,mode.sh:whole,moved-here.md:whole,moved-upstream.md:whole,moved.md:whole,notes.md:1,tail.md:1,x:whole")
	equal(t, "notes.md", hunkOf(t, ev, "notes.md", 1), "two, mine\n|two\n|two, upstream\n")
	equal(t, "markers.md", hunkOf(t, ev, "markers.md", 1), "c, mine\n|c\n|c, upstream\n")
	equal(t, "both.md", hunkOf(t, ev, "both.md", 1), "added here\n||added upstream\n")
	equal(t, "tail.md", hunkOf(t, ev, "tail.md", 1), "y, mine|y|y, upstream")
	for _, c := range []struct {
		path               string
		base, mine, theirs bool
		binary             bool
	}{
		{path: "a.md", base: true, mine: true, theirs: true},
		{path: "both.md", mine: true, theirs: true},
		{path: "gone.md", base: true, mine: true},
		{path: "kept.md", base: true, theirs: true},
		{path: "logo.bin", base: true, mine: true, theirs: true, binary: true},
		{path: "mode.sh", mine: true, theirs: true},
		{path: "moved-here.md", mine: true},
		{path: "moved-upstream.md", theirs: true},
		{path: "moved.md", base: true},
		{path: "x", mine: true, theirs: true},
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
		equal(t, c.path+": binary", f["binary"], c.binary)
	}
	equal(t, "the library", onDisk(t, h.library), library)
	equal(t, "pending_merge", h.listed("kinds")["pending_merge"], true)

	// The same conflicts in text, once the merge is given up.
	h.accountGit("worktree", "remove", "-f", "-f", pendingCheckout(h, "kinds"))
	text := h.run("skill", "update", "kinds")
	equal(t, "exit in text", text.exit, 4)
	equal(t, "the text", text.stdout, "kinds conflicts with its update from "+first[:7]+" to "+second[:7]+" in 13 files\n"+
		"a.md: a symlink here, a file in the update\n"+
		"both.md:1\n<<<<<<< mine\nadded here\n||||||| base\n=======\nadded upstream\n>>>>>>> theirs\n"+
		"gone.md: changed here, deleted by the update\n"+
		"kept.md: deleted here, changed by the update\n"+
		"logo.bin: binary, changed here and by the update\n"+
		"markers.md:1\n<<<<<<< mine\nc, mine\n||||||| base\nc\n=======\nc, upstream\n>>>>>>> theirs\n"+
		"mode.sh: added here and by the update, executable on one side only\n"+
		"moved-here.md: moved here, and moved or deleted by the update\n"+
		"moved-upstream.md: moved here by the update, and moved or deleted here\n"+
		"moved.md: moved or deleted on both sides\n"+
		"notes.md:1\n<<<<<<< mine\ntwo, mine\n||||||| base\ntwo\n=======\ntwo, upstream\n>>>>>>> theirs\n"+
		"tail.md:1\n<<<<<<< mine\ny, mine\n||||||| base\ny\n=======\ny, upstream\n>>>>>>> theirs\n"+
		"x: a symlink here, a file in the update\n")
	equal(t, "stderr in text", text.stderr,
		"error: kinds conflicts with its update in 13 files, so the merge is pending and the library directory was left as it is\n"+
			"hint: run 'agentx skill resolve kinds' to resolve the conflicts, or 'agentx skill resolve kinds --abort' to give the merge up\n")
}

// TestSkillUpdateMergesASkillAtTheRootOfItsSource: a skill at the root of
// its source, whose import tree holds it under the repository's name, merges
// as any other: an edit the update leaves alone is kept, and one it
// overlaps conflicts at a path relative to the skill's directory, which
// resolving it names, and which completes the merge.
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
	equal(t, "files", conflictFiles(ev), "usage.md:1")
	equal(t, "the hunk", hunkOf(t, ev, "usage.md", 1), "root usage, edited here\n|root usage\n|root usage, revised\n")

	candidate := h.ref(lineage.CandidateRef("rooted"))
	h.mustRun("skill", "resolve", "rooted", "--hunk", "usage.md:1=theirs")
	sameTree(t, "the library directory once resolved", libraryTree(t, filepath.Join(h.library, "rooted")), map[string]string{
		"SKILL.md": fileBody(t, filepath.Join(s.work, "SKILL.md")), "notes.md": "root notes, revised\n", "usage.md": "root usage, revised\n",
	})
	equal(t, "the import branch once resolved", h.ref(lineage.ManagedRef("rooted")), candidate)
	noCheckout(t, h, "rooted")
	equal(t, "state once resolved", h.listed("rooted")["state"], stateCurrent)
}

// TestAPendingMergeBlocksWhatWouldReplaceOrRemoveTheSkill: while a merge
// is pending, a revert and a removal of the whole skill, with no --from
// or with --from universal, exit 4 with the hint that gives the merge up,
// and change nothing; so does the removal of what is left of the skill
// once its library directory is gone, while a removal of it from one
// configuration is refused as it is for any skill whose library directory
// is gone. Taking one placement away and putting placements back, with
// --keep-library too, move neither the library directory nor the import
// branch, leave the checkout's merge as it was, and are not blocked. A
// merge left pending after the command first read the refs is found under
// the lock, see
// TestSkillRevertRefusesAMergeLeftPendingBeforeTheLock and
// TestSkillRemoveOfWhatIsLeftRefusesAMergeLeftPendingBeforeTheLock.
func TestAPendingMergeBlocksWhatWouldReplaceOrRemoveTheSkill(t *testing.T) {
	t.Parallel()
	h, s, _ := updateHarness(t)
	pendingMerge(t, h, s)
	head, mergeHead, _ := mergeState(t, h, "alpha")
	refs := h.accountGit("for-each-ref", "--format=%(refname) %(objectname)")
	library := onDisk(t, h.library)
	hint := "run 'agentx skill resolve alpha --abort' to give the merge up; the library directory stays as it is"
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

// TestSkillRevertRefusesAMergeLeftPendingBeforeTheLock: a merge left
// pending while a revert reads the base version, after the revert judged
// the skill and before it took the lock, is found under the lock, and the
// revert discards nothing.
func TestSkillRevertRefusesAMergeLeftPendingBeforeTheLock(t *testing.T) {
	t.Parallel()
	h, _, _ := updateHarness(t)
	editLibrary(t, h, "alpha", "notes.md", editedNotes)
	tip := h.ref(lineage.ManagedRef("alpha"))
	stubGit(t, h, `#!/bin/sh
case " $* " in
*" ls-tree "*) `+fakePendingMerge(t, h, "alpha", tip)+` || exit 1 ;;
esac
exec `+realGit(t)+` "$@"
`)
	out := h.run("--json", "skill", "revert", "alpha")
	equal(t, "exit", out.exit, 4)
	equal(t, "message", h.one(out.stdout, "error")["message"],
		"alpha has a merge with its update pending, so it cannot be reverted until the merge is resolved or given up")
	equal(t, "alpha's notes", fileBody(t, filepath.Join(h.library, "alpha", "notes.md")), editedNotes)
	equal(t, "journals", journalCount(t, h), 0)
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
// skill lists both the newer update and the merge pending.
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
				equal(t, "files", conflictFiles(h.one(out.stdout, "conflict")), "notes.md:1")
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

// TestSkillUpdateConflictRecoversAtEveryBoundary kills an update whose
// merge conflicts with SIGKILL while it sets the merge up: once git added
// the checkout, and once git merged in it. Such an update writes no
// journal, and what it left is a merge pending as any command sees one:
// the next update of the skill exits 4 with the hint that gives the merge
// up, and the library, the copies, the import branch and the candidate are
// as they were.
func TestSkillUpdateConflictRecoversAtEveryBoundary(t *testing.T) {
	t.Parallel()
	for _, after := range []string{"worktree add", "merge-recursive"} {
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
			equal(t, "hint of the next update", h.one(next.stdout, "error")["hint"],
				"run 'agentx skill resolve alpha --abort' to give the merge up; the library directory stays as it is")
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
// merges, before the lock, and while merge-recursive sets the merge up in
// the checkout, under it. Either way the run answers with exit code 9 and
// leaves the machine as it was, no checkout and no registration of one
// left, and the next update leaves the merge pending.
func TestSkillUpdateStoppedWhileItMergesChangesNothing(t *testing.T) {
	t.Parallel()
	for _, sub := range []string{"merge-tree", "merge-recursive"} {
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

// TestSkillUpdateOfABranchStoredInAnOlderForm: a branch an earlier agentx
// wrote over a source's own tree lists its skill as modified whatever the
// library holds. A library directory holding exactly that version's files
// has no edits to merge, so it updates by replacement, and the skill is
// current at the new version afterwards.
func TestSkillUpdateOfABranchStoredInAnOlderForm(t *testing.T) {
	t.Parallel()
	h, s, _ := legacyHarness(t)
	h.storeInOlderForm(t, s)
	s.write("skills/nc/a.md", "a, revised\n")
	s.commit("a revised")
	h.mustRun("skill", "check")
	candidate := h.ref(lineage.CandidateRef("nc"))
	if candidate == "" {
		t.Fatal("the check pinned no candidate")
	}

	out := h.mustRun("--json", "skill", "update", "nc")
	if summary := h.one(out.stdout, "result")["summary"].(string); strings.Contains(summary, "merged") {
		t.Errorf("an update with no edits to merge says %q", summary)
	}
	equal(t, "the import branch", h.ref(lineage.ManagedRef("nc")), candidate)
	equal(t, "a.md", fileBody(t, filepath.Join(h.library, "nc", "a.md")), "a, revised\n")
	equal(t, "state", h.listed("nc")["state"], stateCurrent)
}

// TestSkillUpdateMergesWithNoAttributesOfTheUsers: git attributes of the
// user's own, a merge driver that joins both sides of every Markdown file
// say, decide nothing about how an update merges: an edit the update
// overlaps still conflicts, the merge is left pending with its hunk, the
// file unmerged in the checkout, and the library directory is as it was.
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
	equal(t, "files", conflictFiles(ev), "notes.md:1")
	equal(t, "the hunk", hunkOf(t, ev, "notes.md", 1), crlf(editedNotes)+"|alpha notes\r\n|alpha notes, revised upstream\r\n")
	equal(t, "the unmerged paths", unmerged(t, h, "alpha"), "1 alpha-dir/notes.md\n2 alpha-dir/notes.md\n3 alpha-dir/notes.md")
	equal(t, "the library", onDisk(t, h.library), library)
}

// TestSkillUpdateConflictsInTheCheckoutAsMergeTreeFoundIt: a file whose
// lines repeat, as braces do, conflicts in the checkout where merge-tree
// found it conflicting, since both pair its lines by histogram diff: the
// merge is left pending with the file's three versions in the index, not
// set up with nothing in it to resolve.
func TestSkillUpdateConflictsInTheCheckoutAsMergeTreeFoundIt(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("kit", true)
	lines := func(tokens ...string) string { return strings.Join(tokens, "\n") + "\n" }
	s.skill("skills/braces", "braces", "Nests its braces", map[string]string{"code.txt": lines("{", "{", "x", "{", "{", "a", "}", "a", "}")})
	s.commit("first version")
	h.mustRun("source", "add", s.url)
	h.mustRun("skill", "add", s.url, "--skill", "braces")
	s.write("skills/braces/code.txt", lines("{", "{", "x", "{", "x", "a", "a", "}"))
	s.commit("second version")
	h.mustRun("skill", "check")
	editLibrary(t, h, "braces", "code.txt", lines("{", "x", "{", "{", "a", "a", "}"))

	out := h.run("--json", "skill", "update", "braces")
	equal(t, "exit", out.exit, 4)
	equal(t, "files", conflictFiles(h.one(out.stdout, "conflict")), "code.txt:1")
	equal(t, "the unmerged paths", unmerged(t, h, "braces"), "1 braces/code.txt\n2 braces/code.txt\n3 braces/code.txt")
}

// TestSkillUpdateLeavesPendingWhatTheSkillsOwnAttributesMergeCleanly: the
// skill's own .gitattributes applies in the checkout, so a union merge
// driver it names for a file merge-tree found conflicting merges that file
// cleanly there. The merge is still left pending, with nothing unmerged
// and no file to resolve, and the library, the import branch and the
// candidate are as they were.
func TestSkillUpdateLeavesPendingWhatTheSkillsOwnAttributesMergeCleanly(t *testing.T) {
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
	equal(t, "files", conflictFiles(h.one(out.stdout, "conflict")), "")
	_, mergeHead, _ := mergeState(t, h, "union")
	equal(t, "MERGE_HEAD", mergeHead, candidate)
	equal(t, "the unmerged paths", unmerged(t, h, "union"), "")
	equal(t, "the merged file in the checkout", fileBody(t, filepath.Join(pendingCheckout(h, "union"), "union", "notes.md")), "notes, edited here\nnotes, revised upstream\n")
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
				equal(t, "files", conflictFiles(h.one(out.stdout, "conflict")), "notes.md:1")
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

// TestSkillUpdateKeepsThePermissionsOfWhatItLeavesAlone: an update lays
// its version out afresh, and what the library directory held that the
// update does not change keeps the permissions it had: a file of the
// user's and a file of the version, both made private, a private directory
// of the user's, and the skill's directory itself, whether the update
// merges edits or replaces a directory nobody edited. A file the update
// changes is laid out as the version has it.
func TestSkillUpdateKeepsThePermissionsOfWhatItLeavesAlone(t *testing.T) {
	t.Parallel()
	for _, edited := range []bool{true, false} {
		name := "a skill nobody edited"
		if edited {
			name = "an edited skill"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h, s, _ := updateHarness(t)
			lib := filepath.Join(h.library, "alpha")
			private := map[string]os.FileMode{"": 0o700, "SKILL.md": 0o600, "notes.md": 0o600}
			if edited {
				writeFile(t, mkdirs(t, filepath.Join(lib, "private"), "token.txt"), "a secret of my own\n")
				private["private"], private[filepath.Join("private", "token.txt")] = 0o700, 0o600
			}
			for path, mode := range private {
				if err := os.Chmod(filepath.Join(lib, path), mode); err != nil {
					t.Fatal(err)
				}
			}
			checked(t, h, s)

			h.mustRun("skill", "update", "alpha")
			for path, mode := range private {
				if path == "notes.md" {
					continue
				}
				info, err := os.Stat(filepath.Join(lib, path))
				if err != nil {
					t.Fatal(err)
				}
				if got := info.Mode().Perm(); got != mode {
					t.Errorf("%q is %v after the update, want %v", path, got, mode)
				}
			}
			equal(t, "notes.md", fileBody(t, filepath.Join(lib, "notes.md")), "alpha notes, revised upstream\n")
			want := stateCurrent
			if edited {
				want = stateModified
			}
			equal(t, "state", h.listed("alpha")["state"], want)
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

// TestSkillRemoveOfWhatIsLeftRefusesAMergeLeftPendingBeforeTheLock: a
// merge left pending while the removal of what is left of a skill reads
// its refs again under the lock, after it judged the skill, is found
// there, and the removal takes nothing away: not the import branch, not
// the pending merge, not a placement.
func TestSkillRemoveOfWhatIsLeftRefusesAMergeLeftPendingBeforeTheLock(t *testing.T) {
	t.Parallel()
	h, _, _ := updateHarness(t)
	tip := h.ref(lineage.ManagedRef("alpha"))
	remove(t, filepath.Join(h.library, "alpha"))
	home := onDisk(t, h.home)
	marker := filepath.Join(t.TempDir(), "read")
	stubGit(t, h, `#!/bin/sh
case " $* " in
*" for-each-ref "*`+lineage.UpstreamRemovedRef("alpha")+`*)
  if [ -e `+shellWord(marker)+` ]; then
    `+fakePendingMerge(t, h, "alpha", tip)+` || exit 1
  fi
  : > `+shellWord(marker)+` ;;
esac
exec `+realGit(t)+` "$@"
`)
	out := h.run("--json", "skill", "remove", "alpha")
	equal(t, "exit", out.exit, 4)
	equal(t, "hint", h.one(out.stdout, "error")["hint"], "run 'agentx skill resolve alpha --abort' to give the merge up; the library directory stays as it is")
	equal(t, "journals", journalCount(t, h), 0)
	equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), tip)
	equal(t, "the pending merge's HEAD", strings.TrimSpace(checkoutGit(t, h, "alpha", "rev-parse", "HEAD")), tip)
	equal(t, "the placements", onDisk(t, h.home), home)
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

// TestRecoveryPrunesStaleMergeCheckoutsUnderARelativeHome: agentx home
// named relative to the working directory, a registration of a checkout
// whose directory is gone goes all the same.
func TestRecoveryPrunesStaleMergeCheckoutsUnderARelativeHome(t *testing.T) {
	// Not parallel: it changes the process's working directory.
	h, _, _ := updateHarness(t)
	h.accountGit("worktree", "add", "--quiet", "--detach", "--lock", "--reason", pendingReason,
		pendingCheckout(h, "beta"), h.ref(lineage.ManagedRef("beta")))
	remove(t, pendingCheckout(h, "beta"))
	t.Chdir(filepath.Dir(h.agentx))
	h.env["AGENTX_HOME"] = filepath.Base(h.agentx)

	h.mustRun("config", "set", "label", "pruned")
	if list := h.accountGit("worktree", "list", "--porcelain"); strings.Contains(list, filepath.Join("merges", "beta")) {
		t.Errorf("the registration of beta's checkout is still there:\n%s", list)
	}
}
