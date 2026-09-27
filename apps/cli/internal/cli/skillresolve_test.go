package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// The versions of kit's files resolveHarness merges: what the first
// version holds, what the update holds and what the library directory is
// edited to hold. A file one side deletes has no entry on that side.
var (
	kitBase = map[string]string{
		"notes.md": "one\ntwo\nthree\nfour\nfive\n",
		"guide.md": "guide\n",
		"logo.bin": "\x00base",
		"gone.md":  "gone\n",
		"kept.md":  "kept\n",
		"usage.md": "usage\n",
	}
	kitTheirs = map[string]string{
		"notes.md": "uno\ntwo\nthree\nfour\ncinco\n",
		"guide.md": "guide, upstream\n",
		"logo.bin": "\x00upstream",
		"kept.md":  "kept, upstream\n",
		"new.md":   "new upstream\n",
	}
	kitMine = map[string]string{
		"notes.md": "ONE\ntwo\nthree\nfour\nFIVE\n",
		"guide.md": "guide, mine\n",
		"logo.bin": "\x00mine",
		"gone.md":  "gone, mine\n",
		"mine.md":  "mine\n",
	}
)

// kitConflicts is how kit's update conflicts with its edits, as
// conflictFiles says it: gone.md changed here and deleted by the update,
// guide.md with one hunk, kept.md deleted here and changed by the update,
// logo.bin binary and notes.md with two hunks.
const kitConflicts = "gone.md:whole,guide.md:1,kept.md:whole,logo.bin:binary,notes.md:2"

// resolveHarness is a machine with Claude Code and Cursor and one skill,
// kit, installed from a source with a copy in each client, whose upstream
// directory is not its library name. A check pins its second version, the
// library directory is edited as kitMine says, deleting kept.md, and the
// update leaves a merge pending with a conflict of every kind a resolve
// handles, see kitConflicts; usage.md is left alone by both sides and
// new.md and mine.md are added on one side each, which merge cleanly.
// Editor sessions lay their files out in a temporary directory of the
// test's own. It returns the update's conflict event.
func resolveHarness(t *testing.T) (h *harness, s *sourceRepo, ev jsonEvent) {
	t.Helper()
	h = newHarness(t)
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h.env["TMPDIR"] = tmp
	h.build(t, fixture{dirs: []string{".claude", ".cursor"}})
	s = h.newSourceRepo("kit", true)
	s.skill("skills/kit-dir", "kit", "A skill of every conflict", kitBase)
	s.commit("first version")
	h.mustRun("source", "add", s.url)
	h.mustRun("skill", "add", s.url, "--all", "--copy")
	for path, body := range kitTheirs {
		s.write("skills/kit-dir/"+path, body)
	}
	s.run("rm", "--quiet", "skills/kit-dir/gone.md")
	s.commit("second version")
	h.mustRun("skill", "check")
	for path, body := range kitMine {
		editLibrary(t, h, "kit", path, body)
	}
	remove(t, filepath.Join(h.library, "kit", "kept.md"))
	out := h.run("--json", "skill", "update", "kit")
	if out.exit != 4 {
		t.Fatalf("the update that should conflict: exit %d\n%s%s", out.exit, out.stdout, out.stderr)
	}
	ev = h.one(out.stdout, "conflict")
	equal(t, "the conflicts", conflictFiles(ev), kitConflicts)
	return h, s, ev
}

// kitTree is what kit's library directory holds once its merge completes
// with the files that conflict resolved to resolved, a file that is not
// there as "": the files neither side changed, what each side added, and
// the second version's SKILL.md, which is the first's.
func kitTree(t *testing.T, h *harness, resolved map[string]string) map[string]string {
	t.Helper()
	want := map[string]string{
		"SKILL.md": fileBody(t, filepath.Join(h.library, "kit", "SKILL.md")),
		"usage.md": "usage\n",
		"new.md":   "new upstream\n",
		"mine.md":  "mine\n",
	}
	for path, body := range resolved {
		if body != "" {
			want[path] = body
		}
	}
	return want
}

// upstreamOf is the upstream commit an import commit records, short, read
// back with plain git.
func upstreamOf(h *harness, commit string) string {
	return short(h.accountGit("log", "-1", "--format=%(trailers:key="+lineage.TrailerCommit+",valueonly)", commit))
}

// pendingMessage is the message of the commit a merge ref holds, read back
// with plain git.
func pendingMessage(h *harness, name string) string {
	return h.accountGit("log", "-1", "--format=%B", lineage.MergeRef(name))
}

// filesOf is the files of a conflict event, as JSON, for comparing two
// events' files whole.
func filesOf(t *testing.T, ev jsonEvent) string {
	t.Helper()
	raw, err := json.Marshal(ev["files"])
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// editorDirs is every directory an editor session left in the test's
// temporary directory.
func editorDirs(t *testing.T, h *harness) []string {
	t.Helper()
	dirs, err := filepath.Glob(filepath.Join(h.env["TMPDIR"], "agentx-resolve-*"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(dirs)
	return dirs
}

// TestSkillResolveShowsTheMergeAsItStands: skill resolve with no flag, in
// an invocation of its own after the update, reports the merge pending and
// changes nothing: one conflict event whose files and hunks are exactly the
// ones the update reported, numbered alike, the three versions it names,
// and in the text a line counting the files left, then every hunk under
// its file and number and every whole file with how it conflicts. Nothing
// is written: not the library, not a ref, not the version file.
func TestSkillResolveShowsTheMergeAsItStands(t *testing.T) {
	t.Parallel()
	h, s, updated := resolveHarness(t)
	merge := h.ref(lineage.MergeRef("kit"))
	refs := h.accountGit("for-each-ref", "--format=%(refname) %(objectname)")
	library, before := onDisk(t, h.library), mutationVersion(t, h)
	first, second := upstreamOf(h, lineage.ManagedRef("kit")), short(s.run("rev-parse", "HEAD"))

	out := h.run("--json", "skill", "resolve", "kit")
	equal(t, "exit", out.exit, 0)
	ev := h.one(out.stdout, "conflict")
	equal(t, "the files", filesOf(t, ev), filesOf(t, updated))
	for _, field := range []string{"name", "kind", "base", "mine", "theirs"} {
		equal(t, field, ev[field], updated[field])
	}
	equal(t, "summary", h.one(out.stdout, "result")["summary"],
		"kit has a merge pending with its update from "+first+" to "+second+": 5 of 5 files unresolved")

	text := h.run("skill", "resolve", "kit")
	equal(t, "exit in text", text.exit, 0)
	equal(t, "the text", text.stdout, "kit has a merge pending with its update from "+first+" to "+second+": 5 of 5 files unresolved\n"+
		"gone.md: changed here, deleted by the update\n"+
		"guide.md:1\n<<<<<<< mine\nguide, mine\n||||||| base\nguide\n=======\nguide, upstream\n>>>>>>> theirs\n"+
		"kept.md: deleted here, changed by the update\n"+
		"logo.bin: binary, changed here and by the update\n"+
		"notes.md:1\n<<<<<<< mine\nONE\n||||||| base\none\n=======\nuno\n>>>>>>> theirs\n"+
		"notes.md:2\n<<<<<<< mine\nFIVE\n||||||| base\nfive\n=======\ncinco\n>>>>>>> theirs\n")
	equal(t, "stderr in text", text.stderr, "")

	equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), merge)
	equal(t, "the refs", h.accountGit("for-each-ref", "--format=%(refname) %(objectname)"), refs)
	equal(t, "the library", onDisk(t, h.library), library)
	equal(t, "mutations", mutationVersion(t, h), before)
	equal(t, "journals", journalCount(t, h), 0)
}

// TestSkillResolveRefusesWithoutAMergeToResolve: a name the library does
// not hold and no lineage names is exit code 5; a managed skill with no
// merge pending, and an unmanaged one, are exit code 6, whatever the run
// was asked to do, and nothing is written.
func TestSkillResolveRefusesWithoutAMergeToResolve(t *testing.T) {
	t.Parallel()
	h, _, _ := updateHarness(t)
	if err := os.MkdirAll(filepath.Join(h.library, "own"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(h.library, "own", "SKILL.md"), skill("own", "A skill of my own"))
	for _, c := range []struct {
		args          []string
		exit          int
		message, hint string
	}{
		{args: []string{"ghost"}, exit: 5, message: `the library holds no skill called "ghost" at ` + h.library, hint: "run 'agentx skill list' to see what the library holds"},
		{args: []string{"alpha"}, exit: 6, message: "alpha has no merge pending",
			hint: "a merge is left pending by 'agentx skill update alpha' when your edits conflict with the update; run 'agentx skill list' to see which skills have one"},
		{args: []string{"alpha", "--abort"}, exit: 6, message: "alpha has no merge pending"},
		{args: []string{"alpha", "--hunk", "notes.md:1=mine"}, exit: 6, message: "alpha has no merge pending"},
		{args: []string{"own"}, exit: 6, message: "own has no merge pending"},
	} {
		what := strings.Join(c.args, " ")
		out := h.run(append([]string{"--json", "skill", "resolve"}, c.args...)...)
		equal(t, what+": exit", out.exit, c.exit)
		e := h.one(out.stdout, "error")
		equal(t, what+": message", e["message"], c.message)
		if c.hint != "" {
			equal(t, what+": hint", e["hint"], c.hint)
		}
	}
	equal(t, "journals", journalCount(t, h), 0)
}

// TestSkillResolveUsage: what the command line cannot mean is exit code 1
// before anything is read: --abort beside a choice or the editor, --hunk
// beside --editor, a file named without --editor, a --hunk that is not
// <file>:<index>=<side>, a side that is none of the three, and a path that
// leaves the skill's directory.
func TestSkillResolveUsage(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	for _, c := range []struct {
		args    []string
		message string
	}{
		{[]string{"kit", "--abort", "--hunk", "notes.md:1=mine"}, "--abort gives the merge up and takes neither --hunk nor --editor"},
		{[]string{"kit", "--abort", "--editor"}, "--abort gives the merge up and takes neither --hunk nor --editor"},
		{[]string{"kit", "--editor", "--hunk", "notes.md:1=mine"}, "choose parts with --hunk or open the files with --editor, not both in one run"},
		{[]string{"kit", "notes.md"}, "skill resolve takes one skill name; a file is named after --editor, or in --hunk <file>:<index>=<side>"},
		{[]string{"kit", "--hunk", "notes.md=mine"}, `--hunk "notes.md=mine" is not <file>:<index>=<side>`},
		{[]string{"kit", "--hunk", "notes.md:0=mine"}, `--hunk "notes.md:0=mine" is not <file>:<index>=<side>`},
		{[]string{"kit", "--hunk", "notes.md:x=mine"}, `--hunk "notes.md:x=mine" is not <file>:<index>=<side>`},
		{[]string{"kit", "--hunk", ":1=mine"}, `--hunk ":1=mine" is not <file>:<index>=<side>`},
		{[]string{"kit", "--hunk", "notes.md:1=ours"}, `--hunk "notes.md:1=ours" chooses "ours", which is not mine, theirs or both`},
		{[]string{"kit", "--hunk", "../notes.md:1=mine"}, `--hunk "../notes.md:1=mine" names no file of the skill`},
	} {
		what := strings.Join(c.args, " ")
		out := h.run(append([]string{"--json", "skill", "resolve"}, c.args...)...)
		equal(t, what+": exit", out.exit, 1)
		equal(t, what+": message", h.one(out.stdout, "error")["message"], c.message)
	}
}

// TestSkillResolveRefusesAChoiceTheMergeCannotTake: a --hunk the merge
// cannot take is exit code 1 and writes nothing: a file that does not
// conflict, a hunk a file does not have, both sides of a file that
// conflicts whole, two sides for one hunk, and a file some of whose hunks
// are given no side, which names every one of them.
func TestSkillResolveRefusesAChoiceTheMergeCannotTake(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	merge := h.ref(lineage.MergeRef("kit"))
	see := "run 'agentx skill resolve kit' to see every file left to resolve and its parts"
	for _, c := range []struct {
		hunks         []string
		message, hint string
	}{
		{hunks: []string{"usage.md:1=mine"}, message: "usage.md does not conflict in the merge of kit", hint: see},
		{hunks: []string{"guide.md:2=mine"}, message: "guide.md has 1 hunk, and no hunk 2", hint: see},
		{hunks: []string{"logo.bin:2=mine"}, message: "logo.bin conflicts as a whole file, so its one part is 1, not 2", hint: see},
		{hunks: []string{"gone.md:1=both"}, message: "gone.md conflicts as a whole file, so it is resolved to mine or theirs, not both", hint: see},
		{hunks: []string{"guide.md:1=mine", "guide.md:1=theirs"}, message: "guide.md:1 is given both mine and theirs", hint: see},
		{hunks: []string{"notes.md:2=mine"}, message: "notes.md has 2 hunks and --hunk chooses no side for 1",
			hint: "choose a side for every hunk of a file in the same run, as in --hunk notes.md:1=mine"},
		{hunks: []string{"guide.md:1=mine", "notes.md:1=theirs"}, message: "notes.md has 2 hunks and --hunk chooses no side for 2",
			hint: "choose a side for every hunk of a file in the same run, as in --hunk notes.md:2=mine"},
	} {
		args := []string{"--json", "skill", "resolve", "kit"}
		for _, hunk := range c.hunks {
			args = append(args, "--hunk", hunk)
		}
		what := strings.Join(c.hunks, " ")
		out := h.run(args...)
		equal(t, what+": exit", out.exit, 1)
		e := h.one(out.stdout, "error")
		equal(t, what+": message", e["message"], c.message)
		equal(t, what+": hint", e["hint"], c.hint)
		equal(t, what+": the merge ref", h.ref(lineage.MergeRef("kit")), merge)
	}
	equal(t, "journals", journalCount(t, h), 0)
}

// resolveKit resolves the files of kit's merge the hunks name, failing the
// test when the run does not exit with exit, and returns its outcome.
func resolveKit(t *testing.T, h *harness, exit int, hunks ...string) outcome {
	t.Helper()
	args := []string{"--json", "skill", "resolve", "kit"}
	for _, hunk := range hunks {
		args = append(args, "--hunk", hunk)
	}
	out := h.run(args...)
	if out.exit != exit {
		t.Fatalf("agentx skill resolve kit %s: exit %d, want %d\n%s%s", strings.Join(hunks, " "), out.exit, exit, out.stdout, out.stderr)
	}
	return out
}

// TestSkillResolveEachSideOfEveryKind resolves kit's merge every way a file
// can be: each hunk of a text file to mine, theirs or both, mine then
// theirs; a binary file to either side; a file one side deleted to the
// side that kept it or to the one that deleted it, which deletes it.
//
// Resolving some of the files rewrites the pending merge commit, read back
// with plain git in the next invocation: the same parents and the same
// trailers, a subject that counts the files left, the files resolved
// listed in its body, and each resolved file's content in its tree, a file
// resolved to the version git kept in the tree for it included, which the
// tree alone could not tell from one left unresolved. A later skill
// resolve shows the files left alone, and skill list counts them. Resolving
// the last one completes the merge in the same run, whether the files were
// resolved over two runs or all in one: the library directory holds the
// merged version, the import branch is at the update, and the candidate
// and the merge ref are gone. Of the two copies, the one that still held
// the version installed is refreshed and the one edited where it is kept
// with its warning, and the skill stays modified against its new base.
func TestSkillResolveEachSideOfEveryKind(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name     string
		runs     [][]string        // the --hunk of each run, the last one completing the merge
		resolved map[string]string // what each file that conflicts holds once completed, "" for none
	}{
		{
			name: "in two runs, mine first",
			runs: [][]string{
				{"notes.md:1=mine", "notes.md:2=theirs", "guide.md:1=both", "logo.bin:1=mine", "gone.md:1=mine"},
				{"kept.md:1=mine"},
			},
			resolved: map[string]string{
				"notes.md": "ONE\ntwo\nthree\nfour\ncinco\n", "guide.md": "guide, mine\nguide, upstream\n",
				"logo.bin": "\x00mine", "gone.md": "gone, mine\n", "kept.md": "",
			},
		},
		{
			name: "in one run, theirs first",
			runs: [][]string{{
				"notes.md:1=theirs", "notes.md:2=both", "guide.md:1=theirs", "logo.bin:1=theirs", "gone.md:1=theirs", "kept.md:1=theirs",
			}},
			resolved: map[string]string{
				"notes.md": "uno\ntwo\nthree\nfour\nFIVE\ncinco\n", "guide.md": "guide, upstream\n",
				"logo.bin": "\x00upstream", "gone.md": "", "kept.md": "kept, upstream\n",
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, _, _ := resolveHarness(t)
			claude := filepath.Join(h.home, ".claude", "skills", "kit")
			cursor := filepath.Join(h.home, ".cursor", "skills", "kit")
			editCopy(t, cursor)
			edited := libraryTree(t, cursor)
			tip, candidate := h.ref(lineage.ManagedRef("kit")), h.ref(lineage.CandidateRef("kit"))
			merge := h.ref(lineage.MergeRef("kit"))
			parents := h.accountGit("rev-parse", merge+"^@")
			trailers := h.accountGit("log", "-1", "--format=%(trailers)", merge)

			if len(c.runs) == 2 {
				out := resolveKit(t, h, 0, c.runs[0]...)
				equal(t, "summary of the first run", h.one(out.stdout, "result")["summary"], "resolved 4 files in the merge of kit, 1 file left to resolve")
				equal(t, "what the first run left", conflictFiles(h.one(out.stdout, "conflict")), "kept.md:whole")
				if got := h.eventsOfType(out.stdout, "library_skill"); len(got) != 0 {
					t.Errorf("a run that left the merge pending reported %v", got)
				}
				rewritten := h.ref(lineage.MergeRef("kit"))
				if rewritten == merge {
					t.Fatal("the first run left the pending merge commit as it was")
				}
				equal(t, "the parents after the first run", h.accountGit("rev-parse", rewritten+"^@"), parents)
				equal(t, "the trailers after the first run", h.accountGit("log", "-1", "--format=%(trailers)", rewritten), trailers)
				equal(t, "the message after the first run", pendingMessage(h, "kit"),
					"pending merge of kit: 1 file unresolved\n\nresolved \"gone.md\"\nresolved \"guide.md\"\nresolved \"logo.bin\"\nresolved \"notes.md\"\n\n"+trailers)
				for path, body := range c.resolved {
					if path != "kept.md" {
						equal(t, path+" in the rewritten tree", h.accountGit("cat-file", "blob", rewritten+":kit-dir/"+path), strings.TrimSuffix(body, "\n"))
					}
				}
				equal(t, "the import branch after the first run", h.ref(lineage.ManagedRef("kit")), tip)
				equal(t, "pending_merge", fmt.Sprint(h.listed("kit")["pending_merge"]), "map[unresolved:1]")
				contains(t, "skill list", h.mustRun("skill", "list").stdout, "merge pending (1 unresolved)")
				equal(t, "what is left, shown", conflictFiles(h.one(h.mustRun("--json", "skill", "resolve", "kit").stdout, "conflict")), "kept.md:whole")
			}

			out := resolveKit(t, h, 0, c.runs[len(c.runs)-1]...)
			if got := h.eventsOfType(out.stdout, "conflict"); len(got) != 0 {
				t.Errorf("the run that completed the merge reported %v", got)
			}
			equal(t, "summary", h.one(out.stdout, "result")["summary"],
				"resolved kit and updated it from "+upstreamOf(h, tip)+" to "+upstreamOf(h, candidate)+", 1 copy placement refreshed, 1 placement skipped")
			equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"),
				keptCopyWarning(cursor, "kit", "agentx skill remove kit --from cursor", "agentx skill place kit --to cursor --copy"))
			want := kitTree(t, h, c.resolved)
			sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "kit")), want)
			sameTree(t, "claude's copy", libraryTree(t, claude), want)
			sameTree(t, "cursor's copy", libraryTree(t, cursor), edited)
			equal(t, "the import branch", h.ref(lineage.ManagedRef("kit")), candidate)
			equal(t, "the candidate ref", h.ref(lineage.CandidateRef("kit")), "")
			equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), "")
			for _, dir := range []string{h.library, filepath.Dir(claude), filepath.Dir(cursor)} {
				equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
			}
			equal(t, "journals", journalCount(t, h), 0)
			ev := h.one(out.stdout, "library_skill")
			equal(t, "state", ev["state"], stateModified)
			for _, field := range []string{"candidate", "pending_merge"} {
				if _, ok := ev[field]; ok {
					t.Errorf("the completed skill carries %s: %v", field, ev[field])
				}
			}
			sameEvent(t, "the completion's library_skill and skill list's", ev, h.listed("kit"))
			if strings.Contains(h.mustRun("skill", "list").stdout, "merge pending") {
				t.Error("skill list still shows a merge pending")
			}
		})
	}
}

// kitSides resolves every file of kit's merge but kept.md, which
// completeKit resolves: the files resolve to kitResolved.
var kitSides = []string{"notes.md:1=mine", "notes.md:2=theirs", "guide.md:1=both", "logo.bin:1=mine", "gone.md:1=mine"}

// kitResolved is what the files that conflict hold once kitSides and
// kept.md:1=theirs resolve them.
var kitResolved = map[string]string{
	"notes.md": "ONE\ntwo\nthree\nfour\ncinco\n", "guide.md": "guide, mine\nguide, upstream\n",
	"logo.bin": "\x00mine", "gone.md": "gone, mine\n", "kept.md": "kept, upstream\n",
}

// TestSkillResolveKeepsANewerUpdate: a check that finds a newer version
// while the merge is pending moves the candidate. Completing the merge
// moves the import branch to the update the merge merged, the one
// Agentx-Merge-Theirs names, and never to the candidate, which stays as the
// skill's next update: skill list shows it, and the text says so.
func TestSkillResolveKeepsANewerUpdate(t *testing.T) {
	t.Parallel()
	h, s, _ := resolveHarness(t)
	theirs := trailer(t, h, h.ref(lineage.MergeRef("kit")), lineage.TrailerMergeTheirs)
	s.write("skills/kit-dir/usage.md", "usage, revised upstream\n")
	third := s.commit("third version")
	h.mustRun("skill", "check")
	newer := h.ref(lineage.CandidateRef("kit"))
	if newer == theirs || newer == "" {
		t.Fatalf("the check left the candidate at %q", newer)
	}
	resolveKit(t, h, 0, kitSides...)

	out := resolveKit(t, h, 0, "kept.md:1=theirs")
	contains(t, "summary", h.one(out.stdout, "result")["summary"].(string), "; the newer update the last check found stays for the next update")
	equal(t, "the import branch", h.ref(lineage.ManagedRef("kit")), theirs)
	equal(t, "the candidate", h.ref(lineage.CandidateRef("kit")), newer)
	equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), "")
	sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "kit")), kitTree(t, h, kitResolved))
	listed := h.listed("kit")
	equal(t, "the update listed", listed["candidate"].(map[string]any)["upstream_commit"], third)
	contains(t, "skill list", h.mustRun("skill", "list").stdout, updateAvailable)
}

// TestSkillResolveMergesAnEditMadeMeanwhile: an edit of the library
// directory made while the merge was pending, by any tool, is never lost.
// Completing compares the directory with mine by tree id and merges it
// again when it moved. An edit to a file no side of the merge conflicts in
// keeps every resolution, and the merge completes in the same run with the
// edit in it. An edit to a file resolved already makes it conflict anew:
// the run reports that file alone, keeps every other resolution, rewrites
// the pending merge with the directory as it is now as its mine, first
// parent and trailer, and exits 4; resolving the file then completes the
// merge with both edits.
func TestSkillResolveMergesAnEditMadeMeanwhile(t *testing.T) {
	t.Parallel()
	t.Run("an edit to a file that does not conflict", func(t *testing.T) {
		t.Parallel()
		h, _, _ := resolveHarness(t)
		candidate := h.ref(lineage.CandidateRef("kit"))
		resolveKit(t, h, 0, kitSides...)
		editLibrary(t, h, "kit", "usage.md", "usage, edited meanwhile\n")

		out := resolveKit(t, h, 0, "kept.md:1=theirs")
		contains(t, "summary", h.one(out.stdout, "result")["summary"].(string), "resolved kit, merged the edits made to it meanwhile and updated it from ")
		want := kitTree(t, h, kitResolved)
		want["usage.md"] = "usage, edited meanwhile\n"
		sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "kit")), want)
		equal(t, "the import branch", h.ref(lineage.ManagedRef("kit")), candidate)
		equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), "")
	})
	t.Run("an edit to a file resolved already", func(t *testing.T) {
		t.Parallel()
		h, _, updated := resolveHarness(t)
		candidate := h.ref(lineage.CandidateRef("kit"))
		resolveKit(t, h, 0, kitSides...)
		merge := h.ref(lineage.MergeRef("kit"))
		editLibrary(t, h, "kit", "guide.md", "guide, mine again\n")
		editLibrary(t, h, "kit", "usage.md", "usage, edited meanwhile\n")
		library := onDisk(t, h.library)

		out := resolveKit(t, h, 4, "kept.md:1=theirs")
		e := h.one(out.stdout, "error")
		equal(t, "code", e["code"], "pending_merge")
		equal(t, "message", e["message"], "kit was edited while its merge was pending, and merging it again conflicts in 1 file, so the merge is still pending")
		equal(t, "hint", e["hint"], "run 'agentx skill resolve kit' to see every file left to resolve")
		ev := h.one(out.stdout, "conflict")
		equal(t, "what conflicts anew", conflictFiles(ev), "guide.md:1")
		equal(t, "the hunk", hunkOf(t, ev, "guide.md", 1), "guide, mine again\n|guide\n|guide, upstream\n")
		rewritten := h.ref(lineage.MergeRef("kit"))
		mine := ev["mine"].(string)
		if mine == updated["mine"] || rewritten == merge {
			t.Fatalf("the merge was not merged again: mine %s, merge ref %s", mine, rewritten)
		}
		equal(t, "the parents", h.accountGit("rev-parse", rewritten+"^@"), mine+"\n"+candidate)
		equal(t, "Agentx-Merge-Mine", trailer(t, h, rewritten, lineage.TrailerMergeMine), mine)
		equal(t, "Agentx-Merge-Base", trailer(t, h, rewritten, lineage.TrailerMergeBase), updated["base"])
		equal(t, "Agentx-Merge-Theirs", trailer(t, h, rewritten, lineage.TrailerMergeTheirs), candidate)
		contains(t, "the message", pendingMessage(h, "kit"),
			"pending merge of kit: 1 file unresolved\n\nresolved \"gone.md\"\nresolved \"kept.md\"\nresolved \"logo.bin\"\nresolved \"notes.md\"\n\n")
		equal(t, "the library", onDisk(t, h.library), library)
		equal(t, "the import branch", h.ref(lineage.ManagedRef("kit")), updated["base"])
		equal(t, "what is left, shown", filesOf(t, h.one(h.mustRun("--json", "skill", "resolve", "kit").stdout, "conflict")), filesOf(t, ev))

		resolveKit(t, h, 0, "guide.md:1=theirs")
		want := kitTree(t, h, kitResolved)
		want["guide.md"], want["usage.md"] = "guide, upstream\n", "usage, edited meanwhile\n"
		sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "kit")), want)
		equal(t, "the import branch", h.ref(lineage.ManagedRef("kit")), candidate)
		equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), "")
	})
}

// clashHarness is a machine with Claude Code and one skill, kit, whose
// first version holds base and notes.md, "one", and whose update and edits
// conflict in notes.md, one hunk, and wherever upstream and edit, given
// the source and the library directory, make a file meet a directory. It
// returns once the update left the merge pending.
func clashHarness(t *testing.T, base map[string]string, upstream func(s *sourceRepo), edit func(lib string)) *harness {
	t.Helper()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("kit", true)
	first := map[string]string{"notes.md": "one\n"}
	maps.Copy(first, base)
	s.skill("skills/kit-dir", "kit", "A skill where a file meets a directory", first)
	s.commit("first version")
	h.mustRun("skill", "add", s.url)
	s.write("skills/kit-dir/notes.md", "uno\n")
	upstream(s)
	s.commit("second version")
	h.mustRun("skill", "check")
	editLibrary(t, h, "kit", "notes.md", "ONE\n")
	edit(filepath.Join(h.library, "kit"))
	if out := h.run("skill", "update", "kit"); out.exit != 4 {
		t.Fatalf("the update that should conflict: exit %d\n%s%s", out.exit, out.stdout, out.stderr)
	}
	return h
}

// inMerge is what kit's pending merge commit holds at path, relative to
// the skill's directory, read back with plain git: a file's content, or
// "a directory".
func inMerge(h *harness, path string) string {
	object := lineage.MergeRef("kit") + ":kit-dir/" + path
	if h.accountGit("cat-file", "-t", object) == "tree" {
		return "a directory"
	}
	return h.accountGit("cat-file", "blob", object)
}

// TestSkillResolveSwitchesAFileThatMeetsADirectory: a path where a file
// of one side meets a directory of the other is resolved to one side, then
// to the other, and the second choice is the one that holds: the directory
// comes back whole, as the update merged it, and the file goes, whether the
// switch is written into the pending merge or completes it in the same run.
// A file of the merge inside that directory, whose resolution went when
// the file took the directory's place, is left to resolve again.
func TestSkillResolveSwitchesAFileThatMeetsADirectory(t *testing.T) {
	t.Parallel()
	skillFile := func(t *testing.T, h *harness) string {
		return fileBody(t, filepath.Join(h.library, "kit", "SKILL.md"))
	}
	fileHere := func(t *testing.T) *harness {
		return clashHarness(t, nil,
			func(s *sourceRepo) { s.write("skills/kit-dir/x/a.md", "a, upstream\n") },
			func(lib string) { writeFile(t, filepath.Join(lib, "x"), "a file here\n") })
	}
	for _, completing := range []bool{false, true} {
		t.Run(fmt.Sprintf("a file here and a directory in the update, completing with the switch %v", completing), func(t *testing.T) {
			t.Parallel()
			h := fileHere(t)
			resolveKit(t, h, 0, "x:1=mine")
			equal(t, "x resolved to mine", inMerge(h, "x"), "a file here")
			if completing {
				resolveKit(t, h, 0, "notes.md:1=theirs", "x:1=theirs")
			} else {
				resolveKit(t, h, 0, "x:1=theirs")
				equal(t, "x resolved to theirs", inMerge(h, "x"), "a directory")
				equal(t, "x/a.md resolved to theirs", inMerge(h, "x/a.md"), "a, upstream")
				if names := h.accountGit("ls-tree", "-r", "--name-only", lineage.MergeRef("kit")); strings.Contains(names, "~") {
					t.Errorf("the merge still holds what git moved aside:\n%s", names)
				}
				resolveKit(t, h, 0, "notes.md:1=theirs")
			}
			sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "kit")),
				map[string]string{"SKILL.md": skillFile(t, h), "notes.md": "uno\n", "x/a.md": "a, upstream\n"})
			equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), "")
			equal(t, "state", h.listed("kit")["state"], stateCurrent)
		})
	}
	t.Run("a directory here and a file in the update", func(t *testing.T) {
		t.Parallel()
		h := clashHarness(t, nil,
			func(s *sourceRepo) { s.write("skills/kit-dir/x", "a file upstream\n") },
			func(lib string) { writeFile(t, mkdirs(t, filepath.Join(lib, "x"), "a.md"), "a directory here\n") })
		resolveKit(t, h, 0, "x:1=theirs")
		equal(t, "x resolved to theirs", inMerge(h, "x"), "a file upstream")
		resolveKit(t, h, 0, "x:1=mine")
		equal(t, "x resolved to mine", inMerge(h, "x"), "a directory")
		equal(t, "x/a.md resolved to mine", inMerge(h, "x/a.md"), "a directory here")
		resolveKit(t, h, 0, "x:1=theirs")
		resolveKit(t, h, 0, "notes.md:1=theirs", "x:1=mine")
		sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "kit")),
			map[string]string{"SKILL.md": skillFile(t, h), "notes.md": "uno\n", "x/a.md": "a directory here\n"})
		equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), "")
		equal(t, "state", h.listed("kit")["state"], stateModified)
	})
	t.Run("a file of the merge inside the directory", func(t *testing.T) {
		t.Parallel()
		h := clashHarness(t, map[string]string{"x/a.md": "a\n"},
			func(s *sourceRepo) { s.write("skills/kit-dir/x/a.md", "a, upstream\n") },
			func(lib string) {
				remove(t, filepath.Join(lib, "x"))
				writeFile(t, filepath.Join(lib, "x"), "a file here\n")
			})
		equal(t, "the conflicts", conflictFiles(h.one(h.mustRun("--json", "skill", "resolve", "kit").stdout, "conflict")), "notes.md:1,x:whole,x/a.md:whole")
		resolveKit(t, h, 0, "x:1=mine", "x/a.md:1=mine")
		out := resolveKit(t, h, 0, "x:1=theirs")
		equal(t, "summary", h.one(out.stdout, "result")["summary"], "resolved x in the merge of kit, 2 files left to resolve")
		equal(t, "what is left", conflictFiles(h.one(out.stdout, "conflict")), "notes.md:1,x/a.md:whole")
		equal(t, "x/a.md, back as the update merged it", inMerge(h, "x/a.md"), "a, upstream")
		contains(t, "the message", pendingMessage(h, "kit"), "pending merge of kit: 2 files unresolved\n\nresolved \"x\"\n\n")
		resolveKit(t, h, 0, "notes.md:1=theirs", "x/a.md:1=theirs")
		sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "kit")),
			map[string]string{"SKILL.md": skillFile(t, h), "notes.md": "uno\n", "x/a.md": "a, upstream\n"})
		equal(t, "state", h.listed("kit")["state"], stateCurrent)
	})
}

// accountGitInput is accountGit with input on its stdin, its output
// trimmed.
func (h *harness) accountGitInput(input string, args ...string) string {
	h.t.Helper()
	r := gitx.New(h.env, false, func(string, ...any) {})
	out, err := r.IsolatedInput(context.Background(), gitx.AccountRepoPath(h.agentx), strings.NewReader(input), args...)
	if err != nil {
		h.t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(out)
}

// versionOf writes a commit of kit that holds what commit does, with the
// files changes names, relative to the skill's directory, set to what it
// gives them, "" deleting one, and the message and parents commit has, and
// returns it: another commit of the same version for no changes, or a
// version of kit no source holds. Nothing points at it.
func versionOf(t *testing.T, h *harness, commit string, changes map[string]string) string {
	t.Helper()
	entries := map[string]string{}
	for _, line := range strings.Split(h.accountGit("ls-tree", commit+":kit-dir"), "\n") {
		if _, name, ok := strings.Cut(line, "\t"); ok {
			entries[name] = line
		}
	}
	for path, body := range changes {
		delete(entries, path)
		if body != "" {
			entries[path] = "100644 blob " + h.accountGitInput(body, "hash-object", "-w", "--stdin") + "\t" + path
		}
	}
	lines := slices.Sorted(maps.Values(entries))
	dir := h.accountGitInput(strings.Join(lines, "\n")+"\n", "mktree")
	root := h.accountGitInput("040000 tree "+dir+"\tkit-dir\n", "mktree")
	args := []string{"commit-tree", root}
	for _, parent := range strings.Fields(h.accountGit("rev-parse", commit+"^@")) {
		args = append(args, "-p", parent)
	}
	// A paragraph ahead of the message's own makes a second commit of the
	// same version another commit; its trailers stay the last paragraph.
	return h.accountGitInput("", append(args, "-m", "written again", "-m", h.accountGit("log", "-1", "--format=%B", commit))...)
}

// TestSkillResolveMergesAgainOnAMovedImportBranch: an import branch moved
// since the merge was left pending, which only something outside agentx
// does, is merged on again rather than refused. Showing the merge warns
// of it and changes nothing. A run that resolves a file writes it into the
// merge as it was recorded, then merges the merge again with the version
// the branch points at as its base: a file that conflicts as it did keeps
// its resolution, and a file the move changed is left to resolve and
// reported, with exit code 4, in a pending merge rewritten with the new
// base in Agentx-Merge-Base. Resolving it completes the merge. A branch
// moved to another commit of the same version changes nothing a user sees,
// and the run that resolves the last file completes the merge on it.
func TestSkillResolveMergesAgainOnAMovedImportBranch(t *testing.T) {
	t.Parallel()
	t.Run("to another version", func(t *testing.T) {
		t.Parallel()
		h, _, updated := resolveHarness(t)
		candidate := h.ref(lineage.CandidateRef("kit"))
		resolveKit(t, h, 0, kitSides...)
		merge := h.ref(lineage.MergeRef("kit"))
		moved := versionOf(t, h, updated["base"].(string), map[string]string{"notes.md": "one\ntwo\nthree\nfour\ncinco\n"})
		h.accountGit("update-ref", lineage.ManagedRef("kit"), moved)
		library := onDisk(t, h.library)

		shown := h.run("--json", "skill", "resolve", "kit")
		equal(t, "exit of the show", shown.exit, 0)
		equal(t, "what is shown", conflictFiles(h.one(shown.stdout, "conflict")), "kept.md:whole")
		equal(t, "the warning of the show", strings.Join(warnings(h, shown.stderr), "\n"),
			"the import branch refs/heads/managed/kit moved since the merge of kit was left pending; the next run that resolves a file merges it again on the version the branch points at, keeping every resolution that still applies")
		equal(t, "the merge ref after the show", h.ref(lineage.MergeRef("kit")), merge)

		out := resolveKit(t, h, 4, "kept.md:1=theirs")
		e := h.one(out.stdout, "error")
		equal(t, "code", e["code"], "pending_merge")
		equal(t, "message", e["message"], "the import branch refs/heads/managed/kit moved while the merge of kit was pending, and merging it again leaves 1 file to resolve, so the merge is still pending")
		equal(t, "hint", e["hint"], "run 'agentx skill resolve kit' to see every file left to resolve")
		ev := h.one(out.stdout, "conflict")
		equal(t, "the base of the conflict", ev["base"], moved)
		equal(t, "what conflicts anew", conflictFiles(ev), "notes.md:1")
		equal(t, "the hunk", hunkOf(t, ev, "notes.md", 1), "ONE\n|one\n|uno\n")
		rewritten := h.ref(lineage.MergeRef("kit"))
		equal(t, "the parents", h.accountGit("rev-parse", rewritten+"^@"), updated["mine"].(string)+"\n"+candidate)
		equal(t, "Agentx-Merge-Base", trailer(t, h, rewritten, lineage.TrailerMergeBase), moved)
		equal(t, "Agentx-Merge-Mine", trailer(t, h, rewritten, lineage.TrailerMergeMine), updated["mine"])
		equal(t, "Agentx-Merge-Theirs", trailer(t, h, rewritten, lineage.TrailerMergeTheirs), candidate)
		contains(t, "the message", pendingMessage(h, "kit"),
			"pending merge of kit: 1 file unresolved\n\nresolved \"gone.md\"\nresolved \"guide.md\"\nresolved \"kept.md\"\nresolved \"logo.bin\"\n\n")
		equal(t, "the library", onDisk(t, h.library), library)
		equal(t, "the import branch", h.ref(lineage.ManagedRef("kit")), moved)
		equal(t, "what is left, shown", filesOf(t, h.one(h.mustRun("--json", "skill", "resolve", "kit").stdout, "conflict")), filesOf(t, ev))

		resolveKit(t, h, 0, "notes.md:1=mine")
		want := kitTree(t, h, kitResolved)
		want["notes.md"] = "ONE\ntwo\nthree\nfour\nFIVE\n"
		sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "kit")), want)
		equal(t, "the import branch once completed", h.ref(lineage.ManagedRef("kit")), candidate)
		equal(t, "the merge ref once completed", h.ref(lineage.MergeRef("kit")), "")
		equal(t, "journals", journalCount(t, h), 0)
	})
	t.Run("to another commit of the same version", func(t *testing.T) {
		t.Parallel()
		h, _, updated := resolveHarness(t)
		candidate := h.ref(lineage.CandidateRef("kit"))
		moved := versionOf(t, h, updated["base"].(string), nil)
		h.accountGit("update-ref", lineage.ManagedRef("kit"), moved)

		out := resolveKit(t, h, 0, kitSides...)
		equal(t, "summary", h.one(out.stdout, "result")["summary"], "resolved 4 files in the merge of kit, 1 file left to resolve")
		ev := h.one(out.stdout, "conflict")
		equal(t, "the base of the conflict", ev["base"], moved)
		equal(t, "what is left", conflictFiles(ev), "kept.md:whole")
		equal(t, "Agentx-Merge-Base", trailer(t, h, h.ref(lineage.MergeRef("kit")), lineage.TrailerMergeBase), moved)

		// Moved once more, the merge is merged again and completed in the
		// run that resolves its last file.
		h.accountGit("update-ref", lineage.ManagedRef("kit"), versionOf(t, h, moved, nil))
		resolveKit(t, h, 0, "kept.md:1=theirs")
		sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "kit")), kitTree(t, h, kitResolved))
		equal(t, "the import branch", h.ref(lineage.ManagedRef("kit")), candidate)
		equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), "")
	})
}

// TestSkillResolveRefusesAMergeItCannotMergeAgain: an import branch moved
// to the very update the merge merges leaves nothing to merge, and one moved
// to a commit agentx cannot read names no version to merge on: either is
// exit code 6 whatever the run asks, and nothing is written; giving the
// merge up still works.
func TestSkillResolveRefusesAMergeItCannotMergeAgain(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, message string
		to            func(h *harness, updated jsonEvent) string
	}{
		{name: "moved to the update", message: "the import branch refs/heads/managed/kit moved to the update the merge of kit merges, so there is nothing left to merge",
			to: func(h *harness, _ jsonEvent) string { return h.ref(lineage.CandidateRef("kit")) }},
		{name: "moved to a commit with no lineage", message: "the import branch refs/heads/managed/kit records no version agentx can read",
			to: func(_ *harness, updated jsonEvent) string { return updated["mine"].(string) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, _, updated := resolveHarness(t)
			merge := h.ref(lineage.MergeRef("kit"))
			h.accountGit("update-ref", lineage.ManagedRef("kit"), c.to(h, updated))
			library := onDisk(t, h.library)
			h.env["EDITOR"] = "true"
			for _, args := range [][]string{{}, {"--hunk", "logo.bin:1=mine"}, {"--editor"}} {
				what := strings.Join(args, " ")
				out := h.run(append([]string{"--json", "skill", "resolve", "kit"}, args...)...)
				equal(t, what+": exit", out.exit, 6)
				e := h.one(out.stdout, "error")
				equal(t, what+": message", e["message"], c.message)
				equal(t, what+": hint", e["hint"], "run 'agentx skill resolve kit --abort' to give the merge up; the library directory stays as it is")
				equal(t, what+": the merge ref", h.ref(lineage.MergeRef("kit")), merge)
			}
			equal(t, "the library", onDisk(t, h.library), library)
			h.mustRun("skill", "resolve", "kit", "--abort")
			equal(t, "the merge ref once given up", h.ref(lineage.MergeRef("kit")), "")
		})
	}
}

// editorDir makes a directory an editor session of the skill called name
// could have left, owned by the process pid, or kept by its run when pid
// is "kept", for the agentx home at home, and returns it.
func editorDir(t *testing.T, h *harness, name, pid, home string) string {
	t.Helper()
	dir, err := os.MkdirTemp(h.env["TMPDIR"], "agentx-resolve-")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "owner"), pid+"\n"+home+"\n"+h.ref(lineage.MergeRef(name))+"\n")
	writeFile(t, mkdirs(t, filepath.Join(dir, name), "notes.md"), "<<<<<<< mine\n")
	return dir
}

// ownPid is the process id of the test binary, as an owner file names it:
// a run that is still going.
var ownPid = strconv.Itoa(os.Getpid())

// keptWarning is the warning of a session whose run kept the files the
// editor was given in dir.
func keptWarning(dir string) string {
	return "the files you edited are kept in " + dir + " until the merge of kit completes or is given up; a new session opens the files afresh, so copy what you typed from there"
}

// ownerOf is the first line of the owner file of an editor session's
// directory: the process id of its run, or kept.
func ownerOf(t *testing.T, dir string) string {
	t.Helper()
	first, _, _ := strings.Cut(fileBody(t, filepath.Join(dir, "owner")), "\n")
	return first
}

// TestSkillResolveAbortLeavesTheLibraryAsItWas: --abort gives the merge up,
// in a journal of one step that deletes the merge ref, and leaves the
// library directory, every placement, the import branch and the candidate
// byte for byte as they were. The files an editor session of the skill
// left for this home go with it, a session still open and one its run kept
// included, and those of another skill or another home stay. The skill is
// reported as it now stands, with no merge pending, and giving it up again
// is exit code 6.
func TestSkillResolveAbortLeavesTheLibraryAsItWas(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	resolveKit(t, h, 0, kitSides...)
	tip, candidate := h.ref(lineage.ManagedRef("kit")), h.ref(lineage.CandidateRef("kit"))
	library, placed := onDisk(t, h.library), onDisk(t, h.home)
	ours, kept := editorDir(t, h, "kit", ownPid, h.agentx), editorDir(t, h, "kit", "kept", h.agentx)
	other := editorDir(t, h, "other", ownPid, h.agentx)
	elsewhere := editorDir(t, h, "kit", ownPid, filepath.Join(h.agentx, "elsewhere"))
	before := mutationVersion(t, h)

	out := h.run("--json", "skill", "resolve", "kit", "--abort")
	equal(t, "exit", out.exit, 0)
	equal(t, "summary", h.one(out.stdout, "result")["summary"], "gave up the merge of kit; the library directory is as it was")
	ev := h.one(out.stdout, "library_skill")
	if _, ok := ev["pending_merge"]; ok {
		t.Errorf("the skill still carries pending_merge: %v", ev["pending_merge"])
	}
	equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), "")
	equal(t, "the import branch", h.ref(lineage.ManagedRef("kit")), tip)
	equal(t, "the candidate", h.ref(lineage.CandidateRef("kit")), candidate)
	equal(t, "the library", onDisk(t, h.library), library)
	equal(t, "the placements", onDisk(t, h.home), placed)
	equal(t, "mutations", mutationVersion(t, h), before+1)
	equal(t, "journals", journalCount(t, h), 0)
	nothingAt(t, "the skill's editor files", ours)
	nothingAt(t, "the skill's kept editor files", kept)
	for _, dir := range []string{other, elsewhere} {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("%s went with the merge: %v", dir, err)
		}
	}
	if strings.Contains(h.mustRun("skill", "list").stdout, "merge pending") {
		t.Error("skill list still shows a merge pending")
	}
	equal(t, "the update, still there", h.listed("kit")["candidate"] != nil, true)

	again := h.run("--json", "skill", "resolve", "kit", "--abort")
	equal(t, "exit of a second abort", again.exit, 6)
	equal(t, "message of a second abort", h.one(again.stdout, "error")["message"], "kit has no merge pending")

	text := h.run("skill", "update", "kit")
	equal(t, "the update once given up conflicts again", text.exit, 4)
}

// editorStub writes an editor script, body under a #!/bin/sh line, into a
// directory of the test's own, and returns its path, for EDITOR or
// GIT_EDITOR to name. The script gets the files to edit as its arguments;
// run with --version alone, which is how writeShim makes sure it can be
// run, it does nothing.
func editorStub(t *testing.T, name, body string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stub := filepath.Join(dir, name)
	writeShim(t, stub, "#!/bin/sh\nif [ \"$*\" = --version ]; then exit 0; fi\n"+body)
	return stub
}

// resolvingEditor is an editor that records every file it is given, one
// path per line in args and each file's content in seen, then resolves it
// to "resolved in the editor". It uses nothing but shell builtins.
func resolvingEditor(t *testing.T) (stub, args, seen string) {
	t.Helper()
	logs := t.TempDir()
	args, seen = filepath.Join(logs, "args"), filepath.Join(logs, "seen")
	stub = editorStub(t, "resolving-editor", `for f in "$@"; do
	printf '%s\n' "$f" >> `+shellWord(args)+`
	while IFS= read -r line; do printf '%s\n' "$line"; done < "$f" >> `+shellWord(seen)+`
	printf 'resolved in the editor\n' > "$f"
done
`)
	return stub, args, seen
}

// kitWholeFiles resolves the files of kit's merge that conflict whole,
// which no editor can open, leaving guide.md and notes.md.
var kitWholeFiles = []string{"logo.bin:1=mine", "gone.md:1=mine", "kept.md:1=theirs"}

// TestSkillResolveInAnEditor opens the text files left to resolve in the
// user's editor, a stub named by EDITOR. Each is written into a directory
// of its own in the temporary directory, under the skill's name at its
// path in the skill, with zdiff3 markers naming mine, base and theirs, and
// nothing is registered in the account repo as a worktree.
//
// An editor that resolves every file completes the merge; one that leaves
// the markers resolves nothing and the merge stays pending, each file
// named in a warning; a file named after --editor is the only one opened,
// and resolving it rewrites the merge. The directory is gone afterwards
// either way. A file that conflicts whole cannot be edited, which is exit
// code 1 when it is named and exit code 6 when every file left is one. An
// editor that fails writes nothing and keeps the files, which a warning
// names: a later session leaves them alone, and completing the merge
// removes them.
func TestSkillResolveInAnEditor(t *testing.T) {
	t.Parallel()
	t.Run("an editor that resolves every file", func(t *testing.T) {
		t.Parallel()
		h, _, _ := resolveHarness(t)
		candidate := h.ref(lineage.CandidateRef("kit"))
		resolveKit(t, h, 0, kitWholeFiles...)
		stub, args, seen := resolvingEditor(t)
		h.env["EDITOR"] = stub

		out := h.run("--json", "skill", "resolve", "kit", "--editor")
		equal(t, "exit", out.exit, 0)
		contains(t, "summary", h.one(out.stdout, "result")["summary"].(string), "resolved kit and updated it from ")
		opened := strings.Split(strings.TrimSpace(fileBody(t, args)), "\n")
		if len(opened) != 2 || !strings.HasSuffix(opened[0], filepath.Join("kit", "guide.md")) || !strings.HasSuffix(opened[1], filepath.Join("kit", "notes.md")) {
			t.Errorf("the editor opened %q", opened)
		}
		for _, p := range opened {
			if !strings.HasPrefix(p, h.env["TMPDIR"]+string(filepath.Separator)+"agentx-resolve-") {
				t.Errorf("%s is not in a directory of the temporary directory", p)
			}
		}
		equal(t, "what the editor was given", fileBody(t, seen),
			"<<<<<<< mine\nguide, mine\n||||||| base\nguide\n=======\nguide, upstream\n>>>>>>> theirs\n"+
				"<<<<<<< mine\nONE\n||||||| base\none\n=======\nuno\n>>>>>>> theirs\ntwo\nthree\nfour\n<<<<<<< mine\nFIVE\n||||||| base\nfive\n=======\ncinco\n>>>>>>> theirs\n")
		resolved := map[string]string{"notes.md": "resolved in the editor\n", "guide.md": "resolved in the editor\n", "logo.bin": "\x00mine", "gone.md": "gone, mine\n", "kept.md": "kept, upstream\n"}
		sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "kit")), kitTree(t, h, resolved))
		equal(t, "the import branch", h.ref(lineage.ManagedRef("kit")), candidate)
		equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), "")
		equal(t, "what the session left", strings.Join(editorDirs(t, h), " "), "")
		equal(t, "the worktrees of the account repo", strings.Count(h.accountGit("worktree", "list", "--porcelain"), "worktree "), 1)
	})
	t.Run("an editor that leaves the markers", func(t *testing.T) {
		t.Parallel()
		h, _, _ := resolveHarness(t)
		merge := h.ref(lineage.MergeRef("kit"))
		h.env["EDITOR"] = editorStub(t, "idle-editor", "exit 0\n")

		out := h.run("--json", "skill", "resolve", "kit", "--editor")
		equal(t, "exit", out.exit, 0)
		equal(t, "summary", h.one(out.stdout, "result")["summary"], "resolved nothing in the merge of kit: 5 files left to resolve")
		equal(t, "what is left", conflictFiles(h.one(out.stdout, "conflict")), kitConflicts)
		equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"),
			"guide.md still holds conflict markers, or is gone, so it was left unresolved\n"+
				"notes.md still holds conflict markers, or is gone, so it was left unresolved")
		equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), merge)
		equal(t, "what the session left", strings.Join(editorDirs(t, h), " "), "")
		equal(t, "journals", journalCount(t, h), 0)
	})
	t.Run("a file named after --editor", func(t *testing.T) {
		t.Parallel()
		h, _, _ := resolveHarness(t)
		merge := h.ref(lineage.MergeRef("kit"))
		stub, args, _ := resolvingEditor(t)
		h.env["EDITOR"] = stub

		out := h.run("--json", "skill", "resolve", "kit", "--editor", "notes.md")
		equal(t, "exit", out.exit, 0)
		equal(t, "summary", h.one(out.stdout, "result")["summary"], "resolved notes.md in the merge of kit, 4 files left to resolve")
		opened := strings.TrimSpace(fileBody(t, args))
		if strings.Contains(opened, "\n") || !strings.HasSuffix(opened, filepath.Join("kit", "notes.md")) {
			t.Errorf("the editor opened %q", opened)
		}
		rewritten := h.ref(lineage.MergeRef("kit"))
		equal(t, "the parents", h.accountGit("rev-parse", rewritten+"^@"), h.accountGit("rev-parse", merge+"^@"))
		equal(t, "notes.md in the merge", h.accountGit("cat-file", "blob", rewritten+":kit-dir/notes.md"), "resolved in the editor")
		equal(t, "the subject", h.accountGit("log", "-1", "--format=%s", rewritten), "pending merge of kit: 4 files unresolved")
		equal(t, "what the session left", strings.Join(editorDirs(t, h), " "), "")
	})
	t.Run("files that conflict whole", func(t *testing.T) {
		t.Parallel()
		h, _, _ := resolveHarness(t)
		h.env["EDITOR"] = editorStub(t, "idle-editor", "exit 0\n")
		out := h.run("--json", "skill", "resolve", "kit", "--editor", "logo.bin")
		equal(t, "exit of a whole file named", out.exit, 1)
		e := h.one(out.stdout, "error")
		equal(t, "message", e["message"], "logo.bin conflicts as a whole file, so there is no text to edit")
		equal(t, "hint", e["hint"], "choose a side for it with --hunk logo.bin:1=mine or --hunk logo.bin:1=theirs")
		equal(t, "exit of a file that does not conflict", h.run("skill", "resolve", "kit", "--editor", "usage.md").exit, 1)

		resolveKit(t, h, 0, "notes.md:1=mine", "notes.md:2=mine", "guide.md:1=mine")
		out = h.run("--json", "skill", "resolve", "kit", "--editor")
		equal(t, "exit when only whole files are left", out.exit, 6)
		equal(t, "message when only whole files are left", h.one(out.stdout, "error")["message"],
			"every file left to resolve in the merge of kit conflicts as a whole file, so there is no text to edit")
		equal(t, "what the session left", strings.Join(editorDirs(t, h), " "), "")
	})
	t.Run("an editor that fails", func(t *testing.T) {
		t.Parallel()
		h, _, _ := resolveHarness(t)
		merge := h.ref(lineage.MergeRef("kit"))
		stub := editorStub(t, "failing-editor", `printf 'half done\n' > "$1"; exit 3`+"\n")
		h.env["EDITOR"] = stub

		out := h.run("--json", "skill", "resolve", "kit", "--editor")
		equal(t, "exit", out.exit, 6)
		e := h.one(out.stdout, "error")
		equal(t, "message", e["message"], "the editor "+stub+" exited with status 3, so nothing was written")
		dirs := editorDirs(t, h)
		if len(dirs) != 1 {
			t.Fatalf("the session left %v, want the one directory", dirs)
		}
		equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), keptWarning(dirs[0]))
		equal(t, "what the editor wrote, kept", fileBody(t, filepath.Join(dirs[0], "kit", "guide.md")), "half done\n")
		equal(t, "the owner of what is kept", ownerOf(t, dirs[0]), "kept")
		equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), merge)

		h.env["EDITOR"] = editorStub(t, "idle-editor", "exit 0\n")
		h.mustRun("skill", "resolve", "kit", "--editor")
		equal(t, "what the editor wrote, after a later session", fileBody(t, filepath.Join(dirs[0], "kit", "guide.md")), "half done\n")
		resolveKit(t, h, 0, append(kitSides, "kept.md:1=theirs")...)
		equal(t, "what is left once the merge completed", strings.Join(editorDirs(t, h), " "), "")
	})
	t.Run("an editor whose sh is killed by a signal", func(t *testing.T) {
		t.Parallel()
		h, _, _ := resolveHarness(t)
		h.env["EDITOR"] = editorStub(t, "killing-editor", "kill -9 $PPID\n")
		out := h.run("--json", "skill", "resolve", "kit", "--editor")
		equal(t, "exit", out.exit, 6)
		equal(t, "message", h.one(out.stdout, "error")["message"], "the editor "+h.env["EDITOR"]+" was stopped by SIGKILL, so nothing was written")
	})
}

// gitOnlyPath is a PATH holding git and nothing else, so that a test knows
// no code command of the machine's is on it.
func gitOnlyPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeShim(t, filepath.Join(dir, "git"), "#!/bin/sh\nexec "+realGit(t)+` "$@"`+"\n")
	return dir
}

// TestSkillResolveOpensTheEditorTheEnvironmentNames: the editor is
// GIT_EDITOR when it is set, EDITOR when it is not, and 'code --wait' when
// neither is and a code command is on PATH, as git would pick among them;
// with none of the three the run is exit code 1 before anything is read.
// The command is run through sh, so it may carry arguments of its own.
func TestSkillResolveOpensTheEditorTheEnvironmentNames(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	log := filepath.Join(t.TempDir(), "log")
	stub := func(name string) string {
		return editorStub(t, name, `printf '%s %s\n' `+name+` "$*" >> `+shellWord(log)+"\n")
	}
	code := filepath.Dir(editorStub(t, "code", `printf 'code %s\n' "$*" >> `+shellWord(log)+"\n"))
	path := gitOnlyPath(t)
	h.env["PATH"] = code + string(os.PathListSeparator) + path
	h.env["GIT_EDITOR"], h.env["EDITOR"] = stub("git-editor")+" --flag", stub("editor")
	last := func() string {
		t.Helper()
		lines := strings.Split(strings.TrimSpace(fileBody(t, log)), "\n")
		return strings.Fields(lines[len(lines)-1])[0] + " " + strings.Fields(lines[len(lines)-1])[1]
	}

	h.mustRun("skill", "resolve", "kit", "--editor", "guide.md")
	equal(t, "with GIT_EDITOR set", last(), "git-editor --flag")
	delete(h.env, "GIT_EDITOR")
	h.mustRun("skill", "resolve", "kit", "--editor", "guide.md")
	equal(t, "with EDITOR set", strings.Fields(last())[0], "editor")
	delete(h.env, "EDITOR")
	h.mustRun("skill", "resolve", "kit", "--editor", "guide.md")
	equal(t, "with neither", last(), "code --wait")

	h.env["PATH"] = path
	out := h.run("--json", "skill", "resolve", "kit", "--editor")
	equal(t, "exit with no editor", out.exit, 1)
	e := h.one(out.stdout, "error")
	equal(t, "message", e["message"], "no editor to open the files in: GIT_EDITOR and EDITOR are not set and there is no code command on PATH")
	equal(t, "hint", e["hint"], "set EDITOR to the editor to use, as in EDITOR=vim, or resolve with --hunk <file>:<index>=mine|theirs|both")
}

// waitingEditor is an editor that says it is open on stderr, then waits
// until the file go exists before it resolves every file it was given, as
// resolvingEditor does. The wait is a loop of short sleeps that gives up
// after a minute, so that a test that fails before it writes go leaves no
// editor behind for long; the test's own deadline on the run is shorter.
func waitingEditor(t *testing.T) (stub, goFile string) {
	t.Helper()
	return waitThen(t, `for f in "$@"; do printf 'resolved in the editor\n' > "$f"; done`+"\n")
}

// waitThen is waitingEditor with then, shell, run once go exists instead
// of resolving the files.
func waitThen(t *testing.T, then string) (stub, goFile string) {
	t.Helper()
	sleeper, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	goFile = filepath.Join(t.TempDir(), "go")
	stub = editorStub(t, "waiting-editor", `echo "editor open" >&2
i=0
while [ ! -e `+shellWord(goFile)+` ] && [ $i -lt 1200 ]; do `+sleeper+` 0.05; i=$((i+1)); done
`+then)
	return stub, goFile
}

// TestSkillResolveLeavesTheLockFreeWhileTheEditorIsOpen: nothing holds the
// lock while the editor is open, so every other command goes on meanwhile:
// a mutation and a placement of the skill itself both succeed while a stub
// editor waits. Once it exits, what it resolved is written.
func TestSkillResolveLeavesTheLockFreeWhileTheEditorIsOpen(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	stub, goFile := waitingEditor(t)
	h.env["EDITOR"] = stub
	p := h.start("editor open", "--json", "skill", "resolve", "kit", "--editor")
	p.await()

	if out := h.run("config", "set", "label", "while the editor is open"); out.exit != 0 {
		t.Errorf("a mutation while the editor is open: exit %d\n%s", out.exit, out.stderr)
	}
	if out := h.run("skill", "place", "kit", "--to", "claude-code"); out.exit != 0 {
		t.Errorf("a placement while the editor is open: exit %d\n%s", out.exit, out.stderr)
	}
	writeFile(t, goFile, "")
	out := p.wait()
	equal(t, "exit", out.exit, 0)
	equal(t, "summary", h.one(out.stdout, "result")["summary"], "resolved 2 files in the merge of kit, 3 files left to resolve")
	equal(t, "what the session left", strings.Join(editorDirs(t, h), " "), "")
}

// TestSkillResolveLeavesACtrlCToTheEditor: a terminal's Ctrl-C and Ctrl-\
// reach its whole foreground process group, the editor, the sh that runs
// it and agentx alike. The editor answers them, this one by going on, and
// neither the sh nor agentx dies of them: they wait for the editor, and
// what it resolved completes the merge once it exits.
func TestSkillResolveLeavesACtrlCToTheEditor(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	resolveKit(t, h, 0, kitWholeFiles...)
	candidate := h.ref(lineage.CandidateRef("kit"))
	sleeper, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(t.TempDir(), "open")
	// The child process has the test's own environment too, so GIT_EDITOR
	// names the editor, and PATH holds git alone, which also spares the
	// child the fixture the suite builds with go when it can. The editor
	// waits for both signals, each sleep in the background, where a shell
	// starts a command with both of them ignored.
	h.env["PATH"] = gitOnlyPath(t)
	h.env["GIT_EDITOR"] = editorStub(t, "answering-editor", `trap 'int=1' INT
trap 'quit=1' QUIT
echo open > `+shellWord(ready)+`
i=0
while { [ -z "$int" ] || [ -z "$quit" ]; } && [ $i -lt 1200 ]; do `+sleeper+` 0.05 & wait $!; i=$((i+1)); done
[ -n "$int" ] && [ -n "$quit" ] || exit 1
for f in "$@"; do printf 'resolved in the editor\n' > "$f"; done
`)
	code, stderr := signalled(t, h, stopRun{ready: ready, sigs: []syscall.Signal{syscall.SIGINT, syscall.SIGQUIT}, group: true,
		args: []string{"skill", "resolve", "kit", "--editor", "--color", "off"}})
	if code != 0 {
		t.Fatalf("exit %d, want 0:\n%s", code, stderr)
	}
	equal(t, "the import branch", h.ref(lineage.ManagedRef("kit")), candidate)
	equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), "")
	equal(t, "notes.md", fileBody(t, filepath.Join(h.library, "kit", "notes.md")), "resolved in the editor\n")
	equal(t, "what the session left", strings.Join(editorDirs(t, h), " "), "")
}

// TestSkillResolveStoppedWhileTheEditorIsOpenKeepsTheFiles: a SIGTERM still
// stops a run whose editor is open, exit code 9, and what the editor was
// given stays in its directory, kept, which a warning names; the merge is
// left as it was.
func TestSkillResolveStoppedWhileTheEditorIsOpenKeepsTheFiles(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	merge := h.ref(lineage.MergeRef("kit"))
	stub, _ := waitThen(t, "exit 0\n")
	ready := filepath.Join(t.TempDir(), "open")
	h.env["PATH"] = gitOnlyPath(t) // as TestSkillResolveLeavesACtrlCToTheEditor says
	h.env["GIT_EDITOR"] = editorStub(t, "reporting-editor", "echo open > "+shellWord(ready)+"\nexec "+shellWord(stub)+` "$@"`+"\n")

	code, stderr := signalled(t, h, stopRun{ready: ready, sigs: []syscall.Signal{syscall.SIGTERM}, group: true,
		args: []string{"skill", "resolve", "kit", "--editor", "--color", "off"}})
	equal(t, "exit", code, exitInterrupted.exit)
	contains(t, "stderr", stderr, "error: interrupted")
	dirs := editorDirs(t, h)
	if len(dirs) != 1 {
		t.Fatalf("the run left %v, want the one directory:\n%s", dirs, stderr)
	}
	contains(t, "the warning", stderr, "warning: "+keptWarning(dirs[0]))
	equal(t, "the owner of what is kept", ownerOf(t, dirs[0]), "kept")
	equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), merge)
	equal(t, "journals", journalCount(t, h), 0)
}

// TestSkillResolveFindsTheMergeRewrittenWhileTheEditorIsOpen: a merge
// another run resolved part of while the editor was open is not
// overwritten. The editor's run writes nothing, exits 6, and keeps what
// the editor saved in its directory, which a warning names; the merge ref
// holds what the other run wrote. A new session, as the hint suggests,
// leaves what was kept alone.
func TestSkillResolveFindsTheMergeRewrittenWhileTheEditorIsOpen(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	stub, goFile := waitingEditor(t)
	h.env["EDITOR"] = stub
	p := h.start("editor open", "--json", "skill", "resolve", "kit", "--editor")
	p.await()
	resolveKit(t, h, 0, "logo.bin:1=mine")
	meanwhile := h.ref(lineage.MergeRef("kit"))

	writeFile(t, goFile, "")
	out := p.wait()
	equal(t, "exit", out.exit, 6)
	e := h.one(out.stdout, "error")
	equal(t, "message", e["message"], "the merge of kit changed while it was being resolved, so nothing was written")
	equal(t, "hint", e["hint"], "run 'agentx skill resolve kit' to see the merge as it is now")
	dirs := editorDirs(t, h)
	if len(dirs) != 1 {
		t.Fatalf("the session left %v, want the one directory", dirs)
	}
	var logged []string
	for _, e := range h.eventsOfType(out.stderr, "log") {
		logged = append(logged, e["level"].(string)+": "+e["message"].(string))
	}
	equal(t, "what the run logged", strings.Join(logged, "\n"), "info: editor open\nwarn: "+keptWarning(dirs[0]))
	equal(t, "what the editor saved", fileBody(t, filepath.Join(dirs[0], "kit", "notes.md")), "resolved in the editor\n")
	equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), meanwhile)
	if got := h.accountGit("cat-file", "blob", meanwhile+":kit-dir/notes.md"); strings.Contains(got, "resolved in the editor") {
		t.Errorf("the editor's notes.md reached the merge: %q", got)
	}

	h.env["EDITOR"] = editorStub(t, "idle-editor", "exit 0\n")
	h.mustRun("skill", "resolve", "kit", "--editor")
	equal(t, "what the editor saved, after a new session", fileBody(t, filepath.Join(dirs[0], "kit", "notes.md")), "resolved in the editor\n")
}

// TestSkillResolveReportsNothingOfAMergeGoneWhileTheEditorIsOpen: a
// session that resolved nothing reads the merge ref again before it reports
// the merge. A merge given up while the editor was open, whose files went
// with it, is exit code 6, and so is one another run resolved part of: no
// conflict event reports a merge that is no longer the one pending.
func TestSkillResolveReportsNothingOfAMergeGoneWhileTheEditorIsOpen(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, message string
		meanwhile     func(t *testing.T, h *harness)
	}{
		{name: "given up", message: "kit has no merge pending: it was given up or completed while the editor was open",
			meanwhile: func(_ *testing.T, h *harness) { h.mustRun("skill", "resolve", "kit", "--abort") }},
		{name: "resolved in part", message: "the merge of kit changed while the editor was open, so nothing was written",
			meanwhile: func(t *testing.T, h *harness) { resolveKit(t, h, 0, "logo.bin:1=mine") }},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, _, _ := resolveHarness(t)
			stub, goFile := waitThen(t, "exit 0\n")
			h.env["EDITOR"] = stub
			p := h.start("editor open", "--json", "skill", "resolve", "kit", "--editor")
			p.await()
			c.meanwhile(t, h)
			writeFile(t, goFile, "")
			out := p.wait()
			equal(t, "exit", out.exit, 6)
			equal(t, "message", h.one(out.stdout, "error")["message"], c.message)
			if got := h.eventsOfType(out.stdout, "conflict"); len(got) != 0 {
				t.Errorf("the run reported %v", got)
			}
			equal(t, "what the session left", strings.Join(editorDirs(t, h), " "), "")
		})
	}
}

// TestSkillResolveInAnEditorCompletesIntoAMergeDoneAgain: a session that
// resolves the last file completes the merge, which merges the library
// directory again when it was edited meanwhile. When a file the editor
// did not resolve conflicts anew, the run exits 4 with what the editor
// resolved written into the merge, and the directory goes; when the file
// the editor resolved conflicts anew, what was typed for it stays in the
// directory, which a warning names.
func TestSkillResolveInAnEditorCompletesIntoAMergeDoneAgain(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, edit, body, anew string
	}{
		{name: "another file conflicts anew", edit: "guide.md", body: "guide, mine again\n", anew: "guide.md:1"},
		{name: "the file edited conflicts anew", edit: "notes.md", body: "ONE\ntwo\nthree\nfour\nFIVE again\n", anew: "notes.md:2"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, _, _ := resolveHarness(t)
			resolveKit(t, h, 0, append(slices.Clone(kitWholeFiles), "guide.md:1=mine")...)
			editLibrary(t, h, "kit", c.edit, c.body)
			stub, _, _ := resolvingEditor(t)
			h.env["EDITOR"] = stub

			out := h.run("--json", "skill", "resolve", "kit", "--editor")
			equal(t, "exit", out.exit, 4)
			equal(t, "what conflicts anew", conflictFiles(h.one(out.stdout, "conflict")), c.anew)
			dirs := editorDirs(t, h)
			if c.edit != "notes.md" {
				equal(t, "what the session left", strings.Join(dirs, " "), "")
				equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), "")
				equal(t, "notes.md in the merge", h.accountGit("cat-file", "blob", lineage.MergeRef("kit")+":kit-dir/notes.md"), "resolved in the editor")
				contains(t, "the message", pendingMessage(h, "kit"), "resolved \"notes.md\"\n")
				return
			}
			if len(dirs) != 1 {
				t.Fatalf("the session left %v, want the one directory", dirs)
			}
			equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"),
				"notes.md conflicts anew now that the merge was merged again, so what you typed for it is kept in "+dirs[0]+" until the merge of kit completes or is given up")
			equal(t, "what the editor saved", fileBody(t, filepath.Join(dirs[0], "kit", "notes.md")), "resolved in the editor\n")
			equal(t, "the owner of what is kept", ownerOf(t, dirs[0]), "kept")
		})
	}
}

// TestSkillResolvePrunesWhatAnEditorSessionLeftBehind: a directory an
// editor session of the skill left for this home, whose run is gone
// without keeping it, killed say, is removed when the next session starts;
// one whose run is still going, one its run kept, and one of another skill
// stay. Completing the merge removes the kept one too, and leaves the one
// whose run is still going.
func TestSkillResolvePrunesWhatAnEditorSessionLeftBehind(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	gone := exec.Command("true")
	if err := gone.Run(); err != nil {
		t.Fatal(err)
	}
	pid := strconv.Itoa(gone.Process.Pid)
	stale := editorDir(t, h, "kit", pid, h.agentx)
	live := editorDir(t, h, "kit", ownPid, h.agentx)
	kept := editorDir(t, h, "kit", "kept", h.agentx)
	other := editorDir(t, h, "other", pid, h.agentx)
	h.env["EDITOR"] = editorStub(t, "idle-editor", "exit 0\n")

	h.mustRun("skill", "resolve", "kit", "--editor")
	nothingAt(t, "the stale session", stale)
	equal(t, "what is left", strings.Join(editorDirs(t, h), " "), strings.Join(sortedStrings(live, kept, other), " "))

	resolveKit(t, h, 0, append(kitSides, "kept.md:1=theirs")...)
	equal(t, "what is left once the merge completed", strings.Join(editorDirs(t, h), " "), strings.Join(sortedStrings(live, other), " "))
}

// sortedStrings is ss sorted.
func sortedStrings(ss ...string) []string {
	sort.Strings(ss)
	return ss
}

// resolveChildEnv marks the process the crash tests of skill resolve
// start, which runs skill resolve with the arguments it holds, separated by
// |, against the parent's temporary home, and is killed in the middle of
// it.
const resolveChildEnv = "AGENTX_TEST_RESOLVE_CHILD"

// TestResolveChildProcess is not a test: it is the body of that process.
// It does nothing when the variable that marks it is not set.
func TestResolveChildProcess(t *testing.T) {
	args := os.Getenv(resolveChildEnv)
	if args == "" {
		t.Skip("not the resolve child process")
	}
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	os.Exit(Run(context.Background(), append([]string{"skill", "resolve"}, strings.Split(args, "|")...), env, strings.NewReader(""), os.Stdout, os.Stderr))
}

// TestSkillResolveRecoversAtEveryBoundary kills skill resolve with SIGKILL
// once its journal is on disk, and leaves the machine as a process killed
// after each later step would, then kills it for real right after its last
// live write. The next command recovers each one, skill resolve itself
// included, which then finds the completion it finished:
//
//   - a resolve that rewrites the merge, one step, leaves the merge ref at
//     the rewritten commit;
//   - giving the merge up, one step, leaves no merge ref;
//   - a completion, the import branch moved, the library directory
//     retained and replaced, the unedited copy retained and refreshed,
//     then the candidate and the merge ref deleted, leaves the merge
//     complete: the branch at the update, the library and the copy holding
//     the merged version, the edited copy kept, no candidate, no merge ref
//     and nothing staged or retained left behind.
func TestSkillResolveRecoversAtEveryBoundary(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		args  string
		kinds string // the journal's steps
		check func(t *testing.T, h *harness, journal []journalStep)
	}{
		{name: "a rewrite", args: "kit|--hunk|logo.bin:1=mine", kinds: "ref", check: func(t *testing.T, h *harness, journal []journalStep) {
			equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), journal[0].New)
			equal(t, "the subject", h.accountGit("log", "-1", "--format=%s", lineage.MergeRef("kit")), "pending merge of kit: 4 files unresolved")
		}},
		{name: "giving the merge up", args: "kit|--abort", kinds: "ref", check: func(t *testing.T, h *harness, _ []journalStep) {
			equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), "")
		}},
		{name: "a completion", args: "kit|--hunk|kept.md:1=theirs", kinds: "ref, remove, publish, remove, publish, ref, ref"},
	} {
		steps := len(strings.Split(c.kinds, ", "))
		for stop := 0; stop <= steps+1; stop++ {
			name := fmt.Sprintf("%s after %d steps", c.name, stop)
			if stop > steps {
				name = c.name + " killed after its last live write"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				h, _, _ := resolveHarness(t)
				claude := filepath.Join(h.home, ".claude", "skills", "kit")
				cursor := filepath.Join(h.home, ".cursor", "skills", "kit")
				editCopy(t, cursor)
				edited := libraryTree(t, cursor)
				if c.check == nil {
					resolveKit(t, h, 0, kitSides...)
				}
				candidate := h.ref(lineage.CandidateRef("kit"))
				var journal []journalStep
				if stop > steps {
					script := lastWriteScript
					if c.check != nil {
						script = firstWriteScript
					}
					out := killedChild(t, h, "TestResolveChildProcess", resolveChildEnv, c.args, script)
					equal(t, "journals the killed run left", journalCount(t, h), 1)
					journal = readJournal(t, h)
					if c.check == nil && h.ref(lineage.MergeRef("kit")) != "" {
						t.Fatalf("the completion was not killed after it deleted the merge ref:\n%s", out)
					}
				} else {
					killedChild(t, h, "TestResolveChildProcess", resolveChildEnv, c.args, killedUpdateScript)
					journal = readJournal(t, h)
					var kinds []string
					for _, s := range journal {
						kinds = append(kinds, s.Kind)
					}
					equal(t, "the journal's steps", strings.Join(kinds, ", "), c.kinds)
					applyUpdateSteps(t, h, journal, stop)
				}

				if c.check == nil && stop == 1 {
					// skill resolve itself finishes the completion first, and
					// then finds no merge left to resolve.
					got := h.run("--json", "skill", "resolve", "kit")
					equal(t, "exit of the resolve after the killed run", got.exit, 6)
					equal(t, "its message", h.one(got.stdout, "error")["message"], "kit has no merge pending")
				} else if got := h.run("config", "set", "label", "recovered"); got.exit != 0 {
					t.Fatalf("the command after the killed run: exit %d\n%s", got.exit, got.stderr)
				}
				equal(t, "journals after recovery", journalCount(t, h), 0)
				if c.check != nil {
					c.check(t, h, journal)
					return
				}
				want := kitTree(t, h, kitResolved)
				equal(t, "the import branch", h.ref(lineage.ManagedRef("kit")), candidate)
				equal(t, "the candidate ref", h.ref(lineage.CandidateRef("kit")), "")
				equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), "")
				sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "kit")), want)
				sameTree(t, "claude's copy", libraryTree(t, claude), want)
				sameTree(t, "cursor's copy", libraryTree(t, cursor), edited)
				for _, dir := range []string{h.library, filepath.Dir(claude), filepath.Dir(cursor)} {
					equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
				}
				equal(t, "state", h.listed("kit")["state"], stateModified)
			})
		}
	}
}

// pendingWith points kit's merge ref at a pending merge commit written as
// the update writes one, nothing of it resolved: the merge of the base and
// mine the merge names now with theirs.
func pendingWith(t *testing.T, h *harness, theirs string) {
	t.Helper()
	held := h.ref(lineage.MergeRef("kit"))
	m := lineage.Merge{Base: trailer(t, h, held, lineage.TrailerMergeBase), Mine: trailer(t, h, held, lineage.TrailerMergeMine), Theirs: theirs}
	r := gitx.New(h.env, false, func(string, ...any) {})
	out, _, err := r.IsolatedStatus(context.Background(), gitx.AccountRepoPath(h.agentx), 1,
		"-c", "merge.directoryRenames=false", "merge-tree", "--write-tree", "--no-messages", "--merge-base="+m.Base, m.Mine, m.Theirs)
	if err != nil {
		t.Fatal(err)
	}
	tree, _, _ := strings.Cut(out, "\n")
	commit := h.accountGitInput(m.Message("kit", 5, nil), "commit-tree", tree, "-p", m.Mine, "-p", m.Theirs)
	h.accountGit("update-ref", lineage.MergeRef("kit"), commit, held)
}

// TestSkillResolveRefusesAMergeItCannotRead: a pending merge commit whose
// message agentx did not write, a merge whose versions no longer conflict
// when merged again, and a skill that is a fork are exit code 6 whatever
// the run asks, with nothing written; a merge that cannot be read can
// still be given up.
func TestSkillResolveRefusesAMergeItCannotRead(t *testing.T) {
	t.Parallel()
	abandon := "run 'agentx skill resolve kit --abort' to give the merge up; the library directory stays as it is"
	for _, c := range []struct {
		name, message, hint string
		make                func(t *testing.T, h *harness, updated jsonEvent)
		fork                bool
	}{
		{name: "a message agentx did not write", message: "the pending merge refs/agentx/merge/kit names no versions agentx can read", hint: abandon,
			make: func(_ *testing.T, h *harness, updated jsonEvent) {
				merge := h.ref(lineage.MergeRef("kit"))
				h.accountGit("update-ref", lineage.MergeRef("kit"), h.accountGit("commit-tree", merge+"^{tree}",
					"-p", updated["mine"].(string), "-p", updated["theirs"].(string), "-m", "a merge of my own"))
			}},
		{name: "versions that no longer conflict", message: "the versions the pending merge of kit names no longer conflict when merged again",
			hint: abandon + ", then run 'agentx skill update kit' again",
			make: func(t *testing.T, h *harness, updated jsonEvent) {
				// An update that holds exactly what the library directory holds.
				pendingWith(t, h, h.accountGit("commit-tree", updated["mine"].(string)+"^{tree}",
					"-m", h.accountGit("log", "-1", "--format=%B", updated["theirs"].(string))))
			}},
		{name: "a fork", message: "kit is a fork on this machine", hint: "a fork's versions are its own history; this command works on a managed skill", fork: true,
			make: func(_ *testing.T, h *harness, _ jsonEvent) {
				h.accountGit("update-ref", lineage.ForkRef("kit"), h.ref(lineage.ManagedRef("kit")))
				h.accountGit("update-ref", "-d", lineage.ManagedRef("kit"))
			}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, _, updated := resolveHarness(t)
			c.make(t, h, updated)
			merge, placed := h.ref(lineage.MergeRef("kit")), onDisk(t, h.home)
			for _, args := range [][]string{{}, {"--hunk", "logo.bin:1=mine"}} {
				what := strings.Join(args, " ")
				out := h.run(append([]string{"--json", "skill", "resolve", "kit"}, args...)...)
				equal(t, what+": exit", out.exit, 6)
				e := h.one(out.stdout, "error")
				equal(t, what+": message", e["message"], c.message)
				equal(t, what+": hint", e["hint"], c.hint)
				equal(t, what+": the merge ref", h.ref(lineage.MergeRef("kit")), merge)
			}
			equal(t, "the library and its copies", onDisk(t, h.home), placed)
			equal(t, "journals", journalCount(t, h), 0)
			if !c.fork {
				h.mustRun("skill", "resolve", "kit", "--abort")
				equal(t, "the merge ref once given up", h.ref(lineage.MergeRef("kit")), "")
			}
		})
	}
}

// TestSkillResolveWarnsOfAnUpdateThatNamesTheSkillOtherwise: a completed
// merge whose update names the skill otherwise in its SKILL.md keeps the
// library name, with the warning an update gives.
func TestSkillResolveWarnsOfAnUpdateThatNamesTheSkillOtherwise(t *testing.T) {
	t.Parallel()
	h, _, updated := resolveHarness(t)
	theirs := versionOf(t, h, updated["theirs"].(string), map[string]string{"SKILL.md": skill("kit-renamed", "A skill of every conflict")})
	pendingWith(t, h, theirs)
	resolveKit(t, h, 0, kitSides...)

	out := resolveKit(t, h, 0, "kept.md:1=theirs")
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), `kit: the update names the skill "kit-renamed"; updating it keeps the name kit`)
	equal(t, "the import branch", h.ref(lineage.ManagedRef("kit")), theirs)
	equal(t, "the library's SKILL.md", fileBody(t, filepath.Join(h.library, "kit", "SKILL.md")), skill("kit-renamed", "A skill of every conflict"))
	equal(t, "the name listed", h.listed("kit")["name"], "kit")
}

// TestSkillResolveRefusesToCompleteWhatItCannotReplace: completing the
// merge replaces the library directory, so one it cannot replace as it is
// is refused with exit code 6 and nothing written: a directory that changed
// after the run read it, and before the lock, a library entry that is a
// symlink, one that holds a repository of its own, which git cannot
// record, and one that is gone. So is a merge that resolves to a directory
// with no SKILL.md, which would take the skill out of every client. The
// merge stays pending as it was, the library, every copy, the import
// branch and the candidate are left as they were, and the run can be made
// again once that is put right, or the merge given up.
func TestSkillResolveRefusesToCompleteWhatItCannotReplace(t *testing.T) {
	t.Parallel()
	t.Run("a directory edited before the lock", func(t *testing.T) {
		t.Parallel()
		h, _, _ := resolveHarness(t)
		resolveKit(t, h, 0, kitSides...)
		merge, tip := h.ref(lineage.MergeRef("kit")), h.ref(lineage.ManagedRef("kit"))
		usage := filepath.Join(h.library, "kit", "usage.md")
		stubGit(t, h, `#!/bin/sh
case " $* " in
*" rev-parse "*"^{tree} "*) printf 'an edit made meanwhile\n' > `+shellWord(usage)+` || exit 1 ;;
esac
exec `+realGit(t)+` "$@"
`)
		out := resolveKit(t, h, 6, "kept.md:1=theirs")
		e := h.one(out.stdout, "error")
		equal(t, "message", e["message"], "kit changed while its merge was being completed, so nothing was changed")
		equal(t, "hint", e["hint"], "run the command again to complete the merge with the skill as it is now")
		equal(t, "the edit", fileBody(t, usage), "an edit made meanwhile\n")
		equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), merge)
		equal(t, "the import branch", h.ref(lineage.ManagedRef("kit")), tip)
		equal(t, "journals", journalCount(t, h), 0)
		equal(t, "what is left beside the library", strings.Join(hiddenEntries(t, h.library), " "), "")

		h.env["PATH"] = gitOnlyPath(t) // the git that edits the directory is gone
		again := resolveKit(t, h, 0, "kept.md:1=theirs")
		contains(t, "summary once run again", h.one(again.stdout, "result")["summary"].(string), "resolved kit, merged the edits made to it meanwhile and updated it from ")
		want := kitTree(t, h, kitResolved)
		want["usage.md"] = "an edit made meanwhile\n"
		sameTree(t, "the library directory once run again", libraryTree(t, filepath.Join(h.library, "kit")), want)
	})
	t.Run("a library entry that is a symlink", func(t *testing.T) {
		t.Parallel()
		h, _, _ := resolveHarness(t)
		resolveKit(t, h, 0, kitSides...)
		merge := h.ref(lineage.MergeRef("kit"))
		lib := filepath.Join(h.library, "kit")
		moved := filepath.Join(t.TempDir(), "kit")
		if err := os.Rename(lib, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(moved, lib); err != nil {
			t.Fatal(err)
		}
		out := resolveKit(t, h, 6, "kept.md:1=theirs")
		e := h.one(out.stdout, "error")
		equal(t, "message", e["message"], quotedPath(lib)+" is a symlink to "+quotedPath(moved)+
			"; completing the merge replaces the library directory and would drop the link without touching the files it leads to")
		equal(t, "hint", e["hint"], "replace the link with the directory it points to, then run the command again")
		equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), merge)
		if target, err := os.Readlink(lib); err != nil || target != moved {
			t.Errorf("the library entry is %q, %v", target, err)
		}

		remove(t, lib)
		if err := os.Rename(moved, lib); err != nil {
			t.Fatal(err)
		}
		resolveKit(t, h, 0, "kept.md:1=theirs")
		sameTree(t, "the library directory once run again", libraryTree(t, lib), kitTree(t, h, kitResolved))
	})
	// refused completes kit's merge, with every file but kept.md resolved
	// already, in a run that is refused, and checks that it wrote nothing.
	refused := func(t *testing.T, h *harness) jsonEvent {
		t.Helper()
		refs := h.accountGit("for-each-ref", "--format=%(refname) %(objectname)")
		placed := onDisk(t, h.home)
		out := resolveKit(t, h, 6, "kept.md:1=theirs")
		equal(t, "the refs", h.accountGit("for-each-ref", "--format=%(refname) %(objectname)"), refs)
		equal(t, "the library and its copies", onDisk(t, h.home), placed)
		equal(t, "journals", journalCount(t, h), 0)
		equal(t, "what is left beside the library", strings.Join(hiddenEntries(t, h.library), " "), "")
		return h.one(out.stdout, "error")
	}
	t.Run("a repository inside the library directory", func(t *testing.T) {
		t.Parallel()
		h, _, _ := resolveHarness(t)
		resolveKit(t, h, 0, kitSides...)
		nested := filepath.Join(h.library, "kit", "sub", ".git")
		writeFile(t, mkdirs(t, filepath.Dir(nested), ".git"), "gitdir: elsewhere\n")
		e := refused(t, h)
		equal(t, "message", e["message"], "kit holds "+quotedPath(nested)+", which git cannot record")
		equal(t, "hint", e["hint"], "completing the merge would discard it with no record of it anywhere; move it out of the skill, then run the command again")

		remove(t, filepath.Dir(nested))
		resolveKit(t, h, 0, "kept.md:1=theirs")
		sameTree(t, "the library directory once run again", libraryTree(t, filepath.Join(h.library, "kit")), kitTree(t, h, kitResolved))
	})
	t.Run("a merge that resolves to no SKILL.md", func(t *testing.T) {
		t.Parallel()
		h, _, updated := resolveHarness(t)
		pendingWith(t, h, versionOf(t, h, updated["theirs"].(string), map[string]string{"SKILL.md": ""}))
		resolveKit(t, h, 0, kitSides...)
		e := refused(t, h)
		equal(t, "message", e["message"], "the merge of kit resolves to a directory with no SKILL.md, which no client would read as a skill, so nothing was changed")
		equal(t, "hint", e["hint"], "resolve SKILL.md to the side that keeps it, or run 'agentx skill resolve kit --abort' to give the merge up")
	})
	t.Run("a library directory that is gone", func(t *testing.T) {
		t.Parallel()
		h, _, _ := resolveHarness(t)
		resolveKit(t, h, 0, kitSides...)
		remove(t, filepath.Join(h.library, "kit"))
		e := refused(t, h)
		equal(t, "message", e["message"], "kit is managed in the account repo but the library holds no skill directory for it, so its merge cannot be completed")
		if hint := e["hint"].(string); !strings.HasPrefix(hint, "run 'agentx skill resolve kit --abort' to give the merge up first; then ") {
			t.Errorf("hint = %q", hint)
		}
		h.mustRun("skill", "resolve", "kit", "--abort")
		equal(t, "the merge ref once given up", h.ref(lineage.MergeRef("kit")), "")
	})
}

// TestSkillResolveRecoveryKeepsTheMergeWhenTheLibraryChanged kills a
// completion once its journal is on disk, and the library directory is
// edited before the next command. That command's recovery moves the import
// branch, finds the directory holding something the completion did not
// capture, and refuses: the directory keeps the edit, and the candidate and
// the merge ref, read back with plain git, are still there, since recovery
// deletes them after the paths as the completion does, so nothing of the
// merge is lost. Restoring the directory lets the next command finish it.
func TestSkillResolveRecoveryKeepsTheMergeWhenTheLibraryChanged(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	resolveKit(t, h, 0, kitSides...)
	merge, candidate := h.ref(lineage.MergeRef("kit")), h.ref(lineage.CandidateRef("kit"))
	killedChild(t, h, "TestResolveChildProcess", resolveChildEnv, "kit|--hunk|kept.md:1=theirs", killedUpdateScript)
	equal(t, "journals the killed completion left", journalCount(t, h), 1)
	usage := filepath.Join(h.library, "kit", "usage.md")
	writeFile(t, usage, "an edit made after the completion stopped\n")

	out := h.run("--json", "config", "set", "label", "recovered")
	equal(t, "exit", out.exit, 6)
	contains(t, "message", h.one(out.stdout, "error")["message"].(string), "recovery required")
	equal(t, "the import branch, moved before the paths", h.ref(lineage.ManagedRef("kit")), candidate)
	equal(t, "the candidate ref", h.ref(lineage.CandidateRef("kit")), candidate)
	equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), merge)
	equal(t, "the edit", fileBody(t, usage), "an edit made after the completion stopped\n")

	writeFile(t, usage, "usage\n")
	h.mustRun("config", "set", "label", "recovered")
	equal(t, "journals after recovery", journalCount(t, h), 0)
	equal(t, "the candidate ref after recovery", h.ref(lineage.CandidateRef("kit")), "")
	equal(t, "the merge ref after recovery", h.ref(lineage.MergeRef("kit")), "")
	sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "kit")), kitTree(t, h, kitResolved))
}
