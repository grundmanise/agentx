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

// trailer is the value of one trailer of a commit's message, read back
// with plain git.
func trailer(t *testing.T, h *harness, commit, key string) string {
	t.Helper()
	for _, line := range strings.Split(h.accountGit("log", "-1", "--format=%B", commit), "\n") {
		if k, v, ok := strings.Cut(line, ": "); ok && k == key {
			return v
		}
	}
	return ""
}

// TestSkillUpdateMergesANonOverlappingEdit: a skill edited where its update
// changes nothing merges cleanly. The import branch moves to the candidate
// and the candidate goes, as for any update; the library directory holds
// the new version with the edits on top of it, every file the update
// added, deleted or made executable included, and a directory of the
// user's whose own ignore file names everything in it, so the skill stays
// modified, and skill diff shows the edits and nothing else. Of the two
// copies, the one that still held the version installed is refreshed with
// the merged library, and the one edited where it is kept with its
// warning. No merge is left pending.
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
	second := newVersion(t, s)
	want := secondTree(t, s)
	want["SKILL.md"], want["mine.md"] = skillMD, "a file of my own\n"
	want[filepath.Join("private", ".gitignore")], want[filepath.Join("private", "notes.md")] = "*\n", "notes git would ignore\n"
	h.mustRun("skill", "check")
	tip, candidate := h.ref(lineage.ManagedRef("alpha")), h.ref(lineage.CandidateRef("alpha"))

	out := h.mustRun("--json", "skill", "update", "alpha")
	equal(t, "summary", h.one(out.stdout, "result")["summary"],
		"updated alpha from "+first[:7]+" to "+second[:7]+" and merged its edits cleanly, 1 copy placement refreshed, 1 placement skipped")
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), editedCopyWarning(cursor))
	equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), candidate)
	equal(t, "the candidate ref", h.ref(lineage.CandidateRef("alpha")), "")
	equal(t, "the merge ref", h.ref(lineage.MergeRef("alpha")), "")
	contains(t, "the branch's reflog", h.accountGit("reflog", "show", "--format=%H", lineage.ManagedRef("alpha")), tip)
	sameTree(t, "the library directory", libraryTree(t, lib), want)
	if !executable(t, filepath.Join(lib, "scripts", "run.sh")) {
		t.Error("scripts/run.sh is not executable after the merge")
	}
	nothingAt(t, "old.md", filepath.Join(lib, "old.md"))
	sameTree(t, "claude's copy", libraryTree(t, claude), want)
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
	equal(t, "what skill diff shows", strings.Join(diffs, ", "), "SKILL.md modified, mine.md added, private/.gitignore added, private/notes.md added")
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
// and the stage blobs of the file. The merge is a commit under the skill's
// merge ref, read back with plain git: its parents are mine, the library
// directory committed on the base version, and the candidate, and its three
// trailers name the base, mine and the candidate. A later invocation sees
// the merge pending in skill list, and merging the three trailer commits
// again gives the same files and hunks.
func TestSkillUpdateLeavesAConflictPending(t *testing.T) {
	t.Parallel()
	h, s, _ := updateHarness(t)
	newVersion(t, s)
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
	equal(t, "the hunk", hunkOf(t, ev, "notes.md", 1), editedNotes+"|alpha notes\n|alpha notes, revised upstream\n")
	file := fileOf(t, ev, "notes.md")
	equal(t, "the base blob", file["base"], h.accountGit("rev-parse", tip+":alpha-dir/notes.md"))
	equal(t, "the theirs blob", file["theirs"], h.accountGit("rev-parse", candidate+":alpha-dir/notes.md"))
	equal(t, "the mine blob", file["mine"], h.accountGit("hash-object", filepath.Join(h.library, "alpha", "notes.md")))

	merge := h.ref(lineage.MergeRef("alpha"))
	mine := ev["mine"].(string)
	equal(t, "the pending merge's parents", h.accountGit("rev-parse", merge+"^@"), mine+"\n"+candidate)
	equal(t, "mine's parent", h.accountGit("rev-parse", mine+"^@"), tip)
	equal(t, "mine's notes.md", h.accountGit("cat-file", "blob", mine+":alpha-dir/notes.md"), strings.TrimSuffix(editedNotes, "\n"))
	equal(t, "Agentx-Merge-Base", trailer(t, h, merge, lineage.TrailerMergeBase), tip)
	equal(t, "Agentx-Merge-Mine", trailer(t, h, merge, lineage.TrailerMergeMine), mine)
	equal(t, "Agentx-Merge-Theirs", trailer(t, h, merge, lineage.TrailerMergeTheirs), candidate)
	equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), tip)
	equal(t, "the candidate ref", h.ref(lineage.CandidateRef("alpha")), candidate)
	equal(t, "the library", onDisk(t, h.library), library)
	equal(t, "the clients' directories", onDisk(t, h.home), home)
	equal(t, "mutations", mutationVersion(t, h), before+1)
	equal(t, "journals", journalCount(t, h), 0)

	listed := h.listed("alpha")
	equal(t, "state", listed["state"], stateModified)
	equal(t, "pending_merge", fmt.Sprint(listed["pending_merge"]), "map[unresolved:1]")
	if listed["candidate"] == nil {
		t.Error("the skill no longer lists the update it has not applied")
	}
	contains(t, "skill list", h.mustRun("skill", "list").stdout, "modified, "+updateAvailable+", merge pending (1 unresolved)")

	// The trailers alone give the conflict back, hunk for hunk.
	r := gitx.New(h.env, false, func(string, ...any) {})
	m := lineage.Merge{
		Base:   trailer(t, h, merge, lineage.TrailerMergeBase),
		Mine:   trailer(t, h, merge, lineage.TrailerMergeMine),
		Theirs: trailer(t, h, merge, lineage.TrailerMergeTheirs),
	}
	res, err := mergeVersions(context.Background(), r, gitx.AccountRepoPath(h.agentx), "alpha-dir", m)
	if err != nil {
		t.Fatal(err)
	}
	// Through JSON and back, so that both sides are marshalled from maps,
	// whose keys come out in one order.
	raw, err := json.Marshal(res.files)
	if err != nil {
		t.Fatal(err)
	}
	var files any
	if err := json.Unmarshal(raw, &files); err != nil {
		t.Fatal(err)
	}
	again, err := json.Marshal(files)
	if err != nil {
		t.Fatal(err)
	}
	emitted, err := json.Marshal(ev["files"])
	if err != nil {
		t.Fatal(err)
	}
	equal(t, "the files merged again", string(again), string(emitted))
	equal(t, "the tree merged again", res.tree, h.accountGit("rev-parse", merge+"^{tree}"))

	text := h.run("skill", "update", "alpha")
	equal(t, "exit of the update run again", text.exit, 4)
	equal(t, "stderr of the update run again", text.stderr,
		"error: alpha has a merge with its update pending, so it cannot be updated until the merge is resolved or given up\n"+
			"hint: run 'agentx skill resolve alpha --abort' to give the merge up; the library directory stays as it is\n")
}

// TestSkillUpdateConflictsOfEveryKind merges edits that conflict with an
// update every way a file can: a line changed on both sides; a file with
// lines of its own that look like conflict markers, whose hunks are read
// with markers longer than any of them; a file added on both sides, whose
// base is empty; a file the update deletes and the library changed, and
// one the library deleted and the update changed, each conflicting whole
// with the missing side null; a file moved to one place in the library and
// to another by the update, each of the three paths whole; a binary file
// changed on both sides, whole and binary; and a file with no newline at
// its end, whose hunk ends as the file does. Every path is relative to the skill's directory, which
// the upstream calls otherwise. The text shows every hunk under its file
// and number, and says of each whole file how it conflicts.
func TestSkillUpdateConflictsOfEveryKind(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("kinds", true)
	markers := "a\n<<<<<<< HEAD\nb\n=========\nc\n>>>>>>>> x\n"
	s.skill("skills/kinds-dir", "kinds", "Conflicts of every kind", map[string]string{
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
	library := onDisk(t, h.library)

	out := h.run("--json", "skill", "update", "kinds")
	equal(t, "exit", out.exit, 4)
	ev := h.one(out.stdout, "conflict")
	equal(t, "files", conflictFiles(ev),
		"both.md:1,gone.md:whole,kept.md:whole,logo.bin:binary,markers.md:1,moved-here.md:whole,moved-upstream.md:whole,moved.md:whole,notes.md:1,tail.md:1")
	equal(t, "notes.md", hunkOf(t, ev, "notes.md", 1), "two, mine\n|two\n|two, upstream\n")
	equal(t, "markers.md", hunkOf(t, ev, "markers.md", 1), "c, mine\n|c\n|c, upstream\n")
	equal(t, "both.md", hunkOf(t, ev, "both.md", 1), "added here\n||added upstream\n")
	equal(t, "tail.md", hunkOf(t, ev, "tail.md", 1), "y, mine|y|y, upstream")
	for _, c := range []struct {
		path               string
		base, mine, theirs bool
		binary             bool
	}{
		{path: "both.md", mine: true, theirs: true},
		{path: "gone.md", base: true, mine: true},
		{path: "kept.md", base: true, theirs: true},
		{path: "logo.bin", base: true, mine: true, theirs: true, binary: true},
		{path: "moved-here.md", mine: true},
		{path: "moved-upstream.md", theirs: true},
		{path: "moved.md", base: true},
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
	equal(t, "the merge ref's unresolved files", h.accountGit("log", "-1", "--format=%s", lineage.MergeRef("kinds")), "pending merge of kinds: 10 files unresolved")
	equal(t, "pending_merge", fmt.Sprint(h.listed("kinds")["pending_merge"]), "map[unresolved:10]")

	// The same conflicts in text, from a machine that has not merged yet.
	h.accountGit("update-ref", "-d", lineage.MergeRef("kinds"))
	text := h.run("skill", "update", "kinds")
	equal(t, "exit in text", text.exit, 4)
	equal(t, "the text", text.stdout, "kinds conflicts with its update from "+first[:7]+" to "+second[:7]+" in 10 files\n"+
		"both.md:1\n<<<<<<< mine\nadded here\n||||||| base\n=======\nadded upstream\n>>>>>>> theirs\n"+
		"gone.md: changed here, deleted by the update\n"+
		"kept.md: deleted here, changed by the update\n"+
		"logo.bin: binary, changed here and by the update\n"+
		"markers.md:1\n<<<<<<< mine\nc, mine\n||||||| base\nc\n=======\nc, upstream\n>>>>>>> theirs\n"+
		"moved-here.md: moved here, and moved or deleted by the update\n"+
		"moved-upstream.md: moved here by the update, and moved or deleted here\n"+
		"moved.md: moved or deleted on both sides\n"+
		"notes.md:1\n<<<<<<< mine\ntwo, mine\n||||||| base\ntwo\n=======\ntwo, upstream\n>>>>>>> theirs\n"+
		"tail.md:1\n<<<<<<< mine\ny, mine\n||||||| base\ny\n=======\ny, upstream\n>>>>>>> theirs\n")
	equal(t, "stderr in text", text.stderr,
		"error: kinds conflicts with its update in 10 files, so the merge is pending and the library directory was left as it is\n"+
			"hint: run 'agentx skill resolve kinds' to resolve the conflicts, or 'agentx skill resolve kinds --abort' to give the merge up\n")
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
	equal(t, "files", conflictFiles(ev), "usage.md:1")
	equal(t, "the hunk", hunkOf(t, ev, "usage.md", 1), "root usage, edited here\n|root usage\n|root usage, revised\n")
}

// TestAPendingMergeBlocksWhatWouldReplaceOrRemoveTheSkill: while a merge
// is pending, a revert and a removal of the whole skill, with no --from
// or with --from universal, exit 4 with the hint that gives the merge up,
// and change nothing; so does the removal of what is left of the skill
// once its library directory is gone, while a removal of it from one
// configuration is refused as it is for any skill whose library directory
// is gone. Taking one placement away and repairing placements move
// neither the library directory nor the import branch the merge holds,
// and are not blocked. A merge left pending after the command first read
// the refs is found under the lock, see
// TestSkillRevertRefusesAMergeLeftPendingBeforeTheLock and
// TestSkillRemoveOfWhatIsLeftRefusesAMergeLeftPendingBeforeTheLock.
func TestAPendingMergeBlocksWhatWouldReplaceOrRemoveTheSkill(t *testing.T) {
	t.Parallel()
	h, s, _ := updateHarness(t)
	pendingMerge(t, h, s)
	merge := h.ref(lineage.MergeRef("alpha"))
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
	h.mustRun("skill", "repair", "alpha")
	equal(t, "the merge ref after a placement was removed and repaired", h.ref(lineage.MergeRef("alpha")), merge)
	equal(t, "the library after a placement was removed and repaired", onDisk(t, h.library), library)

	remove(t, filepath.Join(h.library, "alpha"))
	out := h.run("--json", "skill", "remove", "alpha")
	equal(t, "exit of the removal of what is left", out.exit, 4)
	equal(t, "hint of the removal of what is left", h.one(out.stdout, "error")["hint"], hint)
	equal(t, "the merge ref after that", h.ref(lineage.MergeRef("alpha")), merge)
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
	git := realGit(t) + " --git-dir=" + shellWord(gitx.AccountRepoPath(h.agentx))
	stubGit(t, h, `#!/bin/sh
case " $* " in
*" ls-tree "*) `+git+` update-ref `+lineage.MergeRef("alpha")+` `+tip+` || exit 1 ;;
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
// leaves the merge where it is, still naming the version it merged; the
// skill lists both the newer update and the merge pending.
func TestSkillCheckDuringAPendingMergeMovesOnlyTheCandidate(t *testing.T) {
	t.Parallel()
	h, s, _ := updateHarness(t)
	pendingMerge(t, h, s)
	merge, candidate := h.ref(lineage.MergeRef("alpha")), h.ref(lineage.CandidateRef("alpha"))
	library := onDisk(t, h.library)
	s.write("skills/alpha-dir/notes.md", "alpha notes, revised again upstream\n")
	third := s.commit("third version")

	h.mustRun("skill", "check")
	moved := h.ref(lineage.CandidateRef("alpha"))
	if moved == "" || moved == candidate {
		t.Fatalf("the check left the candidate at %q, from %q", moved, candidate)
	}
	equal(t, "the merge ref", h.ref(lineage.MergeRef("alpha")), merge)
	equal(t, "the version the merge merged", trailer(t, h, merge, lineage.TrailerMergeTheirs), candidate)
	equal(t, "the library", onDisk(t, h.library), library)
	listed := h.listed("alpha")
	equal(t, "the update listed", listed["candidate"].(map[string]any)["upstream_commit"], third)
	equal(t, "pending_merge", fmt.Sprint(listed["pending_merge"]), "map[unresolved:1]")
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

// firstWriteScript lets a command run to its first ref transaction and
// kills it the moment that is done: a process stopped after the one live
// write of a journal that has one, before it could record that the journal
// is applied.
const firstWriteScript = `
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

// TestSkillUpdateMergeRecoversAtEveryBoundary kills a clean merge with
// SIGKILL once its journal is on disk, then leaves the machine as a
// process killed after each later step would: the import branch moved, the
// library directory retained, the merged version published, the copy
// retained, the copy refreshed, and the candidate deleted with the journal
// not yet told. A last case is killed for real right after that deletion.
// The next command recovers each one, and the update is then whole: the
// branch at the candidate, the candidate gone, the library and the unedited
// copy holding the new version with the edits on it, the edited copy kept,
// no merge pending and nothing staged or retained left behind.
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
			equal(t, "the merge ref", h.ref(lineage.MergeRef("alpha")), "")
			sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "alpha")), want)
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
// merge conflicts once its journal, one step that creates the merge ref,
// is on disk: before the step, after it with the journal not yet told, and
// for real right after the ref transaction. The next command recovers each
// one, and the merge is then pending at the commit the journal names, with
// the library, the import branch and the candidate as they were.
func TestSkillUpdateConflictRecoversAtEveryBoundary(t *testing.T) {
	t.Parallel()
	for stop := 0; stop <= 2; stop++ {
		name := fmt.Sprintf("after %d steps", stop)
		if stop == 2 {
			name = "killed after its one live write"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h, s, _ := updateHarness(t)
			checked(t, h, s)
			editLibrary(t, h, "alpha", "notes.md", editedNotes)
			tip, candidate := h.ref(lineage.ManagedRef("alpha")), h.ref(lineage.CandidateRef("alpha"))
			library, home := onDisk(t, h.library), onDisk(t, h.home)

			var merge string
			if stop == 2 {
				out := killedChild(t, h, "TestUpdateChildProcess", updateChildEnv, "alpha", firstWriteScript)
				merge = h.ref(lineage.MergeRef("alpha"))
				if merge == "" {
					t.Fatalf("the update was not killed after it created the merge ref:\n%s", out)
				}
				equal(t, "journals the killed update left", journalCount(t, h), 1)
			} else {
				killedChild(t, h, "TestUpdateChildProcess", updateChildEnv, "alpha", killedUpdateScript)
				journal := readJournal(t, h)
				if len(journal) != 1 || journal[0].Kind != "ref" || journal[0].Ref != lineage.MergeRef("alpha") || journal[0].Old != "" {
					t.Fatalf("the journal's steps are %+v, want the creation of the merge ref", journal)
				}
				merge = journal[0].New
				equal(t, "the merge ref when the update was killed", h.ref(lineage.MergeRef("alpha")), "")
				applyUpdateSteps(t, h, journal, stop)
			}

			if got := h.run("config", "set", "label", "recovered"); got.exit != 0 {
				t.Fatalf("the command after the killed update: exit %d\n%s", got.exit, got.stderr)
			}
			equal(t, "journals after recovery", journalCount(t, h), 0)
			equal(t, "the merge ref", h.ref(lineage.MergeRef("alpha")), merge)
			equal(t, "Agentx-Merge-Theirs", trailer(t, h, merge, lineage.TrailerMergeTheirs), candidate)
			equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), tip)
			equal(t, "the candidate ref", h.ref(lineage.CandidateRef("alpha")), candidate)
			equal(t, "the library", onDisk(t, h.library), library)
			equal(t, "the clients' directories", onDisk(t, h.home), home)
			equal(t, "pending_merge", fmt.Sprint(h.listed("alpha")["pending_merge"]), "map[unresolved:1]")
		})
	}
}

// TestSkillUpdateRefusesAMergeWhoseLibraryChangedBeforeTheLock: what a
// merge merged is the library directory as the update read it. An edit
// made after that directory was committed as mine, while git merges it and
// before the lock, is found under the lock. One made while the directory
// is written into the account repo is found by the write, before git
// merges anything, whether the file edited is one the base version holds
// too or the user's own, which the account repo holds nothing of. Either
// way the update refuses with exit code 6, whether its merge was clean or
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
		{name: "a directory edited while it is written", file: "mine.md", content: "a file of my own\n", during: "hash-object", changed: "SKILL.md"},
		{name: "a file of the user's edited while it is written", file: "mine.md", content: "a file of my own\n", during: "hash-object", changed: "mine.md"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, s, _ := updateHarness(t)
			checked(t, h, s)
			editLibrary(t, h, "alpha", c.file, c.content)
			tip, candidate := h.ref(lineage.ManagedRef("alpha")), h.ref(lineage.CandidateRef("alpha"))
			before := mutationVersion(t, h)
			usage := filepath.Join(h.library, "alpha", c.changed)
			calls := filepath.Join(t.TempDir(), "calls")
			stubGit(t, h, `#!/bin/sh
case " $* " in
*" merge-tree "*) echo merge-tree >> `+shellWord(calls)+` ;;
esac
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
			if b, err := os.ReadFile(calls); c.during == "hash-object" && err == nil && len(b) > 0 {
				t.Errorf("the write did not refuse: git merged %q", b)
			}
			equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), tip)
			equal(t, "the candidate", h.ref(lineage.CandidateRef("alpha")), candidate)
			equal(t, "the merge ref", h.ref(lineage.MergeRef("alpha")), "")
			equal(t, "journals", journalCount(t, h), 0)
			equal(t, "mutations", mutationVersion(t, h), before)
			equal(t, "what is left beside the library", strings.Join(hiddenEntries(t, h.library), " "), "")
		})
	}
}

// TestSkillUpdateStoppedWhileItMergesChangesNothing stops an update with
// SIGTERM while git merges the edits with the update, before the lock and
// before any journal: the run answers with exit code 9 and leaves the
// machine as it was, no merge pending, and the next update merges.
func TestSkillUpdateStoppedWhileItMergesChangesNothing(t *testing.T) {
	t.Parallel()
	h, s, _ := updateHarness(t)
	checked(t, h, s)
	editLibrary(t, h, "alpha", "notes.md", editedNotes)
	tip, candidate := h.ref(lineage.ManagedRef("alpha")), h.ref(lineage.CandidateRef("alpha"))
	library := onDisk(t, h.library)
	ready := filepath.Join(t.TempDir(), "merging")
	path := h.env["PATH"]
	hangingGit(t, h, "merge-tree", ready)

	code, stderr := signalled(t, h, stopRun{ready: ready, sigs: []syscall.Signal{syscall.SIGTERM},
		args: []string{"skill", "update", "alpha", "--color", "off"}})
	killLeftover(t, ready)
	h.env["PATH"] = path

	equal(t, "the exit code of a stopped update", code, exitInterrupted.exit)
	contains(t, "stderr", stderr, "error: interrupted")
	equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), tip)
	equal(t, "the candidate", h.ref(lineage.CandidateRef("alpha")), candidate)
	equal(t, "the merge ref", h.ref(lineage.MergeRef("alpha")), "")
	equal(t, "the library", onDisk(t, h.library), library)
	equal(t, "journals", journalCount(t, h), 0)

	equal(t, "exit of the next update", h.run("skill", "update", "alpha").exit, 4)
	if h.ref(lineage.MergeRef("alpha")) == "" {
		t.Error("the next update left no merge pending")
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
// overlaps still conflicts, the merge is left pending with its hunk, and
// the library directory is as it was.
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
	equal(t, "the hunk", hunkOf(t, ev, "notes.md", 1), editedNotes+"|alpha notes\n|alpha notes, revised upstream\n")
	if h.ref(lineage.MergeRef("alpha")) == "" {
		t.Error("the update left no merge pending")
	}
	equal(t, "the library", onDisk(t, h.library), library)
}

// TestSkillUpdateConflictsWhereKindsMeet merges edits with an update where
// a file meets a directory or a symlink at one path, which git records by
// moving one of the two aside under a path of its own: a file of the
// user's where the update adds a directory, a directory of the user's
// where the update adds a file, a file the user replaced with a symlink
// that the update changes, and a symlink of the user's where the update
// adds a file. Each is reported at its own path relative to the skill's
// directory, never the one git moved it to, with the stage blobs git holds
// for it and how it conflicts; and a file added on both sides alike but
// executable on one side only conflicts in its mode alone. None has hunks,
// and the library is left as it was.
func TestSkillUpdateConflictsWhereKindsMeet(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("meet", true)
	s.skill("skills/meet", "meet", "Kinds that meet", map[string]string{"notes.md": "notes\n"})
	first := s.commit("first version")
	h.mustRun("skill", "add", s.url)
	s.write("skills/meet/extra/a.md", "a directory upstream\n")
	s.write("skills/meet/docs", "a file upstream\n")
	s.write("skills/meet/notes.md", "notes, upstream\n")
	s.write("skills/meet/x", "a file upstream\n")
	s.write("skills/meet/mode.sh", "the same\n")
	second := s.commit("second version")
	h.mustRun("skill", "check")
	lib := filepath.Join(h.library, "meet")
	editLibrary(t, h, "meet", "extra", "a file here\n")
	writeFile(t, mkdirs(t, filepath.Join(lib, "docs"), "mine.md"), "a directory here\n")
	remove(t, filepath.Join(lib, "notes.md"))
	for _, link := range []string{"notes.md", "x"} {
		if err := os.Symlink("SKILL.md", filepath.Join(lib, link)); err != nil {
			t.Fatal(err)
		}
	}
	editLibrary(t, h, "meet", "mode.sh", "the same\n")
	if err := os.Chmod(filepath.Join(lib, "mode.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	tip, candidate := h.ref(lineage.ManagedRef("meet")), h.ref(lineage.CandidateRef("meet"))
	library := onDisk(t, h.library)

	out := h.run("--json", "skill", "update", "meet")
	equal(t, "exit", out.exit, 4)
	ev := h.one(out.stdout, "conflict")
	equal(t, "files", conflictFiles(ev), "docs:whole,extra:whole,mode.sh:whole,notes.md:whole,x:whole")
	mine := ev["mine"].(string)
	blob := func(commit, path string) string { return h.accountGit("rev-parse", commit+":meet/"+path) }
	for _, c := range []struct {
		path               string
		base, mine, theirs string
	}{
		{path: "docs", theirs: blob(candidate, "docs")},
		{path: "extra", mine: blob(mine, "extra")},
		{path: "mode.sh", mine: blob(mine, "mode.sh"), theirs: blob(candidate, "mode.sh")},
		{path: "notes.md", base: blob(tip, "notes.md"), mine: blob(mine, "notes.md"), theirs: blob(candidate, "notes.md")},
		{path: "x", mine: blob(mine, "x"), theirs: blob(candidate, "x")},
	} {
		f := fileOf(t, ev, c.path)
		for _, side := range []struct{ name, want string }{{"base", c.base}, {"mine", c.mine}, {"theirs", c.theirs}} {
			got, _ := f[side.name].(string)
			equal(t, c.path+": "+side.name, got, side.want)
		}
		equal(t, c.path+": binary", f["binary"], false)
	}
	for _, f := range ev["files"].([]any) {
		if path := f.(map[string]any)["path"].(string); strings.Contains(path, "~") {
			t.Errorf("the conflict names %s, which is no path of the skill", path)
		}
	}
	equal(t, "the library", onDisk(t, h.library), library)
	equal(t, "the merge ref's unresolved files", h.accountGit("log", "-1", "--format=%s", lineage.MergeRef("meet")), "pending merge of meet: 5 files unresolved")
	equal(t, "pending_merge", fmt.Sprint(h.listed("meet")["pending_merge"]), "map[unresolved:5]")

	h.accountGit("update-ref", "-d", lineage.MergeRef("meet"))
	text := h.run("skill", "update", "meet")
	equal(t, "exit in text", text.exit, 4)
	equal(t, "the text", text.stdout, "meet conflicts with its update from "+first[:7]+" to "+second[:7]+" in 5 files\n"+
		"docs: a directory here, a file in the update\n"+
		"extra: a file here, a directory in the update\n"+
		"mode.sh: added here and by the update, executable on one side only\n"+
		"notes.md: a symlink here, a file in the update\n"+
		"x: a symlink here, a file in the update\n")
	equal(t, "the library after that", onDisk(t, h.library), library)
}

// TestSkillUpdateKeepsAFileAddedInADirectoryTheUpdateRenamed: a file the
// user added inside a directory the update renames merges cleanly and
// stays where the user put it, beside the renamed directory; git is not
// left to guess that it should move with the rest.
func TestSkillUpdateKeepsAFileAddedInADirectoryTheUpdateRenamed(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("renamed", true)
	s.skill("skills/renamed", "renamed", "A directory renamed", map[string]string{"docs/a.md": "a\n", "docs/b.md": "b\n"})
	s.commit("first version")
	h.mustRun("skill", "add", s.url)
	s.run("mv", "skills/renamed/docs", "skills/renamed/guide")
	s.commit("second version")
	h.mustRun("skill", "check")
	lib := filepath.Join(h.library, "renamed")
	editLibrary(t, h, "renamed", filepath.Join("docs", "mine.md"), "a file of my own\n")
	candidate := h.ref(lineage.CandidateRef("renamed"))

	out := h.mustRun("--json", "skill", "update", "renamed")
	contains(t, "summary", h.one(out.stdout, "result")["summary"].(string), " and merged its edits cleanly")
	equal(t, "the import branch", h.ref(lineage.ManagedRef("renamed")), candidate)
	equal(t, "the merge ref", h.ref(lineage.MergeRef("renamed")), "")
	sameTree(t, "the library directory", libraryTree(t, lib), map[string]string{
		"SKILL.md":                       fileBody(t, filepath.Join(s.work, "skills", "renamed", "SKILL.md")),
		filepath.Join("guide", "a.md"):   "a\n",
		filepath.Join("guide", "b.md"):   "b\n",
		filepath.Join("docs", "mine.md"): "a file of my own\n",
	})
	equal(t, "state", h.listed("renamed")["state"], stateModified)
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
// its refs, after that first read and before the lock, is found under the
// lock, and the removal takes nothing away: not the import branch, not the
// merge ref, not a placement.
func TestSkillRemoveOfWhatIsLeftRefusesAMergeLeftPendingBeforeTheLock(t *testing.T) {
	t.Parallel()
	h, _, _ := updateHarness(t)
	tip := h.ref(lineage.ManagedRef("alpha"))
	remove(t, filepath.Join(h.library, "alpha"))
	home := onDisk(t, h.home)
	marker := filepath.Join(t.TempDir(), "read")
	git := realGit(t) + " --git-dir=" + shellWord(gitx.AccountRepoPath(h.agentx))
	stubGit(t, h, `#!/bin/sh
case " $* " in
*" for-each-ref "*`+lineage.MergeRef("alpha")+`*)
  if [ ! -e `+shellWord(marker)+` ]; then
    `+realGit(t)+` "$@" || exit 1
    : > `+shellWord(marker)+`
    `+git+` update-ref `+lineage.MergeRef("alpha")+` `+tip+` || exit 1
    exit 0
  fi ;;
esac
exec `+realGit(t)+` "$@"
`)
	out := h.run("--json", "skill", "remove", "alpha")
	equal(t, "exit", out.exit, 4)
	equal(t, "hint", h.one(out.stdout, "error")["hint"], "run 'agentx skill resolve alpha --abort' to give the merge up; the library directory stays as it is")
	equal(t, "journals", journalCount(t, h), 0)
	equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), tip)
	equal(t, "the merge ref", h.ref(lineage.MergeRef("alpha")), tip)
	equal(t, "the placements", onDisk(t, h.home), home)
}
