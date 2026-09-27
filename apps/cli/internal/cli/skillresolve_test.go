package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

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

// TestSkillResolveReadsAPathThatHoldsAColonOrAnEqualsSign: the side of a
// --hunk is what follows its last =, and the index what lies between that
// and the last : before it, so a file whose name holds both is chosen as
// any other; and the path is cleaned, ./notes.md naming notes.md.
func TestSkillResolveReadsAPathThatHoldsAColonOrAnEqualsSign(t *testing.T) {
	t.Parallel()
	h := clashHarness(t, map[string]string{"a:b=c.md": "a\n"},
		func(s *sourceRepo) { s.write("skills/kit-dir/a:b=c.md", "a, upstream\n") },
		func(lib string) { writeFile(t, filepath.Join(lib, "a:b=c.md"), "a, mine\n") })
	equal(t, "the conflicts", conflictFiles(h.one(h.mustRun("--json", "skill", "resolve", "kit").stdout, "conflict")), "a:b=c.md:1,notes.md:1")
	resolveKit(t, h, 0, "a:b=c.md:1=theirs", "./notes.md:1=theirs")
	equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), "")
	equal(t, "a:b=c.md", fileBody(t, filepath.Join(h.library, "kit", "a:b=c.md")), "a, upstream\n")
	equal(t, "notes.md", fileBody(t, filepath.Join(h.library, "kit", "notes.md")), "uno\n")
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
// merge with both edits. An edit that makes files resolved already merge
// cleanly, one deleted as the update deletes it and one set back to the
// first version's, completes the merge with git's merge of them. A copy
// placed from the library directory once it was edited meanwhile is
// refreshed with the merge.
func TestSkillResolveMergesAnEditMadeMeanwhile(t *testing.T) {
	t.Parallel()
	t.Run("an edit to a file that does not conflict", func(t *testing.T) {
		t.Parallel()
		h, _, _ := resolveHarness(t)
		candidate := h.ref(lineage.CandidateRef("kit"))
		resolveKit(t, h, 0, kitSides...)
		editLibrary(t, h, "kit", "usage.md", "usage, edited meanwhile\n")
		claude := filepath.Join(h.home, ".claude", "skills", "kit")
		h.mustRun("skill", "remove", "kit", "--from", "claude-code")
		h.mustRun("skill", "place", "kit", "--to", "claude-code", "--copy")

		out := resolveKit(t, h, 0, "kept.md:1=theirs")
		contains(t, "summary", h.one(out.stdout, "result")["summary"].(string), "resolved kit, merged the edits made to it meanwhile and updated it from ")
		contains(t, "summary", h.one(out.stdout, "result")["summary"].(string), ", 2 copy placements refreshed")
		want := kitTree(t, h, kitResolved)
		want["usage.md"] = "usage, edited meanwhile\n"
		sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "kit")), want)
		sameTree(t, "claude's copy", libraryTree(t, claude), want)
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
	t.Run("an edit that makes a file resolved already merge cleanly", func(t *testing.T) {
		t.Parallel()
		h, _, _ := resolveHarness(t)
		candidate := h.ref(lineage.CandidateRef("kit"))
		resolveKit(t, h, 0, kitSides...)
		remove(t, filepath.Join(h.library, "kit", "gone.md"))
		editLibrary(t, h, "kit", "guide.md", kitBase["guide.md"])

		out := resolveKit(t, h, 0, "kept.md:1=theirs")
		contains(t, "summary", h.one(out.stdout, "result")["summary"].(string), "resolved kit, merged the edits made to it meanwhile and updated it from ")
		want := kitTree(t, h, kitResolved)
		delete(want, "gone.md")
		want["guide.md"] = kitTheirs["guide.md"]
		sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "kit")), want)
		equal(t, "the import branch", h.ref(lineage.ManagedRef("kit")), candidate)
		equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), "")
	})
}

// clashHarness is a machine with Claude Code and one skill, kit, whose
// first version holds base and notes.md, "one", and whose update and edits
// conflict in notes.md, one hunk, and wherever upstream and edit, given
// the source and the library directory, make a file meet a directory, or
// conflict otherwise. It returns once the update left the merge pending.
func clashHarness(t *testing.T, base map[string]string, upstream func(s *sourceRepo), edit func(lib string)) *harness {
	t.Helper()
	return pendingHarness(t, "kit", base, upstream, edit)
}

// pendingHarness is clashHarness for a skill called name, at
// skills/<name>-dir in its source. Editor sessions lay their files out in
// a temporary directory of the test's own.
func pendingHarness(t *testing.T, name string, base map[string]string, upstream func(s *sourceRepo), edit func(lib string)) *harness {
	t.Helper()
	h := newHarness(t)
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h.env["TMPDIR"] = tmp
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo(name, true)
	first := map[string]string{"notes.md": "one\n"}
	maps.Copy(first, base)
	s.skill("skills/"+name+"-dir", name, "A skill whose update conflicts with its edits", first)
	s.commit("first version")
	h.mustRun("skill", "add", s.url)
	s.write("skills/"+name+"-dir/notes.md", "uno\n")
	upstream(s)
	s.commit("second version")
	h.mustRun("skill", "check")
	editLibrary(t, h, name, "notes.md", "ONE\n")
	edit(filepath.Join(h.library, name))
	if out := h.run("skill", "update", name); out.exit != 4 {
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

// TestSkillResolveRefusesAFileInsideAPathResolvedToAFile: a file of the
// merge inside a path resolved to a file cannot be resolved to a version
// that puts it there, which would take the file resolved at that path away
// with no word. Chosen in the same run or after the path was resolved, the
// choice is refused with exit code 1 and nothing written, and the hint
// names the side that keeps a directory there; chosen with that side for
// the path, it completes the merge. A path resolved to a file after a file
// inside it was resolved takes that resolution away, and the file is left
// to resolve again.
func TestSkillResolveRefusesAFileInsideAPathResolvedToAFile(t *testing.T) {
	t.Parallel()
	pending := func(t *testing.T) *harness {
		return clashHarness(t, map[string]string{"x/a.md": "a\n"},
			func(s *sourceRepo) { s.write("skills/kit-dir/x/a.md", "a, upstream\n") },
			func(lib string) {
				remove(t, filepath.Join(lib, "x"))
				writeFile(t, filepath.Join(lib, "x"), "a file here\n")
			})
	}
	const (
		message = "x is resolved to mine, a file, so x/a.md cannot be resolved to theirs inside it"
		hint    = "resolve x to theirs, which keeps a directory there, first or in the same run, as in --hunk x:1=theirs --hunk x/a.md:1=theirs"
	)
	refused := func(t *testing.T, h *harness, hunks ...string) {
		t.Helper()
		merge, library := h.ref(lineage.MergeRef("kit")), onDisk(t, h.library)
		e := h.one(resolveKit(t, h, 1, hunks...).stdout, "error")
		equal(t, "message", e["message"], message)
		equal(t, "hint", e["hint"], hint)
		equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), merge)
		equal(t, "the library", onDisk(t, h.library), library)
		equal(t, "the file here", fileBody(t, filepath.Join(h.library, "kit", "x")), "a file here\n")
		equal(t, "journals", journalCount(t, h), 0)
	}
	t.Run("in the same run", func(t *testing.T) {
		t.Parallel()
		h := pending(t)
		refused(t, h, "x:1=mine", "x/a.md:1=theirs", "notes.md:1=theirs")
	})
	t.Run("after the path was resolved to a file", func(t *testing.T) {
		t.Parallel()
		h := pending(t)
		resolveKit(t, h, 0, "x:1=mine")
		refused(t, h, "x/a.md:1=theirs", "notes.md:1=theirs")
		equal(t, "what is left", conflictFiles(h.one(h.mustRun("--json", "skill", "resolve", "kit").stdout, "conflict")), "notes.md:1,x/a.md:whole")
	})
	t.Run("before the path was resolved to a file", func(t *testing.T) {
		t.Parallel()
		h := pending(t)
		resolveKit(t, h, 0, "x/a.md:1=theirs")
		out := resolveKit(t, h, 0, "x:1=mine")
		equal(t, "summary", h.one(out.stdout, "result")["summary"], "resolved x in the merge of kit, 2 files left to resolve")
		equal(t, "what is left", conflictFiles(h.one(out.stdout, "conflict")), "notes.md:1,x/a.md:whole")
		equal(t, "x in the merge", inMerge(h, "x"), "a file here")
		contains(t, "the message", pendingMessage(h, "kit"), "pending merge of kit: 2 files unresolved\n\nresolved \"x\"\n\n")
		refused(t, h, "x/a.md:1=theirs")
		resolveKit(t, h, 0, "x/a.md:1=mine", "notes.md:1=theirs")
		sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "kit")),
			map[string]string{"SKILL.md": fileBody(t, filepath.Join(h.library, "kit", "SKILL.md")), "notes.md": "uno\n", "x": "a file here\n"})
	})
	t.Run("with the side that keeps a directory there", func(t *testing.T) {
		t.Parallel()
		h := pending(t)
		resolveKit(t, h, 0, "x:1=theirs", "x/a.md:1=theirs", "notes.md:1=theirs")
		sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "kit")),
			map[string]string{"SKILL.md": fileBody(t, filepath.Join(h.library, "kit", "SKILL.md")), "notes.md": "uno\n", "x/a.md": "a, upstream\n"})
		equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), "")
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

// versionUnder writes a commit of the version commit holds with kit's
// directory named dir, in its tree and in its Agentx-Path trailer, as an
// import of the skill from another directory of its source would be, and
// returns it. Nothing points at it.
func versionUnder(h *harness, commit, dir string) string {
	h.t.Helper()
	root := h.accountGitInput("040000 tree "+h.accountGit("rev-parse", commit+":kit-dir")+"\t"+dir+"\n", "mktree")
	was := h.accountGit("log", "-1", "--format=%B", commit)
	message := strings.Replace(was, lineage.TrailerPath+": skills/kit-dir", lineage.TrailerPath+": skills/"+dir, 1)
	if message == was {
		h.t.Fatalf("%s names no skills/kit-dir:\n%s", commit, was)
	}
	return h.accountGitInput("", "commit-tree", root, "-m", message)
}

// TestSkillResolveRefusesAMergeItCannotMergeAgain: an import branch moved
// to the very update the merge merges leaves nothing to merge, one moved to
// a commit agentx cannot read names no version to merge on, and one moved
// to a version that holds the skill under another upstream directory than
// the update cannot be merged with it: each is exit code 6 whatever the run
// asks, and nothing is written; giving the merge up still works.
func TestSkillResolveRefusesAMergeItCannotMergeAgain(t *testing.T) {
	t.Parallel()
	abandon := "run 'agentx skill resolve kit --abort' to give the merge up; the library directory stays as it is"
	for _, c := range []struct {
		name, message, hint string
		to                  func(h *harness, updated jsonEvent) string
	}{
		{name: "moved to the update", message: "the import branch refs/heads/managed/kit moved to the update the merge of kit merges, so there is nothing left to merge", hint: abandon,
			to: func(h *harness, _ jsonEvent) string { return h.ref(lineage.CandidateRef("kit")) }},
		{name: "moved to a commit with no lineage", message: "the import branch refs/heads/managed/kit records no version agentx can read", hint: abandon,
			to: func(_ *harness, updated jsonEvent) string { return updated["mine"].(string) }},
		{name: "moved to a version under another directory",
			message: "the import branch refs/heads/managed/kit moved since the merge of kit was left pending, to a version that holds the skill under another directory than the update, so the merge cannot be resolved",
			hint:    "run 'agentx skill resolve kit --abort' to give it up, then 'agentx skill update kit' to merge again",
			to: func(h *harness, updated jsonEvent) string {
				return versionUnder(h, updated["base"].(string), "other-dir")
			}},
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
				equal(t, what+": hint", e["hint"], c.hint)
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
// is "kept", for the agentx home at home, and returns it. It holds
// notes.md as the session wrote it, which its owner file records.
func editorDir(t *testing.T, h *harness, name, pid, home string) string {
	t.Helper()
	dir, err := os.MkdirTemp(h.env["TMPDIR"], "agentx-resolve-")
	if err != nil {
		t.Fatal(err)
	}
	const written = "<<<<<<< mine\n"
	writeFile(t, filepath.Join(dir, editorOwner), pid+"\n"+home+"\n"+h.ref(lineage.MergeRef(name))+"\n"+editorRecord("notes.md", []byte(written))+"\n")
	writeFile(t, mkdirs(t, filepath.Join(dir, name), "notes.md"), written)
	return dir
}

// ownPid is the process id of the test binary, as an owner file names it:
// a run that is still going.
var ownPid = strconv.Itoa(os.Getpid())

// keptWarning is the warning of a session whose run kept the files the
// editor was given in dir.
func keptWarning(dir string) string {
	return "the files you edited are kept in " + dir + opensAgainKit("them")
}

// movedWarning is the warning of a session of every file left whose run
// kept the files the editor was given in dir, once another run moved the
// merge while its editor was open: a file that run resolved is not opened
// again by the next session of every file left.
func movedWarning(dir string) string {
	return "the files you edited are kept in " + dir + "; the next 'agentx skill resolve kit --editor' opens what you typed again for each file still left to resolve with the same conflict, but not for a file resolved while your editor was open, so copy what you need from it"
}

// opensAgainKit is how a warning that names a directory kept for what was
// typed in it, it or them, ends.
func opensAgainKit(what string) string {
	return ", and the next 'agentx skill resolve kit --editor' of the same conflict opens " + what + " again"
}

// opensAgainKitFile is opensAgainKit for what was typed in file, one the
// merge records as resolved, which only the session that names it opens.
func opensAgainKitFile(file, what string) string {
	return ", and the next 'agentx skill resolve kit --editor " + file + "' of the same conflict opens " + what + " again"
}

// kitText is what an editor session is given of each text file of kit's
// merge, as merge-file writes it.
var kitText = map[string]string{
	"guide.md": "<<<<<<< mine\nguide, mine\n||||||| base\nguide\n=======\nguide, upstream\n>>>>>>> theirs\n",
	"notes.md": "<<<<<<< mine\nONE\n||||||| base\none\n=======\nuno\n>>>>>>> theirs\ntwo\nthree\nfour\n<<<<<<< mine\nFIVE\n||||||| base\nfive\n=======\ncinco\n>>>>>>> theirs\n",
}

// typedDir makes a directory an editor session of kit could have left for
// this home, owned by the process pid, or kept by its run when pid is
// "kept": its owner file records each file typed names as merge-file wrote
// it for kit's merge, see kitText, and the file holds what typed gives it.
func typedDir(t *testing.T, h *harness, pid string, typed map[string]string) string {
	t.Helper()
	dir, err := os.MkdirTemp(h.env["TMPDIR"], "agentx-resolve-")
	if err != nil {
		t.Fatal(err)
	}
	owner := pid + "\n" + h.agentx + "\n" + h.ref(lineage.MergeRef("kit")) + "\n"
	for _, p := range slices.Sorted(maps.Keys(typed)) {
		owner += editorRecord(p, []byte(kitText[p])) + "\n"
		writeFile(t, mkdirs(t, filepath.Join(dir, "kit"), p), typed[p])
	}
	writeFile(t, filepath.Join(dir, editorOwner), owner)
	return dir
}

// notOpenedWarning is the warning a session gives as it starts of the
// directory in dir of an earlier one that holds something typed that it
// does not open, whether that one's run is gone or it was kept, and
// whether or not an earlier run named it: it promises no session opens it
// again.
func notOpenedWarning(dir string) string {
	return "the files you edited in an earlier session are kept in " + dir +
		"; what you typed there was not opened, because its conflict changed, its file is resolved or was not named, a newer copy was opened, or it is saved under another name, so copy what you need from it"
}

// partlyWarning is notOpenedWarning of a directory some of what was typed
// in which the session carried over, which it keeps for the rest.
func partlyWarning(dir string) string {
	return "the files you edited in an earlier session are kept in " + dir +
		"; what else you typed there was not opened, because its conflict changed, its file is resolved or was not named, a newer copy was opened, or it is saved under another name, so copy what you need from it"
}

// leftWarning is the warning completing kit's merge gives of the directory
// in dir of an earlier session that holds something typed, kept or its run
// gone: it is given up to the user.
func leftWarning(dir string) string {
	return "the files you edited in an earlier session are kept in " + dir + leftToYouKit
}

// givenUpWarning is the warning of a session whose merge was completed
// while its editor was open of the directory in dir, which holds what was
// typed there, given up to the user.
func givenUpWarning(dir string) string {
	return "the files you edited are kept in " + dir + leftToYouKit
}

// notCopyWarnings is the warnings of a run on kit but those of a copy
// edited in place, which completing kit's merge keeps with a warning of its
// own.
func notCopyWarnings(h *harness, stderr string) []string {
	var got []string
	for _, w := range warnings(h, stderr) {
		if !strings.Contains(w, "'s copy of kit is different from the library") {
			got = append(got, w)
		}
	}
	return got
}

// leftToYouKit ends a warning that names a directory given up to the user
// once kit's merge is complete: agentx never removes it, so it says to copy
// what is needed from it and delete it.
const leftToYouKit = ", which agentx leaves to you now that the merge of kit is complete; the folder is in your temporary directory, which your system may clean, so copy what you need from it and delete it"

// abortWarning is the warning giving kit's merge up gives of the directory
// in dir, which holds something typed in an editor session: it says when
// what was typed there opens again.
func abortWarning(dir string) string {
	return "the files you edited are kept in " + dir + ": when an update of kit conflicts the same way, 'agentx skill resolve kit --editor' opens what you typed again" + mayClean
}

// mayClean ends the warning giving kit's merge up gives of a directory that
// holds something typed a later session opens again: nothing reads it until
// then, and the system may clean the temporary directory meanwhile.
const mayClean = "; the folder is in your temporary directory, which your system may clean, so copy what you want to keep, and delete it if you do not need it"

// swapWarning is the warning that names the directory in dir, which holds
// nothing typed but under another name, a swap file say, which no session
// opens, as giving kit's merge up keeps it, or as a session keeps its own,
// a run stopped with nothing typed in the files included: it promises
// nothing opens again.
func swapWarning(dir string) string {
	return "the files you edited are kept in " + dir + "; what you typed there does not open again, because it is saved under another name, such as a swap file an editor left, so copy what you need from it"
}

// abortMixedWarning is the warning giving kit's merge up gives of the
// directory in dir, which holds something typed in a file an editor
// session was given and something under another name besides.
func abortMixedWarning(dir string) string {
	return "the files you edited are kept in " + dir + ": when an update of kit conflicts the same way, 'agentx skill resolve kit --editor' opens what you typed again" +
		"; a file saved there under another name, such as a swap file an editor left, does not open again" + mayClean
}

// reopenedNote is the note a session gives of a file it opens with what
// was typed for it in the earlier session whose directory is dir.
func reopenedNote(path, dir string) string {
	return path + " opens with what you typed in an earlier session, carried over from " + dir
}

// infos is the message of every log event of level info in stderr.
func infos(h *harness, stderr string) []string {
	var messages []string
	for _, e := range h.eventsOfType(stderr, "log") {
		if e["level"] == "info" {
			messages = append(messages, e["message"].(string))
		}
	}
	return messages
}

// seeingEditor is an editor that copies each file it is given, under its
// base name, into seen, a directory of the test's own, byte for byte, a
// last line with no newline included, so that a test sees what it was
// given, and then runs then, shell. It uses nothing but shell builtins.
func seeingEditor(t *testing.T, then string) (stub, seen string) {
	t.Helper()
	seen = t.TempDir()
	stub = editorStub(t, "seeing-editor", `for f in "$@"; do
	{ while IFS= read -r line; do printf '%s\n' "$line"; done; printf '%s' "$line"; } < "$f" > `+shellWord(seen)+`/"${f##*/}"
done
`+then)
	return stub, seen
}

// modTime is when the file at path was last written, to the nanosecond.
func modTime(t *testing.T, path string) string {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.ModTime().UTC().Format(time.RFC3339Nano)
}

// ownerOf is the first line of the owner file of an editor session's
// directory: the process id of its run, or kept.
func ownerOf(t *testing.T, dir string) string {
	t.Helper()
	first, _, _ := strings.Cut(fileBody(t, filepath.Join(dir, editorOwner)), "\n")
	return first
}

// TestSkillResolveAbortLeavesTheLibraryAsItWas: --abort gives the merge up,
// in a journal of one step that deletes the merge ref, and leaves the
// library directory, every placement, the import branch and the candidate
// byte for byte as they were. The files an editor session of the skill
// left for this home go with it when nothing was typed there, one its run
// kept included; a session still open is left alone, and finds the merge
// gone once its editor exits, and those of another skill or another home,
// their runs gone, stay, and so do those given up to the user, which no
// owner file claims.
// The skill is reported as it now stands, with no merge pending, and
// giving it up again is exit code 6.
func TestSkillResolveAbortLeavesTheLibraryAsItWas(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	resolveKit(t, h, 0, kitSides...)
	tip, candidate := h.ref(lineage.ManagedRef("kit")), h.ref(lineage.CandidateRef("kit"))
	library, placed := onDisk(t, h.library), onDisk(t, h.home)
	ours, kept := editorDir(t, h, "kit", ownPid, h.agentx), editorDir(t, h, "kit", "kept", h.agentx)
	other := editorDir(t, h, "other", goneProcess(t), h.agentx)
	elsewhere := editorDir(t, h, "kit", goneProcess(t), filepath.Join(h.agentx, "elsewhere"))
	yours := editorDir(t, h, "kit", goneProcess(t), h.agentx)
	remove(t, filepath.Join(yours, editorOwner))
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
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), "")
	nothingAt(t, "the skill's kept editor files, nothing typed in them", kept)
	for _, dir := range []string{ours, other, elsewhere, yours} {
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
// names, and so does one that leaves markers in a file it typed in, the
// files it wrote into the merge leaving the directory first: the next
// session opens what was typed, markers and all, in place of the file
// afresh, says so, and removes the directory it took it from. A directory
// kept for a file saved under another name, beside the file or beside the
// skill's folder, which no session opens, is given up to the user and
// named once the merge completes.
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
			"guide.md still holds conflict markers, so it was left unresolved\n"+
				"notes.md still holds conflict markers, so it was left unresolved")
		equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), merge)
		equal(t, "what the session left", strings.Join(editorDirs(t, h), " "), "")
		equal(t, "journals", journalCount(t, h), 0)
	})
	t.Run("an editor that leaves markers in a file it typed in", func(t *testing.T) {
		t.Parallel()
		h, _, _ := resolveHarness(t)
		h.env["EDITOR"] = editorStub(t, "careful-editor", `for f in "$@"; do
	case "$f" in
	*/guide.md) printf 'resolved in the editor\n' > "$f" ;;
	*) printf 'MY CAREFUL WORK\n' >> "$f" ;;
	esac
done
`)
		out := h.run("--json", "skill", "resolve", "kit", "--editor")
		equal(t, "exit", out.exit, 0)
		equal(t, "summary", h.one(out.stdout, "result")["summary"], "resolved guide.md in the merge of kit, 4 files left to resolve")
		equal(t, "what is left", conflictFiles(h.one(out.stdout, "conflict")), "gone.md:whole,kept.md:whole,logo.bin:binary,notes.md:2")
		equal(t, "guide.md in the merge", inMerge(h, "guide.md"), "resolved in the editor")
		dirs := editorDirs(t, h)
		if len(dirs) != 1 {
			t.Fatalf("the session left %v, want the one directory", dirs)
		}
		equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"),
			"notes.md still holds conflict markers, so it was left unresolved; what you typed is kept in "+dirs[0]+opensAgainKit("it"))
		equal(t, "the owner of what is kept", ownerOf(t, dirs[0]), "kept")
		typed := kitText["notes.md"] + "MY CAREFUL WORK\n"
		equal(t, "what the editor saved, kept", fileBody(t, filepath.Join(dirs[0], "kit", "notes.md")), typed)
		// guide.md is in the merge, so it leaves what is kept.
		nothingAt(t, "guide.md, written into the merge", filepath.Join(dirs[0], "kit", "guide.md"))
		equal(t, "journals", journalCount(t, h), 0)

		stub, seen := seeingEditor(t, `for f in "$@"; do printf 'finished my careful work\n' > "$f"; done`+"\n")
		h.env["EDITOR"] = stub
		out = h.run("--json", "skill", "resolve", "kit", "--editor")
		equal(t, "exit of the next session", out.exit, 0)
		equal(t, "what the next session opened", fileBody(t, filepath.Join(seen, "notes.md")), typed)
		equal(t, "the note", strings.Join(infos(h, out.stderr), "\n"), reopenedNote("notes.md", dirs[0]))
		equal(t, "the next session's warnings", strings.Join(warnings(h, out.stderr), "\n"), "")
		equal(t, "notes.md in the merge", inMerge(h, "notes.md"), "finished my careful work")
		// Everything the directory held that was not written is carried
		// over, so it goes.
		equal(t, "what is left once what was typed is written", strings.Join(editorDirs(t, h), " "), "")

		resolveKit(t, h, 0, kitWholeFiles...)
		equal(t, "what is left once the merge completed", strings.Join(editorDirs(t, h), " "), "")
	})
	t.Run("an editor that saves a file under another name", func(t *testing.T) {
		t.Parallel()
		for _, where := range []struct {
			name  string
			shell string // where the stub saves the draft of the file it was given, f
			path  string // and that, in the session's directory
		}{
			{name: "beside it", shell: `"${f%.md}.draft.md"`, path: filepath.Join("kit", "guide.draft.md")},
			{name: "beside the skill's folder", shell: `"${f%/kit/guide.md}/guide.draft.md"`, path: "guide.draft.md"},
		} {
			for _, c := range []struct {
				name      string
				resolving string   // a shell pattern of the files the editor resolves
				resolved  []string // what --hunk resolves first
				summary   string
				left      []string // the warnings of the files left, before the one that names the directory
				rest      []string // what --hunk resolves after, completing the merge, when the session does not
			}{
				{name: "and resolves a file", resolving: "*/guide.md", summary: "resolved guide.md in the merge of kit, 4 files left to resolve",
					left: []string{"notes.md still holds conflict markers, so it was left unresolved"},
					rest: append(slices.Clone(kitWholeFiles), "notes.md:1=mine", "notes.md:2=mine")},
				{name: "and resolves nothing", resolving: "-", summary: "resolved nothing in the merge of kit: 5 files left to resolve",
					left: []string{"guide.md still holds conflict markers, so it was left unresolved", "notes.md still holds conflict markers, so it was left unresolved"},
					rest: append(slices.Clone(kitWholeFiles), "guide.md:1=mine", "notes.md:1=mine", "notes.md:2=mine")},
				{name: "and completes the merge", resolving: "*", resolved: kitWholeFiles},
			} {
				t.Run(where.name+" "+c.name, func(t *testing.T) {
					t.Parallel()
					h, _, _ := resolveHarness(t)
					resolveKit(t, h, 0, c.resolved...)
					// Saved as, beside the file the editor was given or one
					// folder up, beside the skill's folder, as most editors
					// offer.
					h.env["EDITOR"] = editorStub(t, "drafting-editor", `for f in "$@"; do
	case "$f" in
	*/guide.md) printf 'MY DRAFT I SAVED ASIDE\n' > `+where.shell+` ;;
	esac
	case "$f" in
	`+c.resolving+`) printf 'resolved in the editor\n' > "$f" ;;
	esac
done
`)
					out := h.run("--json", "skill", "resolve", "kit", "--editor")
					equal(t, "exit", out.exit, 0)
					dirs := editorDirs(t, h)
					if len(dirs) != 1 {
						t.Fatalf("the session left %v, want the one directory", dirs)
					}
					equal(t, "the draft, kept", fileBody(t, filepath.Join(dirs[0], where.path)), "MY DRAFT I SAVED ASIDE\n")
					if c.rest == nil {
						contains(t, "summary", h.one(out.stdout, "result")["summary"].(string), "resolved kit and updated it from ")
						equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), givenUpWarning(dirs[0]))
						nothingAt(t, "the owner file of what is kept", filepath.Join(dirs[0], editorOwner))
						return
					}
					equal(t, "summary", h.one(out.stdout, "result")["summary"], c.summary)
					equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), strings.Join(append(c.left, swapWarning(dirs[0])), "\n"))
					equal(t, "the owner of what is kept", ownerOf(t, dirs[0]), "kept")

					// Completing the merge leaves the draft to the user and says so.
					out = resolveKit(t, h, 0, c.rest...)
					equal(t, "the merge ref, once completed", h.ref(lineage.MergeRef("kit")), "")
					equal(t, "warnings of the completion", strings.Join(notCopyWarnings(h, out.stderr), "\n"), leftWarning(dirs[0]))
					equal(t, "what is left once the merge completed", strings.Join(editorDirs(t, h), " "), dirs[0])
					nothingAt(t, "the owner file of what is left", filepath.Join(dirs[0], editorOwner))
					equal(t, "the draft, left", fileBody(t, filepath.Join(dirs[0], where.path)), "MY DRAFT I SAVED ASIDE\n")
				})
			}
		}
	})
	t.Run("an editor that deletes a file", func(t *testing.T) {
		t.Parallel()
		h, _, _ := resolveHarness(t)
		rm, err := exec.LookPath("rm")
		if err != nil {
			t.Fatal(err)
		}
		h.env["EDITOR"] = editorStub(t, "deleting-editor", `for f in "$@"; do
	case "$f" in
	*/guide.md) printf 'resolved in the editor\n' > "$f" ;;
	*) `+rm+` "$f" ;;
	esac
done
`)
		out := h.run("--json", "skill", "resolve", "kit", "--editor")
		equal(t, "exit", out.exit, 0)
		equal(t, "summary", h.one(out.stdout, "result")["summary"], "resolved guide.md in the merge of kit, 4 files left to resolve")
		equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), "notes.md is gone, so it was left unresolved")
		equal(t, "what the session left", strings.Join(editorDirs(t, h), " "), "")
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

		// The next session opens what the failing editor typed, which holds
		// no marker, so an editor that saves nothing resolves the file to it.
		stub, seen := seeingEditor(t, "exit 0\n")
		h.env["EDITOR"] = stub
		out = h.run("--json", "skill", "resolve", "kit", "--editor")
		equal(t, "exit of the next session", out.exit, 0)
		equal(t, "summary of the next session", h.one(out.stdout, "result")["summary"], "resolved guide.md in the merge of kit, 4 files left to resolve")
		equal(t, "what the next session opened", fileBody(t, filepath.Join(seen, "guide.md")), "half done\n")
		equal(t, "what the next session opened afresh", fileBody(t, filepath.Join(seen, "notes.md")), kitText["notes.md"])
		equal(t, "the note", strings.Join(infos(h, out.stderr), "\n"), reopenedNote("guide.md", dirs[0]))
		equal(t, "guide.md in the merge", inMerge(h, "guide.md"), "half done")
		equal(t, "what is left after the next session", strings.Join(editorDirs(t, h), " "), "")
		resolveKit(t, h, 0, append(kitSides, "kept.md:1=theirs")...)
		equal(t, "what is left once the merge completed", strings.Join(editorDirs(t, h), " "), "")
	})
	t.Run("a file resolved already, named after --editor", func(t *testing.T) {
		t.Parallel()
		h, _, _ := resolveHarness(t)
		resolveKit(t, h, 0, "guide.md:1=both")
		merge := h.ref(lineage.MergeRef("kit"))
		h.env["EDITOR"] = editorStub(t, "idle-editor", "exit 0\n")

		out := h.run("--json", "skill", "resolve", "kit", "--editor", "guide.md")
		equal(t, "exit", out.exit, 0)
		equal(t, "summary", h.one(out.stdout, "result")["summary"], "resolved nothing in the merge of kit: 4 files left to resolve")
		equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"),
			"guide.md still holds conflict markers, so it keeps the way it was resolved before")
		equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), merge)
		equal(t, "guide.md in the merge", h.accountGit("cat-file", "blob", merge+":kit-dir/guide.md"), "guide, mine\nguide, upstream")
	})
	t.Run("an editor that leaves a process holding its output", func(t *testing.T) {
		t.Parallel()
		h, _, _ := resolveHarness(t)
		candidate := h.ref(lineage.CandidateRef("kit"))
		resolveKit(t, h, 0, kitWholeFiles...)
		sleeper, err := exec.LookPath("sleep")
		if err != nil {
			t.Fatal(err)
		}
		// A process the editor starts in the background, as a wrapper
		// starts an editor server, holds the editor's stdout and stderr
		// after the editor exits. Its exit status is what counts.
		lingering := func(name, then string) string {
			return editorStub(t, name, sleeper+" 3 &\n"+then)
		}
		logged := func(out outcome) string {
			var lines []string
			for _, e := range h.eventsOfType(out.stderr, "log") {
				lines = append(lines, e["level"].(string)+": "+e["message"].(string))
			}
			return strings.Join(lines, "\n")
		}

		h.env["EDITOR"] = lingering("failing-editor", "echo \"editor failed\"\nexit 3\n")
		out := h.run("--json", "skill", "resolve", "kit", "--editor")
		equal(t, "exit of an editor that fails", out.exit, 6)
		equal(t, "message of an editor that fails", h.one(out.stdout, "error")["message"], "the editor "+h.env["EDITOR"]+" exited with status 3, so nothing was written")

		h.env["EDITOR"] = lingering("resolving-editor", `for f in "$@"; do printf 'resolved in the editor\n' > "$f"; done
echo "editor done"
`)
		out = h.run("--json", "skill", "resolve", "kit", "--editor")
		equal(t, "exit", out.exit, 0)
		contains(t, "summary", h.one(out.stdout, "result")["summary"].(string), "resolved kit and updated it from ")
		equal(t, "what the run logged", logged(out), "info: editor done")
		equal(t, "notes.md", fileBody(t, filepath.Join(h.library, "kit", "notes.md")), "resolved in the editor\n")
		equal(t, "the import branch", h.ref(lineage.ManagedRef("kit")), candidate)
		equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), "")
		equal(t, "what the sessions left", strings.Join(editorDirs(t, h), " "), "")
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

// settling is the debug line a run of --verbose writes once its editor
// exited and nothing it left was written, as it takes the lock to settle
// its directory, waiting for the lock while another command holds it. A
// test watching for it knows the run is past its write, and reads the
// merge only once it holds the lock, without sleeping for it.
const settling = "taking the lock to settle "

// awaitAlso blocks until the run wrote want too, as await does for the
// line start watches, once that one is seen.
func (p *runProc) awaitAlso(want string) {
	p.t.Helper()
	w := p.log
	w.mu.Lock()
	w.want, w.hit, w.seen = want, make(chan struct{}), strings.Contains(w.buf.String(), want)
	if w.seen {
		close(w.hit)
	}
	w.mu.Unlock()
	p.await()
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

// TestSkillResolveKeepsTheFilesWhenTheLockIsBusyAfterTheEditor: the run
// takes the lock again once the editor exits, without waiting for it to
// write. Held by another command then, one that writes or a scan alike, the
// run is exit code 7 with nothing written, and settles its directory once
// the lock is free, waiting for it: what the editor saved stays in its
// directory, kept, which a warning names; the next session opens it again
// and, as it holds no marker, writes it.
func TestSkillResolveKeepsTheFilesWhenTheLockIsBusyAfterTheEditor(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		hold func(t *testing.T, h *harness) (release func())
	}{
		{name: "held by a command that writes", hold: holdLock},
		{name: "held by a scan", hold: holdReadLock},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, _, _ := resolveHarness(t)
			merge := h.ref(lineage.MergeRef("kit"))
			stub, goFile := waitingEditor(t)
			h.env["EDITOR"] = stub
			p := h.start("editor open", "--json", "--verbose", "skill", "resolve", "kit", "--editor")
			p.await()

			release := c.hold(t, h)
			writeFile(t, goFile, "")
			p.awaitAlso(settling)
			release()
			out := p.wait()
			equal(t, "exit", out.exit, 7)
			equal(t, "code", h.one(out.stdout, "error")["code"], "locked")
			equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), merge)
			equal(t, "journals", journalCount(t, h), 0)
			dirs := editorDirs(t, h)
			if len(dirs) != 1 {
				t.Fatalf("the session left %v, want the one directory", dirs)
			}
			equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), keptWarning(dirs[0]))
			equal(t, "the owner of what is kept", ownerOf(t, dirs[0]), "kept")
			equal(t, "what the editor saved", fileBody(t, filepath.Join(dirs[0], "kit", "notes.md")), "resolved in the editor\n")

			h.env["EDITOR"] = editorStub(t, "idle-editor", "exit 0\n")
			out = h.run("--json", "skill", "resolve", "kit", "--editor")
			equal(t, "exit of the next session", out.exit, 0)
			equal(t, "summary of the next session", h.one(out.stdout, "result")["summary"], "resolved 2 files in the merge of kit, 3 files left to resolve")
			equal(t, "notes.md in the merge", inMerge(h, "notes.md"), "resolved in the editor")
			equal(t, "what is left after the next session", strings.Join(editorDirs(t, h), " "), "")
		})
	}
}

// TestSkillResolveSettlesItsDirectoryAfterACompletionUnderWay: a session
// whose editor exits while another run completes the merge, holding the
// lock with the merge ref not yet deleted, is refused the lock as it
// writes, and settles its directory only once that completion is done,
// waiting for the lock: it finds the merge complete, and gives the
// directory up to the user with what the editor saved in it, which its
// warning says. The completion settles the directories of earlier
// sessions under the hold that deletes the merge ref, while this one's run
// is still going, so it leaves this one to the session and names nothing.
func TestSkillResolveSettlesItsDirectoryAfterACompletionUnderWay(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	arm := gateGit(t, h, `case " $* " in
*" update-ref --stdin "*) gate=1 ;;
esac`)
	stub, goFile := waitingEditor(t)
	h.env["EDITOR"] = stub
	p := h.start("editor open", "--json", "--verbose", "skill", "resolve", "kit", "--editor")
	p.await()

	reached, release := arm()
	args := []string{"--json", "skill", "resolve", "kit"}
	for _, hunk := range append(slices.Clone(kitSides), "kept.md:1=theirs") {
		args = append(args, "--hunk", hunk)
	}
	completion := h.start("", args...)
	reached() // the completion holds the lock, and has written none of its refs
	writeFile(t, goFile, "")
	p.awaitAlso(settling) // the session was refused the lock, and waits for it
	release()
	completed := completion.wait()
	out := p.wait()

	equal(t, "exit of the completion", completed.exit, 0)
	contains(t, "summary of the completion", h.one(completed.stdout, "result")["summary"].(string), "resolved kit and updated it from ")
	equal(t, "warnings of the completion", strings.Join(warnings(h, completed.stderr), "\n"), "")
	equal(t, "exit", out.exit, 7)
	equal(t, "code", h.one(out.stdout, "error")["code"], "locked")
	equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), "")
	dirs := editorDirs(t, h)
	if len(dirs) != 1 {
		t.Fatalf("the session left %v, want the one directory", dirs)
	}
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), givenUpWarning(dirs[0]))
	nothingAt(t, "the owner file of what is kept", filepath.Join(dirs[0], editorOwner))
	equal(t, "what the editor saved", fileBody(t, filepath.Join(dirs[0], "kit", "notes.md")), "resolved in the editor\n")
	equal(t, "what the editor saved of guide.md", fileBody(t, filepath.Join(dirs[0], "kit", "guide.md")), "resolved in the editor\n")
}

// TestSkillResolveSettlesWhatItWroteForAMergeDoneMeanwhile holds an editor
// session at "written", see editorLayoutHook, once it has written what its
// editor resolved and before it settles its directory, where no command it
// starts falls, while another run completes the merge or gives it up. The
// editor resolved guide.md, which leaves the directory, and left markers
// and something typed in notes.md, which keeps it. A completion gives the
// directory up to the user, its owner file gone and what was typed still
// in it, and the warning that names it promises nothing opens it again.
// Giving the merge up keeps it, marked kept, and the warning says, as
// giving the merge up says it, that an update that conflicts the same way
// opens what was typed again, and that a swap file the editor left there
// does not; the next session after such an update does. A session that
// merged the merge again on a moved import branch, notes.md conflicting
// anew, promises nothing for what was typed for the conflict as it was.
//
// The hook is the package's, and every run in this binary reaches it, so
// the test runs alone, never in parallel.
func TestSkillResolveSettlesWhatItWroteForAMergeDoneMeanwhile(t *testing.T) {
	const typed = "MY CAREFUL WORK\n"
	for _, c := range []struct {
		name               string
		abort, swap, moved bool
	}{
		{name: "completed"},
		{name: "given up", abort: true},
		{name: "given up with a swap file", abort: true, swap: true},
		{name: "given up once merged again", abort: true, moved: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, _, updated := resolveHarness(t)
			if c.moved {
				moved := versionOf(t, h, updated["base"].(string), map[string]string{"notes.md": "one\ntwo\nthree\nfour\ncinco\n"})
				h.accountGit("update-ref", lineage.ManagedRef("kit"), moved)
			}
			then := `for f in "$@"; do
	case "$f" in
	*/guide.md) printf 'resolved in the editor\n' > "$f" ;;
	*/notes.md) printf 'MY CAREFUL WORK\n' >> "$f" ;;
	esac
done
`
			if c.swap {
				then += `for f in "$@"; do case "$f" in */notes.md) printf 'unsaved typing\n' > "${f%/*}/.notes.md.swp" ;; esac; done` + "\n"
			}
			h.env["EDITOR"] = editorStub(t, "careful-editor", then)
			written, resume := make(chan struct{}), make(chan struct{})
			editorLayoutHook = func(step string) {
				if step == "written" {
					close(written)
					<-resume
				}
			}
			t.Cleanup(func() { editorLayoutHook = nil })
			p := h.start("", "--json", "skill", "resolve", "kit", "--editor")
			var once sync.Once
			release := func() { once.Do(func() { close(resume) }) }
			t.Cleanup(release)
			select {
			case <-written:
			case <-p.done:
				t.Fatalf("the session ended before it settled its directory:\n%s%s", p.stdout.String(), p.log.text())
			case <-time.After(runDeadline):
				t.Fatalf("the session wrote nothing within %s:\n%s", runDeadline, p.log.text())
			}
			dirs := editorDirs(t, h)
			if len(dirs) != 1 {
				t.Fatalf("the session has %v, want the one directory", dirs)
			}
			dir := dirs[0]
			if c.abort {
				out := h.run("--json", "skill", "resolve", "kit", "--abort")
				equal(t, "exit of the abort", out.exit, 0)
				equal(t, "warnings of the abort", strings.Join(warnings(h, out.stderr), "\n"), "")
			} else {
				out := resolveKit(t, h, 0, append(slices.Clone(kitSides), "kept.md:1=theirs")...)
				equal(t, "warnings of the completion", strings.Join(notCopyWarnings(h, out.stderr), "\n"), "")
			}
			release()
			out := p.wait()

			exit := 0
			if c.moved {
				exit = 4
			}
			equal(t, "exit", out.exit, exit)
			equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), "")
			equal(t, "what is left", strings.Join(editorDirs(t, h), " "), dir)
			nothingAt(t, "guide.md, written", filepath.Join(dir, "kit", "guide.md"))
			equal(t, "what was typed", fileBody(t, filepath.Join(dir, "kit", "notes.md")), kitText["notes.md"]+typed)
			kept := "notes.md still holds conflict markers, so it was left unresolved; what you typed is kept in " + dir
			switch {
			case !c.abort:
				nothingAt(t, "the owner file", filepath.Join(dir, editorOwner))
				got := strings.Join(warnings(h, out.stderr), "\n")
				equal(t, "warnings", got, kept+leftToYouKit)
				if strings.Contains(got, "opens it again") {
					t.Errorf("a warning promises a session opens what was typed again: %s", got)
				}
				return
			case c.moved:
				equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"),
					kept+"; what you typed there does not open again, because its conflict changed before the merge was given up, so copy what you need from it")
				equal(t, "the owner", ownerOf(t, dir), "kept")
				return
			}
			given := abortWarning(dir)
			if c.swap {
				given = abortMixedWarning(dir)
				equal(t, "the swap file", fileBody(t, filepath.Join(dir, "kit", ".notes.md.swp")), "unsaved typing\n")
			}
			equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), kept+strings.TrimPrefix(given, "the files you edited are kept in "+dir))
			equal(t, "the owner", ownerOf(t, dir), "kept")

			equal(t, "the update that conflicts the same way", h.run("skill", "update", "kit").exit, 4)
			stub, seen := seeingEditor(t, "exit 0\n")
			h.env["EDITOR"] = stub
			out = h.run("--json", "skill", "resolve", "kit", "--editor")
			equal(t, "exit of the next session", out.exit, 0)
			equal(t, "what the next session opened", fileBody(t, filepath.Join(seen, "notes.md")), kitText["notes.md"]+typed)
			equal(t, "notes", strings.Join(infos(h, out.stderr), "\n"), reopenedNote("notes.md", dir))
		})
	}
}

// TestSkillResolveInAnEditorRefusesABusyLock: laying the files out takes
// the lock without waiting for it, as every command that writes does. Held
// by another command, a scan included, the run is exit code 7 before any
// file is laid out: no directory is made, no editor starts, and the merge
// is as it was.
func TestSkillResolveInAnEditorRefusesABusyLock(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	merge := h.ref(lineage.MergeRef("kit"))
	ran := filepath.Join(t.TempDir(), "ran")
	h.env["EDITOR"] = editorStub(t, "marking-editor", "echo ran > "+shellWord(ran)+"\n")
	holdReadLock(t, h)

	out := h.run("--json", "skill", "resolve", "kit", "--editor")
	equal(t, "exit", out.exit, 7)
	equal(t, "code", h.one(out.stdout, "error")["code"], "locked")
	nothingAt(t, "what the editor marks when it runs", ran)
	equal(t, "what the session left", strings.Join(editorDirs(t, h), " "), "")
	equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), merge)
	equal(t, "journals", journalCount(t, h), 0)
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
// given stays in its directory, kept, with what was typed and saved in the
// files before the stop, which a warning names, promising the next session
// of the same conflict opens it again; the merge is left as it was.
func TestSkillResolveStoppedWhileTheEditorIsOpenKeepsTheFiles(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	merge := h.ref(lineage.MergeRef("kit"))
	stub, _ := waitThen(t, "exit 0\n")
	ready := filepath.Join(t.TempDir(), "open")
	h.env["PATH"] = gitOnlyPath(t) // as TestSkillResolveLeavesACtrlCToTheEditor says
	h.env["GIT_EDITOR"] = editorStub(t, "reporting-editor", `for f in "$@"; do printf 'typed before the stop\n' > "$f"; done
echo open > `+shellWord(ready)+"\nexec "+shellWord(stub)+` "$@"`+"\n")

	code, stderr := signalled(t, h, stopRun{ready: ready, sigs: []syscall.Signal{syscall.SIGTERM}, group: true,
		args: []string{"skill", "resolve", "kit", "--editor", "--color", "off"}})
	equal(t, "exit", code, exitInterrupted.exit)
	contains(t, "stderr", stderr, "error: interrupted")
	dirs := editorDirs(t, h)
	if len(dirs) != 1 {
		t.Fatalf("the run left %v, want the one directory:\n%s", dirs, stderr)
	}
	contains(t, "the warning", stderr, "warning: "+keptWarning(dirs[0])+"\n")
	equal(t, "what the editor typed, kept", fileBody(t, filepath.Join(dirs[0], "kit", "notes.md")), "typed before the stop\n")
	equal(t, "the owner of what is kept", ownerOf(t, dirs[0]), "kept")
	equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), merge)
	equal(t, "journals", journalCount(t, h), 0)
}

// TestSkillResolveStoppedWhileTheEditorIsOpenStopsTheEditor: a SIGTERM
// sent to agentx alone, as a supervisor or the app sends one, reaches the
// editor too, and not only the sh that runs it: the editor stops before the
// work it would do next, rather than going on after the run is over. The
// run exits 9 and keeps the files, as any stopped run does, with nothing
// typed in them, so its warning promises nothing opens again.
func TestSkillResolveStoppedWhileTheEditorIsOpenStopsTheEditor(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	merge := h.ref(lineage.MergeRef("kit"))
	sleeper, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	logs := t.TempDir()
	ready, stopped, after := filepath.Join(logs, "open"), filepath.Join(logs, "stopped"), filepath.Join(logs, "after")
	h.env["PATH"] = gitOnlyPath(t) // as TestSkillResolveLeavesACtrlCToTheEditor says
	// The editor records the SIGTERM it gets and exits; one that never gets
	// it writes after once it has waited a minute.
	h.env["GIT_EDITOR"] = editorStub(t, "stoppable-editor", `trap 'echo stopped > `+shellWord(stopped)+`; exit 143' TERM
echo open > `+shellWord(ready)+`
i=0
while [ $i -lt 1200 ]; do `+sleeper+` 0.05; i=$((i+1)); done
echo after > `+shellWord(after)+`
`)

	code, stderr := signalled(t, h, stopRun{ready: ready, sigs: []syscall.Signal{syscall.SIGTERM},
		args: []string{"skill", "resolve", "kit", "--editor", "--color", "off"}})
	equal(t, "exit", code, exitInterrupted.exit)
	contains(t, "stderr", stderr, "error: interrupted")
	if !waitFor(stopped) {
		t.Fatalf("the editor never got the SIGTERM:\n%s", stderr)
	}
	nothingAt(t, "what the editor would have written next", after)
	dirs := editorDirs(t, h)
	if len(dirs) != 1 {
		t.Fatalf("the run left %v, want the one directory:\n%s", dirs, stderr)
	}
	contains(t, "the warning", stderr, "warning: "+swapWarning(dirs[0])+"\n")
	equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), merge)
	equal(t, "journals", journalCount(t, h), 0)
}

// TestSkillResolveStoppedWhileTheEditorIsOpenTellsTheMergeAsItIs: a run
// stopped by SIGTERM once another run changed the merge while its editor
// was open, the files typed in, still reads the merge under the lock as it
// settles its directory, and its warning tells the merge as that run left
// it: a merge completed gives the directory up to the user, its owner file
// gone; one given up keeps it, with the warning giving the merge up gives;
// and one another run resolved a file of keeps it, promising no more than
// a moved merge does. A scan that holds the lock, shared, as the run is
// stopped, which its wait gives up on, changes none of that: the run reads
// the merge without the lock, and never promises a merge gone opens again.
func TestSkillResolveStoppedWhileTheEditorIsOpenTellsTheMergeAsItIs(t *testing.T) {
	t.Parallel()
	sleeper, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name      string
		meanwhile []string
		scan      bool // a scan holds the lock, shared, once the merge changed
		warning   func(dir string) string
		owner     string // "" for a directory given up to the user
	}{
		{name: "completed", meanwhile: append([]string{"--hunk", "kept.md:1=theirs"}, hunks(kitSides)...), warning: givenUpWarning},
		{name: "given up", meanwhile: []string{"--abort"}, warning: abortWarning, owner: "kept"},
		{name: "resolved in part", meanwhile: []string{"--hunk", "logo.bin:1=mine"}, warning: movedWarning, owner: "kept"},
		{name: "completed while a scan reads", meanwhile: append([]string{"--hunk", "kept.md:1=theirs"}, hunks(kitSides)...), scan: true, warning: givenUpWarning},
		{name: "given up while a scan reads", meanwhile: []string{"--abort"}, scan: true, warning: abortWarning, owner: "kept"},
		{name: "resolved in part while a scan reads", meanwhile: []string{"--hunk", "logo.bin:1=mine"}, scan: true, warning: movedWarning, owner: "kept"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, _, _ := resolveHarness(t)
			logs := t.TempDir()
			typed, goFile, ready := filepath.Join(logs, "typed"), filepath.Join(logs, "go"), filepath.Join(logs, "open")
			h.env["PATH"] = gitOnlyPath(t) // as TestSkillResolveLeavesACtrlCToTheEditor says
			// The editor types in every file, then waits for the other run
			// to change the merge before it says it is open, and then for
			// the SIGTERM.
			h.env["GIT_EDITOR"] = editorStub(t, "typing-editor", `for f in "$@"; do printf 'typed before the stop\n' > "$f"; done
echo typed > `+shellWord(typed)+`
i=0
while [ ! -e `+shellWord(goFile)+` ] && [ $i -lt 1200 ]; do `+sleeper+` 0.05; i=$((i+1)); done
echo open > `+shellWord(ready)+`
i=0
while [ $i -lt 1200 ]; do `+sleeper+` 0.05; i=$((i+1)); done
`)
			meanwhile := outcome{exit: -1}
			var scan *os.File // the scan's shared hold of the lock
			var scanErr error
			done := make(chan struct{})
			go func() {
				defer close(done)
				if waitFor(typed) {
					meanwhile = h.run(append([]string{"skill", "resolve", "kit"}, c.meanwhile...)...)
					if c.scan {
						// Held as holdReadLock holds it, from a goroutine that
						// may not fail the test itself.
						if scan, scanErr = os.OpenFile(filepath.Join(h.agentx, "lock"), os.O_CREATE|os.O_RDWR, 0o644); scanErr == nil {
							scanErr = syscall.Flock(int(scan.Fd()), syscall.LOCK_SH)
						}
					}
				}
				_ = os.WriteFile(goFile, []byte("go\n"), 0o600)
			}()
			t.Cleanup(func() {
				<-done
				if scan != nil {
					scan.Close()
				}
			})

			code, stderr := signalled(t, h, stopRun{ready: ready, sigs: []syscall.Signal{syscall.SIGTERM}, group: true,
				args: []string{"skill", "resolve", "kit", "--editor", "--color", "off"}})
			<-done
			if scanErr != nil {
				t.Fatal(scanErr)
			}
			equal(t, "exit of the run meanwhile", meanwhile.exit, 0)
			equal(t, "exit", code, exitInterrupted.exit)
			contains(t, "stderr", stderr, "error: interrupted")
			dirs := editorDirs(t, h)
			if len(dirs) != 1 {
				t.Fatalf("the run left %v, want the one directory:\n%s", dirs, stderr)
			}
			contains(t, "the warning", stderr, "warning: "+c.warning(dirs[0])+"\n")
			if strings.Count(stderr, "warning: ") != 1 {
				t.Errorf("want the one warning:\n%s", stderr)
			}
			equal(t, "what the editor typed, kept", fileBody(t, filepath.Join(dirs[0], "kit", "notes.md")), "typed before the stop\n")
			if c.owner == "" {
				nothingAt(t, "the owner file of what is kept", filepath.Join(dirs[0], editorOwner))
				return
			}
			equal(t, "the owner of what is kept", ownerOf(t, dirs[0]), c.owner)
		})
	}
}

// TestSkillResolveStoppedKeepsWhatTheEditorSavesAsItStops: an editor sent
// SIGTERM may write what it held beside the file as it stops, as nano and
// Emacs do, after the sh that runs it is gone and the run has read the
// directory. So a run stopped while its editor is open never removes the
// directory, even with nothing typed in the files when it reads them: once
// another run completed the merge meanwhile it gives the directory up to
// the user, once another run gave it up it keeps it, and so it does with
// the merge left pending, and a warning names it each time. What the editor
// writes as it stops is there. Saved under another name, it is opened by
// no later session, so the warning of a merge left pending promises no
// session opens it, and the next session names the directory as one it
// did not open.
func TestSkillResolveStoppedKeepsWhatTheEditorSavesAsItStops(t *testing.T) {
	t.Parallel()
	sleeper, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name      string
		meanwhile []string // nil for no run meanwhile, the merge left pending as it was
		warning   func(dir string) string
		owner     string // "" for a directory given up to the user
	}{
		{name: "completed", meanwhile: append([]string{"--hunk", "kept.md:1=theirs"}, hunks(kitSides)...), warning: givenUpWarning},
		{name: "given up", meanwhile: []string{"--abort"}, warning: swapWarning, owner: "kept"},
		{name: "pending", warning: swapWarning, owner: "kept"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, _, _ := resolveHarness(t)
			logs := t.TempDir()
			opened, goFile, ready, saved := filepath.Join(logs, "opened"), filepath.Join(logs, "go"), filepath.Join(logs, "open"), filepath.Join(logs, "saved")
			h.env["PATH"] = gitOnlyPath(t) // as TestSkillResolveLeavesACtrlCToTheEditor says
			// The editor types nothing. It waits for the other run to change
			// the merge before it says it is open, and then for the SIGTERM,
			// which it answers a moment later, as an editor that saves what it
			// held does, with a file beside each one it was given.
			h.env["GIT_EDITOR"] = editorStub(t, "saving-editor", `stop() {
	`+sleeper+` 0.3
	for f in "$@"; do printf 'unsaved typing\n' > "$f.save"; done
	echo saved > `+shellWord(saved)+`
	exit 143
}
trap 'stop "$@"' TERM
echo opened > `+shellWord(opened)+`
i=0
while [ ! -e `+shellWord(goFile)+` ] && [ $i -lt 1200 ]; do `+sleeper+` 0.05; i=$((i+1)); done
echo open > `+shellWord(ready)+`
i=0
while [ $i -lt 1200 ]; do `+sleeper+` 0.05; i=$((i+1)); done
`)
			merge := h.ref(lineage.MergeRef("kit"))
			meanwhile := outcome{exit: -1}
			done := make(chan struct{})
			go func() {
				defer close(done)
				if waitFor(opened) && c.meanwhile != nil {
					meanwhile = h.run(append([]string{"skill", "resolve", "kit"}, c.meanwhile...)...)
				}
				_ = os.WriteFile(goFile, []byte("go\n"), 0o600)
			}()
			t.Cleanup(func() { <-done })

			code, stderr := signalled(t, h, stopRun{ready: ready, sigs: []syscall.Signal{syscall.SIGTERM},
				args: []string{"skill", "resolve", "kit", "--editor", "--color", "off"}})
			<-done
			if c.meanwhile != nil {
				equal(t, "exit of the run meanwhile", meanwhile.exit, 0)
			}
			equal(t, "exit", code, exitInterrupted.exit)
			contains(t, "stderr", stderr, "error: interrupted")
			if !waitFor(saved) {
				t.Fatalf("the editor never saved what it held:\n%s", stderr)
			}
			dirs := editorDirs(t, h)
			if len(dirs) != 1 {
				t.Fatalf("the run left %v, want the one directory:\n%s", dirs, stderr)
			}
			contains(t, "the warning", stderr, "warning: "+c.warning(dirs[0])+"\n")
			equal(t, "what the editor saved as it stopped", fileBody(t, filepath.Join(dirs[0], "kit", "notes.md.save")), "unsaved typing\n")
			equal(t, "notes.md, as the editor was given it", fileBody(t, filepath.Join(dirs[0], "kit", "notes.md")), kitText["notes.md"])
			if c.owner == "" {
				nothingAt(t, "the owner file of what is kept", filepath.Join(dirs[0], editorOwner))
				return
			}
			equal(t, "the owner of what is kept", ownerOf(t, dirs[0]), c.owner)
			if c.meanwhile != nil {
				return
			}

			// The next session of the same conflict opens the files afresh,
			// and names the directory as one it did not open.
			equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), merge)
			stub, seen := seeingEditor(t, "exit 0\n")
			h.env["GIT_EDITOR"] = stub
			out := h.run("--json", "skill", "resolve", "kit", "--editor")
			equal(t, "exit of the next session", out.exit, 0)
			equal(t, "what the next session opened, afresh", fileBody(t, filepath.Join(seen, "notes.md")), kitText["notes.md"])
			equal(t, "the notes of the next session", strings.Join(infos(h, out.stderr), "\n"), "")
			equal(t, "the warnings of the next session", strings.Join(warnings(h, out.stderr), "\n"), notOpenedWarning(dirs[0])+
				"\nguide.md still holds conflict markers, so it was left unresolved\nnotes.md still holds conflict markers, so it was left unresolved")
			equal(t, "what the editor saved as it stopped, still kept", fileBody(t, filepath.Join(dirs[0], "kit", "notes.md.save")), "unsaved typing\n")
			equal(t, "what the sessions left", strings.Join(editorDirs(t, h), " "), dirs[0])
		})
	}
}

// hunks is each of choices after --hunk.
func hunks(choices []string) []string {
	var args []string
	for _, c := range choices {
		args = append(args, "--hunk", c)
	}
	return args
}

// TestSkillResolveFindsTheMergeRewrittenWhileTheEditorIsOpen: a merge
// another run resolved part of while the editor was open is not
// overwritten. The editor's run writes nothing, exits 6, and keeps what
// the editor saved in its directory, which a warning names; the merge ref
// holds what the other run wrote. A new session, as the hint suggests,
// opens what was kept again, the files conflicting as they did, and
// writes it.
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
	equal(t, "what the run logged", strings.Join(logged, "\n"), "info: editor open\nwarn: "+movedWarning(dirs[0]))
	equal(t, "what the editor saved", fileBody(t, filepath.Join(dirs[0], "kit", "notes.md")), "resolved in the editor\n")
	equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), meanwhile)
	if got := h.accountGit("cat-file", "blob", meanwhile+":kit-dir/notes.md"); strings.Contains(got, "resolved in the editor") {
		t.Errorf("the editor's notes.md reached the merge: %q", got)
	}

	h.env["EDITOR"] = editorStub(t, "idle-editor", "exit 0\n")
	out = h.run("--json", "skill", "resolve", "kit", "--editor")
	equal(t, "exit of the new session", out.exit, 0)
	equal(t, "notes", strings.Join(infos(h, out.stderr), "\n"), reopenedNote("guide.md", dirs[0])+"\n"+reopenedNote("notes.md", dirs[0]))
	equal(t, "notes.md in the merge, after a new session", inMerge(h, "notes.md"), "resolved in the editor")
	equal(t, "what is left after a new session", strings.Join(editorDirs(t, h), " "), "")
}

// TestSkillResolveKeepsWhatWasTypedForAFileResolvedWhileTheEditorIsOpen: a
// session of every file left whose files another run resolves one of while
// the editor is open writes nothing, exits 6, and keeps what the editor
// saved, and its warning promises the next session opens again only what
// was typed for a file still left to resolve: that session, which does not
// lay out the file the other run resolved, carries over what was typed for
// the rest, and names the directory, which still holds what was typed for
// the file resolved meanwhile.
func TestSkillResolveKeepsWhatWasTypedForAFileResolvedWhileTheEditorIsOpen(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	stub, goFile := waitingEditor(t)
	h.env["EDITOR"] = stub
	p := h.start("editor open", "--json", "skill", "resolve", "kit", "--editor")
	p.await()
	resolveKit(t, h, 0, "notes.md:1=mine", "notes.md:2=theirs")

	writeFile(t, goFile, "")
	out := p.wait()
	equal(t, "exit", out.exit, 6)
	equal(t, "message", h.one(out.stdout, "error")["message"], "the merge of kit changed while it was being resolved, so nothing was written")
	dirs := editorDirs(t, h)
	if len(dirs) != 1 {
		t.Fatalf("the session left %v, want the one directory", dirs)
	}
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), movedWarning(dirs[0]))
	equal(t, "the owner of what is kept", ownerOf(t, dirs[0]), "kept")

	h.env["EDITOR"] = editorStub(t, "idle-editor", "exit 0\n")
	out = h.run("--json", "skill", "resolve", "kit", "--editor")
	equal(t, "exit of the next session", out.exit, 0)
	equal(t, "summary of the next session", h.one(out.stdout, "result")["summary"], "resolved guide.md in the merge of kit, 3 files left to resolve")
	equal(t, "the note", strings.Join(infos(h, out.stderr), "\n"), reopenedNote("guide.md", dirs[0]))
	equal(t, "the next session's warnings", strings.Join(warnings(h, out.stderr), "\n"), partlyWarning(dirs[0]))
	equal(t, "guide.md in the merge", inMerge(h, "guide.md"), "resolved in the editor")
	equal(t, "notes.md in the merge, as the other run resolved it", inMerge(h, "notes.md"), "ONE\ntwo\nthree\nfour\ncinco")
	equal(t, "what is left", strings.Join(editorDirs(t, h), " "), dirs[0])
	equal(t, "what was typed for notes.md, still kept", fileBody(t, filepath.Join(dirs[0], "kit", "notes.md")), "resolved in the editor\n")
	nothingAt(t, "guide.md, carried over", filepath.Join(dirs[0], "kit", "guide.md"))
}

// TestSkillResolveReportsNothingOfAMergeGoneWhileTheEditorIsOpen: a
// session that resolved nothing reads the merge ref again before it reports
// the merge. A merge given up or completed while the editor was open is
// exit code 6, and its files go, since nothing was typed in them; one
// another run resolved part of is exit code 6 too, and keeps the files the
// editor was given, which a warning names, so that nothing typed there is
// lost. No conflict event reports a merge that is no longer the one
// pending.
func TestSkillResolveReportsNothingOfAMergeGoneWhileTheEditorIsOpen(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, message string
		meanwhile     func(t *testing.T, h *harness)
		kept          bool
	}{
		{name: "given up", message: "kit has no merge pending: it was given up or completed while the editor was open",
			meanwhile: func(_ *testing.T, h *harness) { h.mustRun("skill", "resolve", "kit", "--abort") }},
		{name: "completed", message: "kit has no merge pending: it was given up or completed while the editor was open",
			meanwhile: func(t *testing.T, h *harness) {
				resolveKit(t, h, 0, append(slices.Clone(kitSides), "kept.md:1=theirs")...)
			}},
		{name: "resolved in part", message: "the merge of kit changed while the editor was open, so nothing was written", kept: true,
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
			dirs := editorDirs(t, h)
			if !c.kept {
				equal(t, "what the session left", strings.Join(dirs, " "), "")
				equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), "")
				return
			}
			if len(dirs) != 1 {
				t.Fatalf("the session left %v, want the one directory", dirs)
			}
			equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), movedWarning(dirs[0]))
			equal(t, "the owner of what is kept", ownerOf(t, dirs[0]), "kept")
			equal(t, "what the editor was given, kept", fileBody(t, filepath.Join(dirs[0], "kit", "guide.md")),
				"<<<<<<< mine\nguide, mine\n||||||| base\nguide\n=======\nguide, upstream\n>>>>>>> theirs\n")
		})
	}
}

// TestSkillResolveKeepsWhatWasTypedForAMergeGoneWhileTheEditorIsOpen: a
// session whose editor resolved files of a merge another run completed or
// gave up meanwhile writes nothing, exit code 6, and keeps what the editor
// saved in its directory, which a warning names. The library holds what
// the other run completed, or is as it was. A merge given up keeps the
// directory marked kept, with the warning giving the merge up gives, which
// says an update has to leave the merge pending again first, and once an
// update that conflicts the same way does, the next session opens what was
// typed again, and writes it. A merge completed can never conflict the same way
// again, so the directory is given up to the user, its owner file gone, and
// the warning says so, promising nothing: the completion of a later merge
// leaves it as it is.
func TestSkillResolveKeepsWhatWasTypedForAMergeGoneWhileTheEditorIsOpen(t *testing.T) {
	t.Parallel()
	for _, completed := range []bool{true, false} {
		t.Run(fmt.Sprintf("completed %v", completed), func(t *testing.T) {
			t.Parallel()
			h, s, _ := resolveHarness(t)
			library := onDisk(t, h.library)
			stub, goFile := waitingEditor(t)
			h.env["EDITOR"] = stub
			p := h.start("editor open", "--json", "skill", "resolve", "kit", "--editor")
			p.await()
			if completed {
				resolveKit(t, h, 0, append(slices.Clone(kitSides), "kept.md:1=theirs")...)
			} else {
				h.mustRun("skill", "resolve", "kit", "--abort")
			}

			writeFile(t, goFile, "")
			out := p.wait()
			equal(t, "exit", out.exit, 6)
			equal(t, "message", h.one(out.stdout, "error")["message"], "the merge of kit changed while it was being resolved, so nothing was written")
			dirs := editorDirs(t, h)
			if len(dirs) != 1 {
				t.Fatalf("the session left %v, want the one directory", dirs)
			}
			equal(t, "what the editor saved", fileBody(t, filepath.Join(dirs[0], "kit", "notes.md")), "resolved in the editor\n")
			equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), "")
			if completed {
				equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), givenUpWarning(dirs[0]))
				nothingAt(t, "the owner file of what is kept", filepath.Join(dirs[0], editorOwner))
				sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "kit")), kitTree(t, h, kitResolved))

				s.write("skills/kit-dir/guide.md", "guide, upstream again\n")
				s.commit("third version")
				h.mustRun("skill", "check")
				if out := h.run("--json", "skill", "update", "kit"); out.exit != 4 || conflictFiles(h.one(out.stdout, "conflict")) != "guide.md:1" {
					t.Fatalf("the later update: exit %d\n%s%s", out.exit, out.stdout, out.stderr)
				}
				resolveKit(t, h, 0, "guide.md:1=theirs")
				equal(t, "what is left once a later merge completed", strings.Join(editorDirs(t, h), " "), dirs[0])
				equal(t, "what the editor saved, still there", fileBody(t, filepath.Join(dirs[0], "kit", "notes.md")), "resolved in the editor\n")
				return
			}
			// No merge is pending, so the warning says an update has to
			// leave it pending again first, as giving it up says.
			equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), abortWarning(dirs[0]))
			equal(t, "the owner of what is kept", ownerOf(t, dirs[0]), "kept")
			equal(t, "the library", onDisk(t, h.library), library)

			equal(t, "the update that conflicts again", h.run("skill", "update", "kit").exit, 4)
			stub, seen := seeingEditor(t, "exit 0\n")
			h.env["EDITOR"] = stub
			out = h.run("--json", "skill", "resolve", "kit", "--editor")
			equal(t, "exit of the next session", out.exit, 0)
			equal(t, "summary of the next session", h.one(out.stdout, "result")["summary"], "resolved 2 files in the merge of kit, 3 files left to resolve")
			equal(t, "what the next session opened", fileBody(t, filepath.Join(seen, "guide.md")), "resolved in the editor\n")
			equal(t, "notes", strings.Join(infos(h, out.stderr), "\n"), reopenedNote("guide.md", dirs[0])+"\n"+reopenedNote("notes.md", dirs[0]))
			equal(t, "notes.md in the merge", inMerge(h, "notes.md"), "resolved in the editor")
			equal(t, "what is left after the next session", strings.Join(editorDirs(t, h), " "), "")
		})
	}
}

// TestSkillResolveSaysWhatAnEditorLeftUnderAnotherNameForAMergeGivenUp: a
// session whose merge is given up while its editor is open, and whose
// editor left a swap file beside a file it was given, keeps its directory,
// marked kept, and says what opens again as giving the merge up says it:
// with nothing typed in the files it was given, that nothing there opens
// again, since no session opens a swap file; with something typed in them
// besides, that the next session opens that once an update conflicts the
// same way, and that the swap file does not open again.
func TestSkillResolveSaysWhatAnEditorLeftUnderAnotherNameForAMergeGivenUp(t *testing.T) {
	t.Parallel()
	for _, typed := range []bool{false, true} {
		t.Run(fmt.Sprintf("typed %v", typed), func(t *testing.T) {
			t.Parallel()
			h, _, _ := resolveHarness(t)
			then := `for f in "$@"; do case "$f" in */guide.md) printf 'unsaved typing\n' > "${f%/*}/.guide.md.swp" ;; esac; done` + "\n"
			if typed {
				then += `for f in "$@"; do printf 'resolved in the editor\n' > "$f"; done` + "\n"
			}
			stub, goFile := waitThen(t, then)
			h.env["EDITOR"] = stub
			p := h.start("editor open", "--json", "skill", "resolve", "kit", "--editor")
			p.await()
			h.mustRun("skill", "resolve", "kit", "--abort")

			writeFile(t, goFile, "")
			out := p.wait()
			equal(t, "exit", out.exit, 6)
			dirs := editorDirs(t, h)
			if len(dirs) != 1 {
				t.Fatalf("the session left %v, want the one directory", dirs)
			}
			want := swapWarning(dirs[0])
			if typed {
				want = abortMixedWarning(dirs[0])
			}
			equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), want)
			equal(t, "the owner of what is kept", ownerOf(t, dirs[0]), "kept")
			equal(t, "the swap file", fileBody(t, filepath.Join(dirs[0], "kit", ".guide.md.swp")), "unsaved typing\n")
		})
	}
}

// TestSkillResolveInAnEditorCompletesIntoAMergeDoneAgain: a session that
// resolves the last file completes the merge, which merges the library
// directory again when it was edited meanwhile. When a file the editor
// did not resolve conflicts anew, the run exits 4 with what the editor
// resolved written into the merge, and the directory goes; when the file
// the editor resolved conflicts anew, what was typed for it stays in the
// directory, which a warning names. So does what was typed for a file the
// edit made merge cleanly, git's merge of it written in its place, and
// for one a moved import branch makes merge cleanly, in a run that leaves
// the merge pending with other files to resolve.
//
// A run that merges again and completes the merge, the edit or the moved
// branch leaving no file to resolve, keeps what was typed for such a file
// the same way, a file left with markers in it included: the directory
// stays, its owner file gone, since the merge is complete, and the warning
// says it is the user's to copy from and delete.
func TestSkillResolveInAnEditorCompletesIntoAMergeDoneAgain(t *testing.T) {
	t.Parallel()
	moveGuide := func(t *testing.T, h *harness, base string) {
		h.accountGit("update-ref", lineage.ManagedRef("kit"), versionOf(t, h, base, map[string]string{"guide.md": kitMine["guide.md"]}))
	}
	for _, c := range []struct {
		name      string
		resolved  []string // what --hunk resolves first, beside the files that conflict whole
		meanwhile func(t *testing.T, h *harness, base string)
		args      []string // after --editor
		typing    bool     // the editor types into guide.md and leaves its markers, rather than resolving it
		exit      int
		left      string // the files the conflict event reports, "" when the run completes the merge
		kept      string // the file whose typed content stays, if any
		clean     bool   // and it merges cleanly now, rather than conflicting anew
	}{
		{name: "another file conflicts anew", resolved: []string{"guide.md:1=mine"}, exit: 4, left: "guide.md:1",
			meanwhile: func(t *testing.T, h *harness, _ string) { editLibrary(t, h, "kit", "guide.md", "guide, mine again\n") }},
		{name: "the file edited conflicts anew", resolved: []string{"guide.md:1=mine"}, exit: 4, left: "notes.md:2", kept: "notes.md",
			meanwhile: func(t *testing.T, h *harness, _ string) {
				editLibrary(t, h, "kit", "notes.md", "ONE\ntwo\nthree\nfour\nFIVE again\n")
			}},
		{name: "the file edited merges cleanly", resolved: []string{"guide.md:1=mine"}, exit: 4, left: "guide.md:1", kept: "notes.md", clean: true,
			meanwhile: func(t *testing.T, h *harness, _ string) {
				editLibrary(t, h, "kit", "notes.md", kitTheirs["notes.md"])
				editLibrary(t, h, "kit", "guide.md", "guide, mine again\n")
			}},
		{name: "a file merges cleanly on a moved import branch", args: []string{"guide.md"}, exit: 0, left: "notes.md:2", kept: "guide.md", clean: true,
			meanwhile: moveGuide},
		{name: "the file edited merges cleanly and completes the merge", resolved: []string{"guide.md:1=mine"}, exit: 0, kept: "notes.md", clean: true,
			meanwhile: func(t *testing.T, h *harness, _ string) { editLibrary(t, h, "kit", "notes.md", kitTheirs["notes.md"]) }},
		{name: "a file merges cleanly on a moved import branch and completes the merge", resolved: []string{"notes.md:1=mine", "notes.md:2=theirs"},
			args: []string{"guide.md"}, exit: 0, kept: "guide.md", clean: true, meanwhile: moveGuide},
		{name: "a file left with markers merges cleanly on a moved import branch and completes the merge", typing: true, exit: 0, kept: "guide.md", clean: true,
			meanwhile: moveGuide},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, _, updated := resolveHarness(t)
			resolveKit(t, h, 0, append(slices.Clone(kitWholeFiles), c.resolved...)...)
			c.meanwhile(t, h, updated["base"].(string))
			stub, _, _ := resolvingEditor(t)
			if c.typing {
				stub = editorStub(t, "typing-editor", `for f in "$@"; do
	case "$f" in
	*/guide.md) printf 'typed\n' >> "$f" ;;
	*) printf 'resolved in the editor\n' > "$f" ;;
	esac
done
`)
			}
			h.env["EDITOR"] = stub

			out := h.run(append([]string{"--json", "skill", "resolve", "kit", "--editor"}, c.args...)...)
			equal(t, "exit", out.exit, c.exit)
			until := ""
			if c.left == "" {
				equal(t, "conflict events", len(h.eventsOfType(out.stdout, "conflict")), 0)
				contains(t, "summary", h.one(out.stdout, "result")["summary"].(string), " and updated it from ")
				equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), "")
				until = leftToYouKit
			} else {
				equal(t, "what is left", conflictFiles(h.one(out.stdout, "conflict")), c.left)
			}
			dirs := editorDirs(t, h)
			if c.kept == "" {
				equal(t, "what the session left", strings.Join(dirs, " "), "")
				equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), "")
				equal(t, "notes.md in the merge", h.accountGit("cat-file", "blob", lineage.MergeRef("kit")+":kit-dir/notes.md"), "resolved in the editor")
				contains(t, "the message", pendingMessage(h, "kit"), "resolved \"notes.md\"\n")
				return
			}
			if len(dirs) != 1 {
				t.Fatalf("the session left %v, want the one directory", dirs)
			}
			how := " conflicts anew now that the merge was merged again, so what you typed for it is kept in "
			switch {
			case c.typing:
				how = " still holds conflict markers, and merges cleanly now that the merge was merged again, so git's merge of it was written; what you typed is kept in "
			case c.clean:
				how = " merges cleanly now that the merge was merged again, so what you typed for it was not written and is kept in "
			}
			// The copies, which hold the version before the import branch
			// moved, are kept with warnings of their own when the merge
			// completes on it.
			equal(t, "warnings", strings.Join(notCopyWarnings(h, out.stderr), "\n"), c.kept+how+dirs[0]+until)
			saved := fileBody(t, filepath.Join(dirs[0], "kit", c.kept))
			if c.typing {
				if !strings.HasPrefix(saved, "<<<<<<< mine\n") || !strings.HasSuffix(saved, ">>>>>>> theirs\ntyped\n") {
					t.Errorf("what the editor saved is not what it typed after the markers:\n%s", saved)
				}
			} else {
				equal(t, "what the editor saved", saved, "resolved in the editor\n")
			}
			if c.left != "" {
				if c.clean {
					equal(t, c.kept+" in the merge, as git merged it", h.accountGit("cat-file", "blob", lineage.MergeRef("kit")+":kit-dir/"+c.kept),
						strings.TrimSpace(kitTheirs[c.kept]))
				}
				equal(t, "the owner of what is kept", ownerOf(t, dirs[0]), "kept")
				return
			}
			equal(t, c.kept+" in the library, as git merged it", fileBody(t, filepath.Join(h.library, "kit", c.kept)), kitTheirs[c.kept])
			nothingAt(t, "the owner file of what is kept", filepath.Join(dirs[0], editorOwner))
			if c.typing {
				equal(t, "notes.md in the library", fileBody(t, filepath.Join(h.library, "kit", "notes.md")), "resolved in the editor\n")
			}
		})
	}
}

// TestSkillResolveSaysWhatWasTypedForAConflictAMovedBranchChanged: a
// session that writes a file on a moved import branch merges the merge
// again, and a file it left with markers and something typed may conflict
// otherwise in the merge merged again. Then no session of the merge
// pending opens what was typed for it, and the warning that names the
// directory kept for it says so rather than promise it; the next session
// opens the file afresh and names the directory as one it did not open.
// Where the branch moved to another commit of the same version, the file
// conflicts as it did, and the next session opens what was typed, as the
// warning says.
func TestSkillResolveSaysWhatWasTypedForAConflictAMovedBranchChanged(t *testing.T) {
	t.Parallel()
	for _, changed := range []bool{true, false} {
		t.Run(fmt.Sprintf("its conflict changed %v", changed), func(t *testing.T) {
			t.Parallel()
			h, _, updated := resolveHarness(t)
			resolveKit(t, h, 0, kitWholeFiles...)
			moves := map[string]string(nil)
			if changed {
				moves = map[string]string{"notes.md": "one\ntwo\nthree\nfour\ncinco\n"}
			}
			h.accountGit("update-ref", lineage.ManagedRef("kit"), versionOf(t, h, updated["base"].(string), moves))
			h.env["EDITOR"] = editorStub(t, "typing-editor", `for f in "$@"; do
	case "$f" in
	*/guide.md) printf 'resolved in the editor\n' > "$f" ;;
	*) printf 'MY TYPING\n' >> "$f" ;;
	esac
done
`)

			out := h.run("--json", "skill", "resolve", "kit", "--editor")
			dirs := editorDirs(t, h)
			if len(dirs) != 1 {
				t.Fatalf("the session left %v, want the one directory", dirs)
			}
			typed := kitText["notes.md"] + "MY TYPING\n"
			equal(t, "what the editor saved, kept", fileBody(t, filepath.Join(dirs[0], "kit", "notes.md")), typed)
			equal(t, "the owner of what is kept", ownerOf(t, dirs[0]), "kept")
			equal(t, "guide.md in the merge", inMerge(h, "guide.md"), "resolved in the editor")
			kept := "notes.md still holds conflict markers, so it was left unresolved; what you typed is kept in " + dirs[0]
			stub, seen := seeingEditor(t, "exit 0\n")
			h.env["EDITOR"] = stub
			if !changed {
				equal(t, "exit", out.exit, 0)
				equal(t, "what is left", conflictFiles(h.one(out.stdout, "conflict")), "notes.md:2")
				equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), kept+opensAgainKit("it"))

				out = h.run("--json", "skill", "resolve", "kit", "--editor")
				equal(t, "exit of the next session", out.exit, 0)
				equal(t, "what the next session opened", fileBody(t, filepath.Join(seen, "notes.md")), typed)
				equal(t, "the note", strings.Join(infos(h, out.stderr), "\n"), reopenedNote("notes.md", dirs[0]))
				return
			}
			equal(t, "exit", out.exit, 4)
			equal(t, "code", h.one(out.stdout, "error")["code"], "pending_merge")
			equal(t, "what conflicts anew", conflictFiles(h.one(out.stdout, "conflict")), "notes.md:1")
			equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"),
				kept+", but it does not open again, because its conflict changed when the merge was merged again, so copy what you need from it")

			out = h.run("--json", "skill", "resolve", "kit", "--editor")
			equal(t, "exit of the next session", out.exit, 0)
			equal(t, "what the next session opened, afresh for the conflict as it is now", fileBody(t, filepath.Join(seen, "notes.md")),
				"<<<<<<< mine\nONE\n||||||| base\none\n=======\nuno\n>>>>>>> theirs\ntwo\nthree\nfour\nFIVE\n")
			equal(t, "the notes of the next session", strings.Join(infos(h, out.stderr), "\n"), "")
			equal(t, "the warnings of the next session", strings.Join(warnings(h, out.stderr), "\n"),
				notOpenedWarning(dirs[0])+"\nnotes.md still holds conflict markers, so it was left unresolved")
			equal(t, "what was typed, still kept", fileBody(t, filepath.Join(dirs[0], "kit", "notes.md")), typed)
		})
	}
}

// TestSkillResolvePrunesWhatAnEditorSessionLeftBehind: a directory an
// editor session of the skill left for this home, whose run is gone without
// keeping it, killed or hung up on say, is removed when the next session
// starts, as long as it holds nothing but the files as they were written,
// one of them deleted included, and so is one that holds nothing but its
// owner file, a run killed before it made the skill's directory, the owner
// file a run killed as it kept it wrote anew beside it or not, and one its
// run kept that holds nothing typed; one whose run is still going, one
// that run is still laying out, its files not yet in place, one of another
// skill, and one whose run is gone that holds a file beside its owner file
// and no directory of the skill stay. One whose run is gone and that holds
// anything else the session does not open again, a file saved for a
// conflict that is not the one the session opens, a swap file an editor
// left beside one, a file or a folder saved beside the skill's directory,
// or an owner file that records no file, is kept, marked so, and a warning
// names it, and the session after that names it again, as every session
// names every directory it keeps. Completing the merge with --hunk gives
// the ones kept up to the user, their owner files gone, and names each,
// and leaves the ones whose run is still going and the one that holds a
// file beside its owner file as they are.
func TestSkillResolvePrunesWhatAnEditorSessionLeftBehind(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	pid := goneProcess(t)
	stale := editorDir(t, h, "kit", pid, h.agentx)
	unwritten := editorDir(t, h, "kit", pid, h.agentx)
	remove(t, filepath.Join(unwritten, "kit", "notes.md"))
	ownerOnly := editorDir(t, h, "kit", pid, h.agentx)
	remove(t, filepath.Join(ownerOnly, "kit"))
	ownerBeingKept := editorDir(t, h, "kit", pid, h.agentx)
	remove(t, filepath.Join(ownerBeingKept, "kit"))
	writeFile(t, filepath.Join(ownerBeingKept, editorOwner+".123"), editorKept+"\n")
	beside := editorDir(t, h, "kit", pid, h.agentx)
	remove(t, filepath.Join(beside, "kit"))
	writeFile(t, filepath.Join(beside, "notes.md"), "saved beside the owner file\n")
	live := editorDir(t, h, "kit", ownPid, h.agentx)
	laying := editorDir(t, h, "kit", ownPid, h.agentx)
	if err := os.Rename(filepath.Join(laying, "kit"), filepath.Join(laying, editorLayingOut)); err != nil {
		t.Fatal(err)
	}
	kept := editorDir(t, h, "kit", "kept", h.agentx)
	other := editorDir(t, h, "other", pid, h.agentx)
	saved := editorDir(t, h, "kit", pid, h.agentx)
	writeFile(t, filepath.Join(saved, "kit", "notes.md"), "saved before the hang-up\n")
	swapped := editorDir(t, h, "kit", pid, h.agentx)
	writeFile(t, filepath.Join(swapped, "kit", ".notes.md.swp"), "typed, not saved\n")
	unrecorded := editorDir(t, h, "kit", pid, h.agentx)
	writeFile(t, filepath.Join(unrecorded, editorOwner), pid+"\n"+h.agentx+"\n"+h.ref(lineage.MergeRef("kit"))+"\n")
	// Saved one folder up from the file the editor was given, beside the
	// skill's directory, whose files are as they were written.
	drafted := editorDir(t, h, "kit", pid, h.agentx)
	writeFile(t, filepath.Join(drafted, "guide.draft.md"), "MY DRAFT\n")
	drafts := editorDir(t, h, "kit", pid, h.agentx)
	writeFile(t, mkdirs(t, filepath.Join(drafts, "drafts"), "x.md"), "MY DRAFTS\n")
	h.env["EDITOR"] = editorStub(t, "idle-editor", "exit 0\n")

	out := h.run("--json", "skill", "resolve", "kit", "--editor")
	equal(t, "exit", out.exit, 0)
	nothingAt(t, "the stale session", stale)
	nothingAt(t, "the session one of whose files was deleted", unwritten)
	nothingAt(t, "the session killed before it made the skill's directory", ownerOnly)
	nothingAt(t, "the same, with the owner file being kept beside it", ownerBeingKept)
	nothingAt(t, "the session kept with nothing typed in it", kept)
	equal(t, "what is left", strings.Join(editorDirs(t, h), " "),
		strings.Join(sortedStrings(live, laying, other, beside, saved, swapped, unrecorded, drafted, drafts), " "))
	equal(t, "the owner of the file saved beside the owner file", ownerOf(t, beside), pid)
	var want []string
	for _, dir := range sortedStrings(saved, swapped, unrecorded, drafted, drafts) {
		equal(t, "the owner of "+dir, ownerOf(t, dir), "kept")
		want = append(want, notOpenedWarning(dir))
	}
	// Each file the idle editor left as it was is named in a warning of
	// its own after those.
	got := warnings(h, out.stderr)
	equal(t, "warnings", strings.Join(got[:min(len(got), len(want))], "\n"), strings.Join(want, "\n"))
	equal(t, "notes", strings.Join(infos(h, out.stderr), "\n"), "")
	equal(t, "what was saved", fileBody(t, filepath.Join(saved, "kit", "notes.md")), "saved before the hang-up\n")
	equal(t, "the swap file", fileBody(t, filepath.Join(swapped, "kit", ".notes.md.swp")), "typed, not saved\n")
	equal(t, "the draft beside the skill's directory", fileBody(t, filepath.Join(drafted, "guide.draft.md")), "MY DRAFT\n")
	equal(t, "the folder of drafts beside it", fileBody(t, filepath.Join(drafts, "drafts", "x.md")), "MY DRAFTS\n")

	out = h.run("--json", "skill", "resolve", "kit", "--editor")
	equal(t, "exit of the session after", out.exit, 0)
	got = warnings(h, out.stderr)
	equal(t, "warnings of the session after", strings.Join(got[:min(len(got), len(want))], "\n"), strings.Join(want, "\n"))
	equal(t, "what is left after the session after", strings.Join(editorDirs(t, h), " "),
		strings.Join(sortedStrings(live, laying, other, beside, saved, swapped, unrecorded, drafted, drafts), " "))

	out = resolveKit(t, h, 0, append(kitSides, "kept.md:1=theirs")...)
	want = nil
	for _, dir := range sortedStrings(saved, swapped, unrecorded, drafted, drafts) {
		nothingAt(t, "the owner file of "+dir, filepath.Join(dir, editorOwner))
		want = append(want, leftWarning(dir))
	}
	equal(t, "warnings of the completion", strings.Join(notCopyWarnings(h, out.stderr), "\n"), strings.Join(want, "\n"))
	equal(t, "what is left once the merge completed", strings.Join(editorDirs(t, h), " "),
		strings.Join(sortedStrings(live, laying, other, beside, saved, swapped, unrecorded, drafted, drafts), " "))
	equal(t, "what was saved, left", fileBody(t, filepath.Join(saved, "kit", "notes.md")), "saved before the hang-up\n")
	equal(t, "the swap file, left", fileBody(t, filepath.Join(swapped, "kit", ".notes.md.swp")), "typed, not saved\n")
	equal(t, "what was saved beside the owner file", fileBody(t, filepath.Join(beside, "notes.md")), "saved beside the owner file\n")
	equal(t, "the draft beside the skill's directory, left", fileBody(t, filepath.Join(drafted, "guide.draft.md")), "MY DRAFT\n")
	equal(t, "the folder of drafts beside it, left", fileBody(t, filepath.Join(drafts, "drafts", "x.md")), "MY DRAFTS\n")
}

// goneProcess is the process id of a process that has exited, as the
// owner file of a session whose run was killed names it.
func goneProcess(t *testing.T) string {
	t.Helper()
	gone := exec.Command("true")
	if err := gone.Run(); err != nil {
		t.Fatal(err)
	}
	return strconv.Itoa(gone.Process.Pid)
}

// TestSkillResolveCompletingLeavesWhatWasTypedToYou: completing the merge
// never removes a directory of an earlier editor session that holds
// something typed, whichever run completes it, an editor session or
// --hunk, and whether that directory's run is gone, hung up on say, or it
// was kept: each is given up to the user as the merge completes, its owner
// file gone, so that no later run touches it, and a warning names it,
// after the one an editor session gives of it as it starts. A directory
// that holds the files as they were written goes, whether its run is gone
// or kept it.
func TestSkillResolveCompletingLeavesWhatWasTypedToYou(t *testing.T) {
	t.Parallel()
	for _, editor := range []bool{true, false} {
		for _, source := range []string{"gone", "kept"} {
			t.Run(fmt.Sprintf("in an editor %v, its run %s", editor, source), func(t *testing.T) {
				t.Parallel()
				h, _, _ := resolveHarness(t)
				resolveKit(t, h, 0, kitWholeFiles...)
				pid := source
				if source == "gone" {
					pid = goneProcess(t)
				}
				saved := editorDir(t, h, "kit", pid, h.agentx)
				writeFile(t, filepath.Join(saved, "kit", "notes.md"), "saved before the hang-up\n")
				stale := editorDir(t, h, "kit", goneProcess(t), h.agentx)
				kept := editorDir(t, h, "kit", "kept", h.agentx)

				var out outcome
				var want []string
				if editor {
					stub, _, _ := resolvingEditor(t)
					h.env["EDITOR"] = stub
					out = h.run("--json", "skill", "resolve", "kit", "--editor")
					equal(t, "exit", out.exit, 0)
					want = append(want, notOpenedWarning(saved))
				} else {
					out = resolveKit(t, h, 0, "notes.md:1=mine", "notes.md:2=theirs", "guide.md:1=both")
				}
				want = append(want, leftWarning(saved))
				contains(t, "summary", h.one(out.stdout, "result")["summary"].(string), "resolved kit and updated it from ")
				equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), "")
				equal(t, "warnings", strings.Join(notCopyWarnings(h, out.stderr), "\n"), strings.Join(want, "\n"))
				nothingAt(t, "the stale session", stale)
				nothingAt(t, "the session its run kept with nothing typed", kept)
				equal(t, "what the sessions left", strings.Join(editorDirs(t, h), " "), saved)
				equal(t, "what was saved", fileBody(t, filepath.Join(saved, "kit", "notes.md")), "saved before the hang-up\n")
				nothingAt(t, "the owner file of what was saved", filepath.Join(saved, editorOwner))
			})
		}
	}
}

// TestSkillResolveAbortKeepsWhatAHungUpSessionSaved: giving the merge up
// keeps a directory an editor session left whose run is gone, hung up on
// say, with a file saved in it, marked kept, and a warning names it, in
// text and with --json, and says that the next editor session of the same
// conflict opens it again once an update conflicts the same way. One that
// holds a file or a folder saved beside the skill's directory, its files
// as they were written, is kept too, and its warning says nothing there
// opens again. Giving a later merge up names them again, as it names every
// directory that holds something typed; completing a later merge with
// --hunk, which opens nothing, gives them up to the user and names each.
// A directory whose run is gone and that holds the files as they were
// written goes, and so does one that holds nothing but its owner file, a
// run killed before it made the skill's directory.
func TestSkillResolveAbortKeepsWhatAHungUpSessionSaved(t *testing.T) {
	t.Parallel()
	for _, json := range []bool{true, false} {
		t.Run(fmt.Sprintf("json %v", json), func(t *testing.T) {
			t.Parallel()
			h, _, _ := resolveHarness(t)
			pid := goneProcess(t)
			saved := typedDir(t, h, pid, map[string]string{"notes.md": "saved before the hang-up\n"})
			stale := editorDir(t, h, "kit", pid, h.agentx)
			ownerOnly := editorDir(t, h, "kit", pid, h.agentx)
			remove(t, filepath.Join(ownerOnly, "kit"))
			drafted := editorDir(t, h, "kit", pid, h.agentx)
			writeFile(t, filepath.Join(drafted, "guide.draft.md"), "MY DRAFT\n")
			drafts := editorDir(t, h, "kit", pid, h.agentx)
			writeFile(t, mkdirs(t, filepath.Join(drafts, "drafts"), "x.md"), "MY DRAFTS\n")
			library := onDisk(t, h.library)
			left := sortedStrings(saved, drafted, drafts)
			var aborted, completed []string
			for _, dir := range left {
				if dir == saved {
					aborted = append(aborted, abortWarning(dir))
				} else {
					aborted = append(aborted, swapWarning(dir))
				}
				completed = append(completed, leftWarning(dir))
			}

			args := []string{"skill", "resolve", "kit", "--abort"}
			if json {
				args = append([]string{"--json"}, args...)
			}
			out := h.run(args...)
			equal(t, "exit", out.exit, 0)
			if json {
				equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), strings.Join(aborted, "\n"))
				equal(t, "summary", h.one(out.stdout, "result")["summary"], "gave up the merge of kit; the library directory is as it was")
			} else {
				equal(t, "stderr", out.stderr, "warning: "+strings.Join(aborted, "\nwarning: ")+"\n")
				contains(t, "stdout", out.stdout, "gave up the merge of kit; the library directory is as it was")
			}
			equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), "")
			equal(t, "the library", onDisk(t, h.library), library)
			equal(t, "what the sessions left", strings.Join(editorDirs(t, h), " "), strings.Join(left, " "))
			equal(t, "what was saved", fileBody(t, filepath.Join(saved, "kit", "notes.md")), "saved before the hang-up\n")
			for _, dir := range left {
				equal(t, "the owner of "+dir, ownerOf(t, dir), "kept")
			}
			nothingAt(t, "the stale session", stale)
			nothingAt(t, "the session killed before it made the skill's directory", ownerOnly)

			equal(t, "the update that conflicts again", h.run("--json", "skill", "update", "kit").exit, 4)
			out = h.run("--json", "skill", "resolve", "kit", "--abort")
			equal(t, "exit of the later abort", out.exit, 0)
			equal(t, "warnings of the later abort", strings.Join(warnings(h, out.stderr), "\n"), strings.Join(aborted, "\n"))
			equal(t, "what the sessions left after the later abort", strings.Join(editorDirs(t, h), " "), strings.Join(left, " "))

			equal(t, "the update that conflicts once more", h.run("--json", "skill", "update", "kit").exit, 4)
			out = resolveKit(t, h, 0, append(kitSides, "kept.md:1=theirs")...)
			contains(t, "summary of the later completion", h.one(out.stdout, "result")["summary"].(string), "resolved kit and updated it from ")
			equal(t, "warnings of the later completion", strings.Join(notCopyWarnings(h, out.stderr), "\n"), strings.Join(completed, "\n"))
			equal(t, "what the sessions left once a merge completed", strings.Join(editorDirs(t, h), " "), strings.Join(left, " "))
			equal(t, "what was saved, left", fileBody(t, filepath.Join(saved, "kit", "notes.md")), "saved before the hang-up\n")
			equal(t, "the draft beside the skill's directory, left", fileBody(t, filepath.Join(drafted, "guide.draft.md")), "MY DRAFT\n")
			equal(t, "the folder of drafts beside it, left", fileBody(t, filepath.Join(drafts, "drafts", "x.md")), "MY DRAFTS\n")
			for _, dir := range left {
				nothingAt(t, "the owner file of "+dir, filepath.Join(dir, editorOwner))
			}
		})
	}
}

// TestSkillResolveReopensWhatARealSessionLeft checks the owner file a
// session writes against the checks a later one makes of it: an editor
// that copies the directory of its own session aside twice, once as it is
// and once with a byte of a file changed, stands in for two sessions whose
// run was killed while the editor was open, their owner files naming a
// process that has exited. The files sit in a nested folder, under a name
// git quotes. The next session removes the copy that holds the files as
// they were written, opens the file changed as the other copy holds it,
// markers and all, and removes that copy too once its own directory holds
// what was typed; left with markers, what was typed is kept there.
func TestSkillResolveReopensWhatARealSessionLeft(t *testing.T) {
	t.Parallel()
	nested := "docs/deep dir/say \"hi\"\tnow.md"
	h := clashHarness(t, map[string]string{nested: "one\n"},
		func(s *sourceRepo) { s.write("skills/kit-dir/"+nested, "uno\n") },
		func(lib string) { writeFile(t, filepath.Join(lib, filepath.FromSlash(nested)), "ONE\n") })
	equal(t, "the conflicts", conflictFiles(h.one(h.mustRun("--json", "skill", "resolve", "kit").stdout, "conflict")), nested+":1,notes.md:1")
	cp, err := exec.LookPath("cp")
	if err != nil {
		t.Fatal(err)
	}
	tmp := h.env["TMPDIR"]
	clean, edited := filepath.Join(tmp, "agentx-resolve-clean"), filepath.Join(tmp, "agentx-resolve-edited")
	h.env["EDITOR"] = editorStub(t, "copying-editor", `rest=${1#"$TMPDIR"/}
session="$TMPDIR/${rest%%/*}"
`+cp+` -R "$session" `+shellWord(clean)+`
`+cp+` -R "$session" `+shellWord(edited)+`
`)
	if out := h.run("--json", "skill", "resolve", "kit", "--editor"); out.exit != 0 {
		t.Fatalf("the session that copies its files: exit %d\n%s%s", out.exit, out.stdout, out.stderr)
	}
	equal(t, "what the sessions left", strings.Join(editorDirs(t, h), " "), strings.Join(sortedStrings(clean, edited), " "))
	pid := goneProcess(t)
	for _, dir := range []string{clean, edited} {
		owner := fileBody(t, filepath.Join(dir, editorOwner))
		first, rest, _ := strings.Cut(owner, "\n")
		equal(t, "the owner of "+dir, first, strconv.Itoa(os.Getpid()))
		contains(t, "the owner file of "+dir, rest, gitQuoted(nested))
		writeFile(t, filepath.Join(dir, editorOwner), pid+"\n"+rest)
	}
	file := filepath.Join(edited, "kit", filepath.FromSlash(nested))
	body := fileBody(t, file)
	writeFile(t, file, strings.Replace(body, "ONE", "One", 1))
	h.env["EDITOR"] = editorStub(t, "idle-editor", "exit 0\n")

	out := h.run("--json", "skill", "resolve", "kit", "--editor")
	equal(t, "exit", out.exit, 0)
	nothingAt(t, "the copy as it was written", clean)
	nothingAt(t, "the edited copy", edited)
	dirs := editorDirs(t, h)
	if len(dirs) != 1 {
		t.Fatalf("the session left %v, want the one directory", dirs)
	}
	equal(t, "the owner of what is kept", ownerOf(t, dirs[0]), "kept")
	equal(t, "what was edited, kept", fileBody(t, filepath.Join(dirs[0], "kit", filepath.FromSlash(nested))), strings.Replace(body, "ONE", "One", 1))
	equal(t, "the note", strings.Join(infos(h, out.stderr), "\n"), reopenedNote(quotedPath(nested), edited))
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"),
		quotedPath(nested)+" still holds conflict markers, so it was left unresolved; what you typed is kept in "+dirs[0]+opensAgainKit("it")+"\n"+
			"notes.md still holds conflict markers, so it was left unresolved")
}

// hungUpSession runs an editor session of kit in a process of its own,
// whose editor runs typing, shell, on the files it is given and then kills
// that process with SIGKILL, as a terminal closed or a connection dropped
// ends a run while its editor is open. It returns the directory the
// session left, whose owner file names that process, now gone.
func hungUpSession(t *testing.T, h *harness, typing string) string {
	t.Helper()
	return killedSession(t, h, editorStub(t, "hanging-up-editor", `rest=${1#"$TMPDIR"/}
session="$TMPDIR/${rest%%/*}"
`+typing+`
IFS= read -r pid < "$session/`+editorOwner+`"
kill -9 "$pid"
`))
}

// killedSession runs an editor session of kit in a process of its own,
// whose editor is stub and whose environment also holds env, and which is
// killed with SIGKILL before it ends, by its editor or at the step of
// laying out the files, or of keeping the directory, resolveKillAtEnv
// names. It returns the directory
// the session left, whose owner file names that process, now gone.
func killedSession(t *testing.T, h *harness, stub string, env ...string) string {
	t.Helper()
	before := editorDirs(t, h)
	child := exec.Command(os.Args[0], "-test.run=^TestResolveChildProcess$")
	child.Env = append(os.Environ(), resolveChildEnv+"=kit|--editor")
	for k, v := range h.env {
		child.Env = append(child.Env, k+"="+v)
	}
	// GIT_EDITOR names the editor, whatever the test's own environment
	// names, and PATH holds git alone, as TestSkillResolveLeavesACtrlCToTheEditor
	// says.
	child.Env = append(append(child.Env, "PATH="+gitOnlyPath(t), "GIT_EDITOR="+stub), env...)
	out, _ := child.CombinedOutput()
	if status, ok := child.ProcessState.Sys().(syscall.WaitStatus); !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatalf("the session was not killed, it ended %v:\n%s", child.ProcessState, out)
	}
	var left []string
	for _, dir := range editorDirs(t, h) {
		if !slices.Contains(before, dir) {
			left = append(left, dir)
		}
	}
	if len(left) != 1 {
		t.Fatalf("the session left %v, want the one directory:\n%s", left, out)
	}
	if pid, err := strconv.Atoi(ownerOf(t, left[0])); err != nil || running(pid) {
		t.Fatalf("the owner of what the session left is %q, want the process killed", ownerOf(t, left[0]))
	}
	return left[0]
}

// TestSkillResolveReopensWhatAHungUpSessionTyped: a session whose run is
// killed while its editor is open, as a closed terminal or a dropped
// connection kills it, leaves what the editor saved in its directory, which
// no warning named. The next session opens it in place of the file afresh,
// and a note says so before its editor starts; with no marker left in it,
// it is written, the directory it came from goes, and nothing is left in
// the temporary directory.
func TestSkillResolveReopensWhatAHungUpSessionTyped(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	left := hungUpSession(t, h, `for f in "$@"; do case "$f" in */guide.md) printf 'typed before the hang-up\n' > "$f" ;; esac; done`)
	equal(t, "what the editor saved", fileBody(t, filepath.Join(left, "kit", "guide.md")), "typed before the hang-up\n")

	stub, seen := seeingEditor(t, "exit 0\n")
	h.env["EDITOR"] = stub
	out := h.run("--json", "skill", "resolve", "kit", "--editor")
	equal(t, "exit", out.exit, 0)
	equal(t, "summary", h.one(out.stdout, "result")["summary"], "resolved guide.md in the merge of kit, 4 files left to resolve")
	equal(t, "what the editor opened", fileBody(t, filepath.Join(seen, "guide.md")), "typed before the hang-up\n")
	equal(t, "what the editor opened afresh", fileBody(t, filepath.Join(seen, "notes.md")), kitText["notes.md"])
	equal(t, "the note", strings.Join(infos(h, out.stderr), "\n"), reopenedNote("guide.md", left))
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), "notes.md still holds conflict markers, so it was left unresolved")
	equal(t, "guide.md in the merge", inMerge(h, "guide.md"), "typed before the hang-up")
	equal(t, "what is left", strings.Join(editorDirs(t, h), " "), "")
}

// TestSkillResolveReopensWhatWasTypedOnceTheMergeIsGivenUp: giving the
// merge up keeps the directory of a session that typed in a file and left
// markers in it, and a warning names it. An update that conflicts the same
// way leaves the merge pending again, and the next session opens what was
// typed there for the file left with markers, and removes the directory.
// What the session wrote into the merge left the directory then, and was
// given up with the merge, as a --hunk choice is: that file opens afresh.
func TestSkillResolveReopensWhatWasTypedOnceTheMergeIsGivenUp(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	h.env["EDITOR"] = editorStub(t, "careful-editor", `for f in "$@"; do
	case "$f" in
	*/guide.md) printf 'resolved in the editor\n' > "$f" ;;
	*) printf 'MY CAREFUL WORK\n' >> "$f" ;;
	esac
done
`)
	h.mustRun("skill", "resolve", "kit", "--editor")
	dirs := editorDirs(t, h)
	if len(dirs) != 1 {
		t.Fatalf("the session left %v, want the one directory", dirs)
	}

	out := h.run("--json", "skill", "resolve", "kit", "--abort")
	equal(t, "exit of the abort", out.exit, 0)
	equal(t, "warnings of the abort", strings.Join(warnings(h, out.stderr), "\n"), abortWarning(dirs[0]))
	equal(t, "what is left once the merge is given up", strings.Join(editorDirs(t, h), " "), dirs[0])
	equal(t, "the owner of what is kept", ownerOf(t, dirs[0]), "kept")

	equal(t, "the update that conflicts the same way", h.run("skill", "update", "kit").exit, 4)
	stub, seen := seeingEditor(t, "exit 0\n")
	h.env["EDITOR"] = stub
	out = h.run("--json", "skill", "resolve", "kit", "--editor")
	equal(t, "exit", out.exit, 0)
	equal(t, "summary", h.one(out.stdout, "result")["summary"], "resolved nothing in the merge of kit: 5 files left to resolve")
	equal(t, "guide.md as the editor opened it", fileBody(t, filepath.Join(seen, "guide.md")), kitText["guide.md"])
	equal(t, "notes.md as the editor opened it", fileBody(t, filepath.Join(seen, "notes.md")), kitText["notes.md"]+"MY CAREFUL WORK\n")
	equal(t, "notes", strings.Join(infos(h, out.stderr), "\n"), reopenedNote("notes.md", dirs[0]))
	nothingAt(t, "the directory kept when the merge was given up", dirs[0])
	now := editorDirs(t, h)
	if len(now) != 1 {
		t.Fatalf("the session left %v, want the one directory", now)
	}
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"),
		"guide.md still holds conflict markers, so it was left unresolved\n"+
			"notes.md still holds conflict markers, so it was left unresolved; what you typed is kept in "+now[0]+opensAgainKit("it"))
	equal(t, "what is kept", fileBody(t, filepath.Join(now[0], "kit", "notes.md")), kitText["notes.md"]+"MY CAREFUL WORK\n")
}

// TestSkillResolveOpensAfreshAFileWhoseConflictChanged: what was typed in
// an earlier session is opened again only for the conflict it was typed
// for. Once the library directory is edited and the merge merged again, the
// file conflicts otherwise, the next session opens it afresh, and the
// directory of the earlier session, its run gone, is kept, marked so, and
// named in a warning, which the session after gives again, since what was
// typed there is still not opened.
func TestSkillResolveOpensAfreshAFileWhoseConflictChanged(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	earlier := typedDir(t, h, goneProcess(t), map[string]string{"notes.md": "typed for the conflict as it was\n"})
	editLibrary(t, h, "kit", "notes.md", "ONE\ntwo\nthree\nfour\nFIVE again\n")
	resolveKit(t, h, 4, append(slices.Clone(kitWholeFiles), "guide.md:1=mine", "notes.md:1=mine", "notes.md:2=mine")...)

	stub, seen := seeingEditor(t, "exit 0\n")
	h.env["EDITOR"] = stub
	out := h.run("--json", "skill", "resolve", "kit", "--editor")
	equal(t, "exit", out.exit, 0)
	contains(t, "what the editor opened", fileBody(t, filepath.Join(seen, "notes.md")), "FIVE again\n")
	equal(t, "notes", strings.Join(infos(h, out.stderr), "\n"), "")
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"),
		notOpenedWarning(earlier)+"\nnotes.md still holds conflict markers, so it was left unresolved")
	equal(t, "the owner of what is kept", ownerOf(t, earlier), "kept")
	equal(t, "what was typed", fileBody(t, filepath.Join(earlier, "kit", "notes.md")), "typed for the conflict as it was\n")

	out = h.run("--json", "skill", "resolve", "kit", "--editor")
	equal(t, "exit of the session after", out.exit, 0)
	equal(t, "warnings of the session after", strings.Join(warnings(h, out.stderr), "\n"),
		notOpenedWarning(earlier)+"\nnotes.md still holds conflict markers, so it was left unresolved")
	equal(t, "what is left", strings.Join(editorDirs(t, h), " "), earlier)

	// A directory whose notes.md holds just what a session lays out now,
	// but for the conflict as it was, holds something typed for that
	// conflict: it is no copy of what the session laid out, so it stays,
	// and the directory is kept and named.
	fresh := fileBody(t, filepath.Join(seen, "notes.md"))
	older := typedDir(t, h, goneProcess(t), map[string]string{"notes.md": fresh})
	out = h.run("--json", "skill", "resolve", "kit", "--editor")
	equal(t, "exit of the session with it", out.exit, 0)
	named := sortedStrings(earlier, older)
	equal(t, "warnings of the session with it", strings.Join(warnings(h, out.stderr), "\n"),
		notOpenedWarning(named[0])+"\n"+notOpenedWarning(named[1])+"\nnotes.md still holds conflict markers, so it was left unresolved")
	equal(t, "the owner of it", ownerOf(t, older), "kept")
	equal(t, "what it holds", fileBody(t, filepath.Join(older, "kit", "notes.md")), fresh)
}

// TestSkillResolveNamesEveryFolderItDoesNotOpen: every editor session
// names, as it starts and before its editor takes the terminal, each
// directory an earlier session of the skill left that holds something
// typed it does not open, in one warning per directory, whether that
// directory was kept by its own run, by an earlier session that found its
// run gone or by giving the merge up, and however many runs named it
// before. Here two directories each hold a file typed for a conflict that
// changed and a swap file, one kept and one whose run is gone, through two
// sessions, a merge given up and a session once an update conflicts the
// same way: each session names both, and neither is removed meanwhile.
func TestSkillResolveNamesEveryFolderItDoesNotOpen(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	var dirs []string
	for _, pid := range []string{"kept", goneProcess(t)} {
		dir := typedDir(t, h, pid, map[string]string{"notes.md": "typed for the conflict as it was\n"})
		owner := filepath.Join(dir, editorOwner)
		// notes.md was typed for a conflict that has changed since.
		writeFile(t, owner, strings.Replace(fileBody(t, owner), editorRecord("notes.md", []byte(kitText["notes.md"])), editorRecord("notes.md", []byte("an older conflict\n")), 1))
		writeFile(t, filepath.Join(dir, "kit", ".notes.md.swp"), "unsaved typing\n")
		dirs = append(dirs, dir)
	}
	slices.Sort(dirs)
	h.env["EDITOR"] = editorStub(t, "idle-editor", "echo \"editor open\" >&2\nexit 0\n")
	session := func(what string) {
		t.Helper()
		out := h.run("skill", "resolve", "kit", "--editor")
		equal(t, "exit of "+what, out.exit, 0)
		equal(t, "stderr of "+what, out.stderr, "warning: "+notOpenedWarning(dirs[0])+"\nwarning: "+notOpenedWarning(dirs[1])+
			"\neditor open\nwarning: guide.md still holds conflict markers, so it was left unresolved\nwarning: notes.md still holds conflict markers, so it was left unresolved\n")
		equal(t, "what is left after "+what, strings.Join(editorDirs(t, h), " "), strings.Join(dirs, " "))
		for _, dir := range dirs {
			equal(t, "the owner of "+dir+" after "+what, ownerOf(t, dir), "kept")
			equal(t, "what was typed in "+dir+" after "+what, fileBody(t, filepath.Join(dir, "kit", "notes.md")), "typed for the conflict as it was\n")
			equal(t, "the swap file in "+dir+" after "+what, fileBody(t, filepath.Join(dir, "kit", ".notes.md.swp")), "unsaved typing\n")
		}
	}
	session("the first session")
	session("the session after")
	h.mustRun("skill", "resolve", "kit", "--abort")
	equal(t, "the update that conflicts the same way", h.run("skill", "update", "kit").exit, 4)
	session("the session once the merge is pending anew")
}

// TestSkillResolveReopensTheNewestOfWhatWasTyped: where two earlier
// sessions typed in the same file for the same conflict, the one saved last
// is opened, whichever of the two directories' names sorts first, and the
// one whose name sorts last where both were saved at once; the directory it
// came from goes, and the other, whose run is gone, is kept and named in a
// warning, what was typed there left as it is. The note that names where
// the file comes from, and that warning, come before the editor starts, in
// text and with --json.
func TestSkillResolveReopensTheNewestOfWhatWasTyped(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name                          string
		newerSortsFirst, atOnce, text bool
	}{
		{name: "the newer sorts first", newerSortsFirst: true},
		{name: "the newer sorts last"},
		{name: "both saved at once", atOnce: true},
		{name: "in text", newerSortsFirst: true, text: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, _, _ := resolveHarness(t)
			pid := goneProcess(t)
			older := typedDir(t, h, pid, map[string]string{"guide.md": "typed first\n"})
			newer := typedDir(t, h, pid, map[string]string{"guide.md": "typed last\n"})
			if (newer < older) != c.newerSortsFirst {
				older, newer = newer, older
				writeFile(t, filepath.Join(older, "kit", "guide.md"), "typed first\n")
				writeFile(t, filepath.Join(newer, "kit", "guide.md"), "typed last\n")
			}
			saved := map[string]time.Time{older: time.Now().Add(-2 * time.Hour), newer: time.Now().Add(-time.Hour)}
			if c.atOnce {
				saved[older] = saved[newer]
			}
			for dir, at := range saved {
				if err := os.Chtimes(filepath.Join(dir, "kit", "guide.md"), at, at); err != nil {
					t.Fatal(err)
				}
			}

			stub, seen := seeingEditor(t, "echo \"editor open\" >&2\nexit 0\n")
			h.env["EDITOR"] = stub
			if c.text {
				out := h.run("skill", "resolve", "kit", "--editor")
				equal(t, "exit", out.exit, 0)
				equal(t, "stderr", out.stderr, "info: "+reopenedNote("guide.md", newer)+"\nwarning: "+notOpenedWarning(older)+
					"\neditor open\nwarning: notes.md still holds conflict markers, so it was left unresolved\n")
			} else {
				out := h.run("--json", "skill", "resolve", "kit", "--editor")
				equal(t, "exit", out.exit, 0)
				var logged []string
				for _, e := range h.eventsOfType(out.stderr, "log") {
					logged = append(logged, e["level"].(string)+": "+e["message"].(string))
				}
				equal(t, "what the run logged", strings.Join(logged, "\n"), "info: "+reopenedNote("guide.md", newer)+"\nwarn: "+notOpenedWarning(older)+
					"\ninfo: editor open\nwarn: notes.md still holds conflict markers, so it was left unresolved")
			}
			equal(t, "what the editor opened", fileBody(t, filepath.Join(seen, "guide.md")), "typed last\n")
			equal(t, "guide.md in the merge", inMerge(h, "guide.md"), "typed last")
			equal(t, "what is left", strings.Join(editorDirs(t, h), " "), older)
			equal(t, "what was typed first", fileBody(t, filepath.Join(older, "kit", "guide.md")), "typed first\n")
		})
	}
}

// TestSkillResolveKeepsWhatWasTypedForAFileResolvedMeanwhile: a file
// resolved with --hunk after an earlier session typed in it is not opened
// again, so the directory that holds what was typed stays. An editor
// session keeps it, marked so, and names it. Completing the merge in a
// later run gives it up to the user and names it, whether a session kept
// it or its run is gone: agentx never removes what was typed.
func TestSkillResolveKeepsWhatWasTypedForAFileResolvedMeanwhile(t *testing.T) {
	t.Parallel()
	for _, session := range []bool{true, false} {
		t.Run(fmt.Sprintf("a session between %v", session), func(t *testing.T) {
			t.Parallel()
			h, _, _ := resolveHarness(t)
			earlier := typedDir(t, h, goneProcess(t), map[string]string{"notes.md": "typed before the hang-up\n"})
			resolveKit(t, h, 0, "notes.md:1=mine", "notes.md:2=theirs")
			if session {
				stub, seen := seeingEditor(t, "exit 0\n")
				h.env["EDITOR"] = stub
				out := h.run("--json", "skill", "resolve", "kit", "--editor")
				equal(t, "exit of the session", out.exit, 0)
				nothingAt(t, "notes.md as the editor opened it", filepath.Join(seen, "notes.md"))
				equal(t, "warnings of the session", strings.Join(warnings(h, out.stderr), "\n"),
					notOpenedWarning(earlier)+"\nguide.md still holds conflict markers, so it was left unresolved")
				equal(t, "the owner of what is kept", ownerOf(t, earlier), "kept")
			}

			out := resolveKit(t, h, 0, append(slices.Clone(kitWholeFiles), "guide.md:1=mine")...)
			contains(t, "summary", h.one(out.stdout, "result")["summary"].(string), "resolved kit and updated it from ")
			equal(t, "warnings", strings.Join(notCopyWarnings(h, out.stderr), "\n"), leftWarning(earlier))
			equal(t, "what is left", strings.Join(editorDirs(t, h), " "), earlier)
			nothingAt(t, "the owner file of what is left", filepath.Join(earlier, editorOwner))
			equal(t, "what was typed", fileBody(t, filepath.Join(earlier, "kit", "notes.md")), "typed before the hang-up\n")
		})
	}
}

// TestSkillResolveOpensAFileResolvedSinceAsTheMergeHoldsIt: a session that
// writes one file into the merge and keeps its directory for another, left
// with markers and something typed, keeps only what it did not write: the
// file it wrote leaves the directory, whose owner file still records it.
// Resolved again with --hunk and then named after --editor, that file opens
// with markers afresh, and an editor closed unchanged keeps the --hunk
// resolution, the kept directory left as it is and named, since what was
// typed there is not opened. The next session of every file left opens
// what was typed for the other, writes it and removes the directory. What an earlier session whose run is gone typed for a file
// resolved with --hunk since, and holds no marker, is not opened for it
// either, closed unchanged it would replace that resolution: its directory
// is kept, and a warning says what was typed there was not opened. What was
// typed with a marker still in it is opened, whether the directory's run is
// gone or kept it, since closed unchanged it keeps the resolution: the
// directory it came from goes, the new one is kept for it, with a warning
// that names the session of that file, since the session of every file
// left, run in between, does not open it, and names the directory as it
// starts, and a session that then resolves it writes it in place of the
// --hunk resolution.
func TestSkillResolveOpensAFileResolvedSinceAsTheMergeHoldsIt(t *testing.T) {
	t.Parallel()
	t.Run("written by a session that kept its directory", func(t *testing.T) {
		t.Parallel()
		h, _, _ := resolveHarness(t)
		h.env["EDITOR"] = editorStub(t, "careful-editor", `for f in "$@"; do
	case "$f" in
	*/guide.md) printf 'resolved in the editor\n' > "$f" ;;
	*) printf 'MY CAREFUL WORK\n' >> "$f" ;;
	esac
done
`)
		out := h.run("--json", "skill", "resolve", "kit", "--editor")
		equal(t, "exit of the first session", out.exit, 0)
		equal(t, "guide.md in the merge", inMerge(h, "guide.md"), "resolved in the editor")
		dirs := editorDirs(t, h)
		if len(dirs) != 1 {
			t.Fatalf("the session left %v, want the one directory", dirs)
		}
		kept, typed := dirs[0], kitText["notes.md"]+"MY CAREFUL WORK\n"
		equal(t, "the owner of what is kept", ownerOf(t, kept), "kept")
		nothingAt(t, "guide.md, written into the merge", filepath.Join(kept, "kit", "guide.md"))
		contains(t, "the owner file", fileBody(t, filepath.Join(kept, editorOwner)), editorRecord("guide.md", []byte(kitText["guide.md"]))+"\n")
		equal(t, "what was typed, kept", fileBody(t, filepath.Join(kept, "kit", "notes.md")), typed)

		resolveKit(t, h, 0, "guide.md:1=theirs")
		stub, seen := seeingEditor(t, "exit 0\n")
		h.env["EDITOR"] = stub
		out = h.run("--json", "skill", "resolve", "kit", "--editor", "guide.md")
		equal(t, "exit", out.exit, 0)
		equal(t, "what the editor opened", fileBody(t, filepath.Join(seen, "guide.md")), kitText["guide.md"])
		equal(t, "notes", strings.Join(infos(h, out.stderr), "\n"), "")
		equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"),
			notOpenedWarning(kept)+"\nguide.md still holds conflict markers, so it keeps the way it was resolved before")
		equal(t, "guide.md in the merge", inMerge(h, "guide.md"), "guide, upstream")
		equal(t, "what is left", strings.Join(editorDirs(t, h), " "), kept)
		equal(t, "what was typed, still kept", fileBody(t, filepath.Join(kept, "kit", "notes.md")), typed)

		stub, seen = seeingEditor(t, `for f in "$@"; do printf 'finished my careful work\n' > "$f"; done`+"\n")
		h.env["EDITOR"] = stub
		out = h.run("--json", "skill", "resolve", "kit", "--editor")
		equal(t, "exit of the session of every file left", out.exit, 0)
		equal(t, "what it opened", fileBody(t, filepath.Join(seen, "notes.md")), typed)
		nothingAt(t, "guide.md as it opened it", filepath.Join(seen, "guide.md"))
		equal(t, "its note", strings.Join(infos(h, out.stderr), "\n"), reopenedNote("notes.md", kept))
		equal(t, "notes.md in the merge", inMerge(h, "notes.md"), "finished my careful work")
		equal(t, "guide.md in the merge, still", inMerge(h, "guide.md"), "guide, upstream")
		equal(t, "what is left after it", strings.Join(editorDirs(t, h), " "), "")
	})
	t.Run("typed in a session whose run is gone", func(t *testing.T) {
		t.Parallel()
		h, _, _ := resolveHarness(t)
		earlier := typedDir(t, h, goneProcess(t), map[string]string{"guide.md": "typed before the hang-up\n"})
		resolveKit(t, h, 0, "guide.md:1=theirs")
		stub, seen := seeingEditor(t, "exit 0\n")
		h.env["EDITOR"] = stub
		out := h.run("--json", "skill", "resolve", "kit", "--editor", "guide.md")
		equal(t, "exit", out.exit, 0)
		equal(t, "what the editor opened", fileBody(t, filepath.Join(seen, "guide.md")), kitText["guide.md"])
		equal(t, "notes", strings.Join(infos(h, out.stderr), "\n"), "")
		equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"),
			notOpenedWarning(earlier)+"\nguide.md still holds conflict markers, so it keeps the way it was resolved before")
		equal(t, "guide.md in the merge", inMerge(h, "guide.md"), "guide, upstream")
		equal(t, "the owner of what is kept", ownerOf(t, earlier), "kept")
		equal(t, "what was typed", fileBody(t, filepath.Join(earlier, "kit", "guide.md")), "typed before the hang-up\n")
		equal(t, "what is left", strings.Join(editorDirs(t, h), " "), earlier)
	})
	for _, gone := range []bool{false, true} {
		t.Run(fmt.Sprintf("typed with markers still in it, its run gone %v", gone), func(t *testing.T) {
			t.Parallel()
			h, _, _ := resolveHarness(t)
			pid := "kept"
			if gone {
				pid = goneProcess(t)
			}
			typed := kitText["guide.md"] + "MY CAREFUL WORK\n"
			earlier := typedDir(t, h, pid, map[string]string{"guide.md": typed})
			resolveKit(t, h, 0, "guide.md:1=theirs")
			stub, seen := seeingEditor(t, "exit 0\n")
			h.env["EDITOR"] = stub
			out := h.run("--json", "skill", "resolve", "kit", "--editor", "guide.md")
			equal(t, "exit", out.exit, 0)
			equal(t, "what the editor opened", fileBody(t, filepath.Join(seen, "guide.md")), typed)
			equal(t, "notes", strings.Join(infos(h, out.stderr), "\n"), reopenedNote("guide.md", earlier))
			nothingAt(t, "the directory it was carried from", earlier)
			dirs := editorDirs(t, h)
			if len(dirs) != 1 {
				t.Fatalf("the session left %v, want the one directory", dirs)
			}
			// guide.md is resolved, so the session of every file left does
			// not open it, and the warning names the one that does.
			equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"),
				"guide.md still holds conflict markers, so it keeps the way it was resolved before; what you typed is kept in "+dirs[0]+opensAgainKitFile("guide.md", "it"))
			equal(t, "guide.md in the merge", inMerge(h, "guide.md"), "guide, upstream")
			equal(t, "the owner of what is kept", ownerOf(t, dirs[0]), "kept")
			equal(t, "what was typed, kept", fileBody(t, filepath.Join(dirs[0], "kit", "guide.md")), typed)

			stub, seen = seeingEditor(t, "exit 0\n")
			h.env["EDITOR"] = stub
			out = h.run("--json", "skill", "resolve", "kit", "--editor")
			equal(t, "exit of the session of every file left", out.exit, 0)
			nothingAt(t, "guide.md as the session of every file left opened it", filepath.Join(seen, "guide.md"))
			equal(t, "notes.md as it opened it", fileBody(t, filepath.Join(seen, "notes.md")), kitText["notes.md"])
			equal(t, "its notes", strings.Join(infos(h, out.stderr), "\n"), "")
			equal(t, "its warnings", strings.Join(warnings(h, out.stderr), "\n"),
				notOpenedWarning(dirs[0])+"\nnotes.md still holds conflict markers, so it was left unresolved")
			equal(t, "what is left after it", strings.Join(editorDirs(t, h), " "), dirs[0])
			equal(t, "what was typed, still kept", fileBody(t, filepath.Join(dirs[0], "kit", "guide.md")), typed)

			stub, seen = seeingEditor(t, `printf 'finished my careful work\n' > "$1"`+"\n")
			h.env["EDITOR"] = stub
			out = h.run("--json", "skill", "resolve", "kit", "--editor", "guide.md")
			equal(t, "exit of the session that finishes it", out.exit, 0)
			equal(t, "what it opened", fileBody(t, filepath.Join(seen, "guide.md")), typed)
			equal(t, "its note", strings.Join(infos(h, out.stderr), "\n"), reopenedNote("guide.md", dirs[0]))
			equal(t, "guide.md in the merge once finished", inMerge(h, "guide.md"), "finished my careful work")
			equal(t, "what is left after it", strings.Join(editorDirs(t, h), " "), "")
		})
	}
}

// TestSkillResolveReopensWhatAFailedEditorTypedForAResolvedFile: a file
// resolved with --hunk and then named after --editor, typed in by an editor
// that fails, keeps what was typed, in a directory marked kept, and the
// error names the session of that file. With no marker left, what was
// typed replaces the --hunk resolution, which is what the failed session
// meant: the next session that names the file opens it again while the
// merge is the one it was typed in, and an editor closed unchanged writes
// it, the directory it came from gone. Once anything else changes the
// merge first, a --hunk of another file say, it opens afresh and the
// directory stays as it is, which the warning said may happen: it promised
// only that session, before any change; that session names the directory
// as it starts. What was typed with a marker still in it opens again
// whatever changed, and the warning says so. A merge changed while the
// editor was open refuses to write, and the warning says that what was
// typed is not opened again while the file stays resolved, and the next
// session names it as it starts.
func TestSkillResolveReopensWhatAFailedEditorTypedForAResolvedFile(t *testing.T) {
	t.Parallel()
	const typed = "my better resolution\n"
	withMarkers := kitText["guide.md"] + "MY CAREFUL WORK\n"
	whileUnchanged := ", and 'agentx skill resolve kit --editor guide.md' opens them again if it runs before anything else changes the merge of kit; otherwise copy what you need from it"
	for _, c := range []struct {
		name    string
		saved   string // what the failing editor saves in guide.md
		between bool   // a --hunk of another file changes the merge before the next session
		reopens bool
		warning string // how the warning that names the kept directory ends
	}{
		{name: "nothing between", saved: typed, reopens: true, warning: whileUnchanged},
		{name: "a --hunk between", saved: typed, between: true, warning: whileUnchanged},
		{name: "markers left, a --hunk between", saved: withMarkers, between: true, reopens: true, warning: opensAgainKitFile("guide.md", "them")},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, _, _ := resolveHarness(t)
			resolveKit(t, h, 0, "guide.md:1=theirs")
			failing := editorStub(t, "failing-editor", "printf '%s' "+shellWord(c.saved)+` > "$1"; exit 1`+"\n")
			h.env["EDITOR"] = failing
			out := h.run("--json", "skill", "resolve", "kit", "--editor", "guide.md")
			equal(t, "exit of the failing editor", out.exit, 6)
			e := h.one(out.stdout, "error")
			equal(t, "message", e["message"], "the editor "+failing+" exited with status 1, so nothing was written")
			equal(t, "hint", e["hint"], "run 'agentx skill resolve kit --editor guide.md' again once the editor works, or set GIT_EDITOR or EDITOR to another")
			dirs := editorDirs(t, h)
			if len(dirs) != 1 {
				t.Fatalf("the session left %v, want the one directory", dirs)
			}
			kept := dirs[0]
			equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), "the files you edited are kept in "+kept+c.warning)
			equal(t, "the owner of what is kept", ownerOf(t, kept), "kept")
			equal(t, "what was typed, kept", fileBody(t, filepath.Join(kept, "kit", "guide.md")), c.saved)
			equal(t, "guide.md in the merge", inMerge(h, "guide.md"), "guide, upstream")

			if c.between {
				resolveKit(t, h, 0, "logo.bin:1=mine")
			}
			stub, seen := seeingEditor(t, "exit 0\n")
			h.env["EDITOR"] = stub
			out = h.run("--json", "skill", "resolve", "kit", "--editor", "guide.md")
			equal(t, "exit of the next session", out.exit, 0)
			if !c.reopens {
				equal(t, "what the next session opened", fileBody(t, filepath.Join(seen, "guide.md")), kitText["guide.md"])
				equal(t, "its notes", strings.Join(infos(h, out.stderr), "\n"), "")
				equal(t, "its warnings", strings.Join(warnings(h, out.stderr), "\n"),
					notOpenedWarning(kept)+"\nguide.md still holds conflict markers, so it keeps the way it was resolved before")
				equal(t, "guide.md in the merge, still", inMerge(h, "guide.md"), "guide, upstream")
				equal(t, "what is left", strings.Join(editorDirs(t, h), " "), kept)
				equal(t, "what was typed, still kept", fileBody(t, filepath.Join(kept, "kit", "guide.md")), typed)
				return
			}
			equal(t, "what the next session opened", fileBody(t, filepath.Join(seen, "guide.md")), c.saved)
			equal(t, "its notes", strings.Join(infos(h, out.stderr), "\n"), reopenedNote("guide.md", kept))
			nothingAt(t, "the directory it was carried from", kept)
			if c.saved == typed {
				equal(t, "its warnings", strings.Join(warnings(h, out.stderr), "\n"), "")
				equal(t, "summary", h.one(out.stdout, "result")["summary"], "resolved guide.md in the merge of kit, 4 files left to resolve")
				equal(t, "guide.md in the merge, written", inMerge(h, "guide.md"), "my better resolution")
				equal(t, "what is left", strings.Join(editorDirs(t, h), " "), "")
				return
			}
			dirs = editorDirs(t, h)
			if len(dirs) != 1 {
				t.Fatalf("the next session left %v, want the one directory", dirs)
			}
			equal(t, "its warnings", strings.Join(warnings(h, out.stderr), "\n"),
				"guide.md still holds conflict markers, so it keeps the way it was resolved before; what you typed is kept in "+dirs[0]+opensAgainKitFile("guide.md", "it"))
			equal(t, "guide.md in the merge, still", inMerge(h, "guide.md"), "guide, upstream")
			equal(t, "what was typed, kept anew", fileBody(t, filepath.Join(dirs[0], "kit", "guide.md")), c.saved)
		})
	}
	t.Run("the merge changed while the editor was open", func(t *testing.T) {
		t.Parallel()
		h, _, _ := resolveHarness(t)
		resolveKit(t, h, 0, "guide.md:1=theirs")
		stub, goFile := waitThen(t, "printf '%s' "+shellWord(typed)+` > "$1"`+"\n")
		h.env["EDITOR"] = stub
		p := h.start("editor open", "--json", "skill", "resolve", "kit", "--editor", "guide.md")
		p.await()
		resolveKit(t, h, 0, "logo.bin:1=mine")
		writeFile(t, goFile, "")
		out := p.wait()
		equal(t, "exit", out.exit, 6)
		equal(t, "message", h.one(out.stdout, "error")["message"], "the merge of kit changed while it was being resolved, so nothing was written")
		dirs := editorDirs(t, h)
		if len(dirs) != 1 {
			t.Fatalf("the session left %v, want the one directory", dirs)
		}
		equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), "the files you edited are kept in "+dirs[0]+
			"; what you typed there is not opened again while guide.md stays resolved, since the merge of kit changed while your editor was open, so copy what you need from it")
		equal(t, "what was typed, kept", fileBody(t, filepath.Join(dirs[0], "kit", "guide.md")), typed)

		seeing, seen := seeingEditor(t, "exit 0\n")
		h.env["EDITOR"] = seeing
		out = h.run("--json", "skill", "resolve", "kit", "--editor", "guide.md")
		equal(t, "exit of the next session", out.exit, 0)
		equal(t, "what the next session opened", fileBody(t, filepath.Join(seen, "guide.md")), kitText["guide.md"])
		equal(t, "its notes", strings.Join(infos(h, out.stderr), "\n"), "")
		equal(t, "its warnings", strings.Join(warnings(h, out.stderr), "\n"),
			notOpenedWarning(dirs[0])+"\nguide.md still holds conflict markers, so it keeps the way it was resolved before")
		equal(t, "guide.md in the merge, still", inMerge(h, "guide.md"), "guide, upstream")
		equal(t, "what is left", strings.Join(editorDirs(t, h), " "), dirs[0])
	})
}

// TestSkillResolveCarriesWhatWasTypedOutOfTheFolderItCameFrom: a file a
// session opens with what an earlier one typed for it leaves the earlier
// one's directory once the new directory holds it, whether that one's run
// is gone or it is kept, so that nothing opens it again once the merge
// holds what it was resolved to. The directory stays for what else it
// holds, a swap file an editor left, or a file typed in that the session
// did not open, its line for the file carried over still in its owner
// file: kept, and named in a warning that speaks of what else was typed
// there, whether its run was gone or it was kept. The file named after
// --editor again opens as the merge holds it, with markers afresh, and that
// session names the directory again. Once the merge is given up and an
// update conflicts the same way, the file carried over opens afresh too;
// what else the directory holds opens again where it was typed in a file
// an editor session was given, and the directory goes once it is carried
// over, whereas a swap file stays, which the warning of giving up said:
// nothing opens it again, and every session names it.
func TestSkillResolveCarriesWhatWasTypedOutOfTheFolderItCameFrom(t *testing.T) {
	t.Parallel()
	draft := kitText["guide.md"] + "DRAFT\n"
	for _, source := range []string{"gone", "kept"} {
		for _, swap := range []bool{true, false} {
			t.Run(fmt.Sprintf("its run %s, a swap file %v", source, swap), func(t *testing.T) {
				t.Parallel()
				h, _, _ := resolveHarness(t)
				pid := source
				if source == "gone" {
					pid = goneProcess(t)
				}
				typed := map[string]string{"guide.md": draft}
				if !swap {
					typed["notes.md"] = "typed notes\n"
				}
				earlier := typedDir(t, h, pid, typed)
				swapFile := filepath.Join(earlier, "kit", ".guide.md.swp")
				if swap {
					writeFile(t, swapFile, "unsaved typing\n")
				}

				stub, seen := seeingEditor(t, `printf 'final guide\n' > "$1"`+"\n")
				h.env["EDITOR"] = stub
				out := h.run("--json", "skill", "resolve", "kit", "--editor", "guide.md")
				equal(t, "exit", out.exit, 0)
				equal(t, "what the editor opened", fileBody(t, filepath.Join(seen, "guide.md")), draft)
				equal(t, "the note", strings.Join(infos(h, out.stderr), "\n"), reopenedNote("guide.md", earlier))
				equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), partlyWarning(earlier))
				equal(t, "guide.md in the merge", inMerge(h, "guide.md"), "final guide")
				equal(t, "what is left", strings.Join(editorDirs(t, h), " "), earlier)
				equal(t, "the owner of what is left", ownerOf(t, earlier), "kept")
				nothingAt(t, "guide.md, carried over", filepath.Join(earlier, "kit", "guide.md"))
				contains(t, "the owner file", fileBody(t, filepath.Join(earlier, editorOwner)), editorRecord("guide.md", []byte(kitText["guide.md"]))+"\n")
				if swap {
					equal(t, "the swap file", fileBody(t, swapFile), "unsaved typing\n")
				} else {
					equal(t, "notes.md, not opened", fileBody(t, filepath.Join(earlier, "kit", "notes.md")), "typed notes\n")
				}

				stub, seen = seeingEditor(t, "exit 0\n")
				h.env["EDITOR"] = stub
				out = h.run("--json", "skill", "resolve", "kit", "--editor", "guide.md")
				equal(t, "exit of the session of guide.md again", out.exit, 0)
				equal(t, "what it opened", fileBody(t, filepath.Join(seen, "guide.md")), kitText["guide.md"])
				equal(t, "its notes", strings.Join(infos(h, out.stderr), "\n"), "")
				equal(t, "its warnings", strings.Join(warnings(h, out.stderr), "\n"),
					notOpenedWarning(earlier)+"\nguide.md still holds conflict markers, so it keeps the way it was resolved before")
				equal(t, "guide.md in the merge, still", inMerge(h, "guide.md"), "final guide")
				equal(t, "what is left after it", strings.Join(editorDirs(t, h), " "), earlier)

				out = h.run("--json", "skill", "resolve", "kit", "--abort")
				equal(t, "exit of the abort", out.exit, 0)
				want := abortWarning(earlier)
				if swap {
					want = swapWarning(earlier)
				}
				equal(t, "warnings of the abort", strings.Join(warnings(h, out.stderr), "\n"), want)
				equal(t, "the update that conflicts the same way", h.run("skill", "update", "kit").exit, 4)
				stub, seen = seeingEditor(t, "exit 0\n")
				h.env["EDITOR"] = stub
				out = h.run("--json", "skill", "resolve", "kit", "--editor")
				equal(t, "exit of the session once the merge is pending anew", out.exit, 0)
				equal(t, "guide.md as it opened it", fileBody(t, filepath.Join(seen, "guide.md")), kitText["guide.md"])
				if swap {
					equal(t, "notes.md as it opened it", fileBody(t, filepath.Join(seen, "notes.md")), kitText["notes.md"])
					equal(t, "its notes once the merge is pending anew", strings.Join(infos(h, out.stderr), "\n"), "")
					equal(t, "its warnings once the merge is pending anew", strings.Join(warnings(h, out.stderr), "\n"), notOpenedWarning(earlier)+
						"\nguide.md still holds conflict markers, so it was left unresolved\nnotes.md still holds conflict markers, so it was left unresolved")
					equal(t, "what is left once the merge is pending anew", strings.Join(editorDirs(t, h), " "), earlier)
					equal(t, "the swap file, still", fileBody(t, swapFile), "unsaved typing\n")
					return
				}
				equal(t, "notes.md as it opened it", fileBody(t, filepath.Join(seen, "notes.md")), "typed notes\n")
				equal(t, "its notes once the merge is pending anew", strings.Join(infos(h, out.stderr), "\n"), reopenedNote("notes.md", earlier))
				equal(t, "its warnings once the merge is pending anew", strings.Join(warnings(h, out.stderr), "\n"), "guide.md still holds conflict markers, so it was left unresolved")
				equal(t, "notes.md in the merge", inMerge(h, "notes.md"), "typed notes")
				equal(t, "what is left once the merge is pending anew", strings.Join(editorDirs(t, h), " "), "")
			})
		}
	}
}

// TestSkillResolveSaysWhatWasTypedThereWhereNothingWasCarried: a file of
// an earlier session's directory, its run gone, that holds nothing typed,
// just as the next session lays it out, leaves the directory as that
// session starts, and the directory is kept for what else it holds: a swap
// file, or a file typed for a conflict that changed since. Nothing typed
// there was carried over, so the warning that names it says what was typed
// there was not opened, and not what else was.
func TestSkillResolveSaysWhatWasTypedThereWhereNothingWasCarried(t *testing.T) {
	t.Parallel()
	for _, swap := range []bool{true, false} {
		t.Run(fmt.Sprintf("a swap file %v", swap), func(t *testing.T) {
			t.Parallel()
			h, _, _ := resolveHarness(t)
			typed := map[string]string{"guide.md": kitText["guide.md"]}
			if !swap {
				typed["notes.md"] = "typed for the conflict as it was\n"
			}
			earlier := typedDir(t, h, goneProcess(t), typed)
			owner, swapFile := filepath.Join(earlier, editorOwner), filepath.Join(earlier, "kit", ".guide.md.swp")
			if swap {
				writeFile(t, swapFile, "unsaved typing\n")
			} else {
				// notes.md was typed for a conflict that has changed since.
				writeFile(t, owner, strings.Replace(fileBody(t, owner), editorRecord("notes.md", []byte(kitText["notes.md"])), editorRecord("notes.md", []byte("an older conflict\n")), 1))
			}

			stub, seen := seeingEditor(t, "exit 0\n")
			h.env["EDITOR"] = stub
			out := h.run("--json", "skill", "resolve", "kit", "--editor")
			equal(t, "exit", out.exit, 0)
			equal(t, "guide.md as the editor opened it", fileBody(t, filepath.Join(seen, "guide.md")), kitText["guide.md"])
			equal(t, "notes.md as the editor opened it", fileBody(t, filepath.Join(seen, "notes.md")), kitText["notes.md"])
			equal(t, "notes", strings.Join(infos(h, out.stderr), "\n"), "")
			equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), notOpenedWarning(earlier)+
				"\nguide.md still holds conflict markers, so it was left unresolved\nnotes.md still holds conflict markers, so it was left unresolved")
			nothingAt(t, "guide.md, as the session laid it out", filepath.Join(earlier, "kit", "guide.md"))
			equal(t, "what is left", strings.Join(editorDirs(t, h), " "), earlier)
			equal(t, "the owner of what is left", ownerOf(t, earlier), "kept")
			contains(t, "the owner file", fileBody(t, owner), editorRecord("guide.md", []byte(kitText["guide.md"]))+"\n")
			if swap {
				equal(t, "the swap file", fileBody(t, swapFile), "unsaved typing\n")
			} else {
				equal(t, "notes.md, not opened", fileBody(t, filepath.Join(earlier, "kit", "notes.md")), "typed for the conflict as it was\n")
			}
		})
	}
}

// TestSkillResolveSaysWhatAFailedEditorLeftUnderAnotherName: a session
// whose editor fails having saved nothing but a swap file beside a file it
// was given, as an editor stopped with its run may leave, keeps its
// directory for it, and the warning that names it says what is there opens
// in no session, rather than promise the next session opens it again, for
// a session of every file left and for one of a file the merge records as
// resolved alike. The next session opens the file afresh, and names the
// directory as it starts, which stays with the swap file in it.
func TestSkillResolveSaysWhatAFailedEditorLeftUnderAnotherName(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name     string
		resolved []string // what --hunk resolves first
		file     []string // what names the file after --editor
		left     string   // the warnings of the files the next session leaves
	}{
		{name: "every file left",
			left: "guide.md still holds conflict markers, so it was left unresolved\nnotes.md still holds conflict markers, so it was left unresolved"},
		{name: "a file resolved already", resolved: []string{"guide.md:1=theirs"}, file: []string{"guide.md"},
			left: "guide.md still holds conflict markers, so it keeps the way it was resolved before"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, _, _ := resolveHarness(t)
			if len(c.resolved) > 0 {
				resolveKit(t, h, 0, c.resolved...)
			}
			merge := h.ref(lineage.MergeRef("kit"))
			h.env["EDITOR"] = editorStub(t, "failing-editor", `for f in "$@"; do
	case "$f" in
	*/guide.md) printf 'unsaved typing\n' > "${f%/*}/.guide.md.swp" ;;
	esac
done
exit 1
`)
			args := append([]string{"--json", "skill", "resolve", "kit", "--editor"}, c.file...)
			out := h.run(args...)
			equal(t, "exit of the failing editor", out.exit, 6)
			dirs := editorDirs(t, h)
			if len(dirs) != 1 {
				t.Fatalf("the session left %v, want the one directory", dirs)
			}
			kept, swap := dirs[0], filepath.Join(dirs[0], "kit", ".guide.md.swp")
			equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), swapWarning(kept))
			equal(t, "the owner of what is kept", ownerOf(t, kept), "kept")
			equal(t, "the swap file", fileBody(t, swap), "unsaved typing\n")
			equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), merge)

			stub, seen := seeingEditor(t, "exit 0\n")
			h.env["EDITOR"] = stub
			out = h.run(args...)
			equal(t, "exit of the next session", out.exit, 0)
			equal(t, "what the next session opened", fileBody(t, filepath.Join(seen, "guide.md")), kitText["guide.md"])
			equal(t, "its notes", strings.Join(infos(h, out.stderr), "\n"), "")
			equal(t, "its warnings", strings.Join(warnings(h, out.stderr), "\n"), notOpenedWarning(kept)+"\n"+c.left)
			equal(t, "what is left", strings.Join(editorDirs(t, h), " "), kept)
			equal(t, "the owner of what is left", ownerOf(t, kept), "kept")
			equal(t, "the swap file, still", fileBody(t, swap), "unsaved typing\n")
		})
	}
}

// TestSkillResolveAbortSaysWhatNeverOpensAgain: giving the merge up keeps a
// directory an editor session whose run is gone left with a swap file in
// it, beside a file the editor was given, and its warning says what is
// there opens in no session, rather than promise it opens again: once an
// update conflicts the same way, the next session opens the file afresh,
// and leaves the directory, and the swap file, as they are, naming it as
// it starts. Where the file was typed in too, the warning says that opens
// again and the swap file does not, and the next session opens what was
// typed, and names the directory for what else is there.
func TestSkillResolveAbortSaysWhatNeverOpensAgain(t *testing.T) {
	t.Parallel()
	for _, typed := range []bool{false, true} {
		t.Run(fmt.Sprintf("typed in the file too %v", typed), func(t *testing.T) {
			t.Parallel()
			h, _, _ := resolveHarness(t)
			notes := kitText["notes.md"]
			if typed {
				notes += "typed before the hang-up\n"
			}
			earlier := typedDir(t, h, goneProcess(t), map[string]string{"notes.md": notes})
			swap := filepath.Join(earlier, "kit", ".notes.md.swp")
			writeFile(t, swap, "unsaved typing\n")

			out := h.run("--json", "skill", "resolve", "kit", "--abort")
			equal(t, "exit of the abort", out.exit, 0)
			want := swapWarning(earlier)
			if typed {
				want = abortMixedWarning(earlier)
			}
			equal(t, "warnings of the abort", strings.Join(warnings(h, out.stderr), "\n"), want)
			equal(t, "the owner of what is kept", ownerOf(t, earlier), "kept")

			equal(t, "the update that conflicts the same way", h.run("skill", "update", "kit").exit, 4)
			stub, seen := seeingEditor(t, "exit 0\n")
			h.env["EDITOR"] = stub
			out = h.run("--json", "skill", "resolve", "kit", "--editor")
			equal(t, "exit", out.exit, 0)
			equal(t, "what the editor opened", fileBody(t, filepath.Join(seen, "notes.md")), notes)
			equal(t, "the swap file", fileBody(t, swap), "unsaved typing\n")
			if !typed {
				equal(t, "notes", strings.Join(infos(h, out.stderr), "\n"), "")
				equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), notOpenedWarning(earlier)+
					"\nguide.md still holds conflict markers, so it was left unresolved\nnotes.md still holds conflict markers, so it was left unresolved")
				equal(t, "what is left", strings.Join(editorDirs(t, h), " "), earlier)
				return
			}
			equal(t, "notes", strings.Join(infos(h, out.stderr), "\n"), reopenedNote("notes.md", earlier))
			var now string
			for _, dir := range editorDirs(t, h) {
				if dir != earlier {
					now = dir
				}
			}
			equal(t, "what is left", strings.Join(editorDirs(t, h), " "), strings.Join(sortedStrings(earlier, now), " "))
			nothingAt(t, "notes.md, carried over", filepath.Join(earlier, "kit", "notes.md"))
			equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), partlyWarning(earlier)+
				"\nguide.md still holds conflict markers, so it was left unresolved\nnotes.md still holds conflict markers, so it was left unresolved; what you typed is kept in "+now+opensAgainKit("it"))
		})
	}
}

// TestSkillResolveAbortPromisesNothingForAConflictThatChanged: what the
// editor resolved for a file that conflicts anew once a completion merges
// the edited library directory again is kept for the conflict it was
// typed for, which the merge left pending no longer has. Giving that merge
// up keeps it, but promises no session opens it again: an update then
// conflicts as the merge given up did, and the next session opens the file
// afresh and names the directory as one it did not open.
func TestSkillResolveAbortPromisesNothingForAConflictThatChanged(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	resolveKit(t, h, 0, append(slices.Clone(kitWholeFiles), "guide.md:1=mine")...)
	editLibrary(t, h, "kit", "notes.md", "ONE\ntwo\nthree\nfour\nFIVE again\n")
	stub, _, _ := resolvingEditor(t)
	h.env["EDITOR"] = stub
	out := h.run("--json", "skill", "resolve", "kit", "--editor")
	equal(t, "exit of the session", out.exit, 4)
	equal(t, "what conflicts anew", conflictFiles(h.one(out.stdout, "conflict")), "notes.md:2")
	dirs := editorDirs(t, h)
	if len(dirs) != 1 {
		t.Fatalf("the session left %v, want the one directory", dirs)
	}
	dir := dirs[0]
	equal(t, "warnings of the session", strings.Join(warnings(h, out.stderr), "\n"),
		"notes.md conflicts anew now that the merge was merged again, so what you typed for it is kept in "+dir)

	out = h.run("--json", "skill", "resolve", "kit", "--abort")
	equal(t, "exit of the abort", out.exit, 0)
	got := strings.Join(warnings(h, out.stderr), "\n")
	equal(t, "warnings of the abort", got, "the files you edited are kept in "+dir+
		"; what you typed there does not open again, because its conflict changed before the merge was given up, so copy what you need from it")
	if strings.Contains(got, "opens what you typed again") {
		t.Errorf("the abort promises what was typed opens again: %s", got)
	}
	equal(t, "the owner of what is kept", ownerOf(t, dir), "kept")

	equal(t, "the update", h.run("skill", "update", "kit").exit, 4)
	seeing, seen := seeingEditor(t, "exit 0\n")
	h.env["EDITOR"] = seeing
	out = h.run("--json", "skill", "resolve", "kit", "--editor")
	equal(t, "exit of the next session", out.exit, 0)
	equal(t, "notes of the next session", strings.Join(infos(h, out.stderr), "\n"), "")
	if opened := fileBody(t, filepath.Join(seen, "notes.md")); !strings.HasPrefix(opened, "<<<<<<< mine\n") {
		t.Errorf("the next session opened notes.md with something other than its conflict:\n%s", opened)
	}
	got = strings.Join(warnings(h, out.stderr), "\n")
	contains(t, "warnings of the next session", got, notOpenedWarning(dir))
	equal(t, "what was typed, kept", fileBody(t, filepath.Join(dir, "kit", "notes.md")), "resolved in the editor\n")
}

// TestSkillResolveAbortSaysWhatWasTypedForAConflictThatChanged: giving the
// merge up keeps a directory of an earlier session that holds something
// typed for a conflict the merge no longer has, as its owner file records
// it, and its warning says that does not open again, beside what does, and
// beside a swap file its editor left, which does not either. Once an
// update conflicts the same way, the next session opens what the warning
// promised and nothing else, and names the directory for what it did not
// open.
func TestSkillResolveAbortSaysWhatWasTypedForAConflictThatChanged(t *testing.T) {
	t.Parallel()
	const (
		changed = "; what you typed there does not open again, because its conflict changed before the merge was given up"
		again   = ": when an update of kit conflicts the same way, 'agentx skill resolve kit --editor' opens what you typed again"
		swapped = "; a file saved there under another name, such as a swap file an editor left, does not open again"
	)
	for _, c := range []struct {
		name          string
		reopens, swap bool
		end           string
	}{
		{name: "alone", end: changed + ", so copy what you need from it"},
		{name: "beside a swap file", swap: true,
			end: changed + " or it is saved under another name, such as a swap file an editor left, so copy what you need from it"},
		{name: "beside what opens again", reopens: true,
			end: again + "; what you typed for a conflict that changed before the merge was given up does not open again" + mayClean},
		{name: "beside what opens again and a swap file", reopens: true, swap: true,
			end: again + "; what you typed for a conflict that changed before the merge was given up does not open again" + swapped + mayClean},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, _, _ := resolveHarness(t)
			typed := map[string]string{"notes.md": "typed for a conflict as it was\n"}
			if c.reopens {
				typed["guide.md"] = "typed for the conflict as it is\n"
			}
			dir := typedDir(t, h, goneProcess(t), typed)
			owner := filepath.Join(dir, editorOwner)
			writeFile(t, owner, strings.Replace(fileBody(t, owner),
				editorRecord("notes.md", []byte(kitText["notes.md"])), editorRecord("notes.md", []byte("<<<<<<< mine\nas it was\n")), 1))
			if c.swap {
				writeFile(t, filepath.Join(dir, "kit", ".notes.md.swp"), "unsaved typing\n")
			}

			out := h.run("--json", "skill", "resolve", "kit", "--abort")
			equal(t, "exit of the abort", out.exit, 0)
			equal(t, "warnings of the abort", strings.Join(warnings(h, out.stderr), "\n"), "the files you edited are kept in "+dir+c.end)
			equal(t, "the owner of what is kept", ownerOf(t, dir), "kept")

			equal(t, "the update that conflicts the same way", h.run("skill", "update", "kit").exit, 4)
			stub, seen := seeingEditor(t, "exit 0\n")
			h.env["EDITOR"] = stub
			out = h.run("--json", "skill", "resolve", "kit", "--editor")
			equal(t, "exit", out.exit, 0)
			equal(t, "notes.md as the editor opened it", fileBody(t, filepath.Join(seen, "notes.md")), kitText["notes.md"])
			note, named := "", notOpenedWarning(dir)
			if c.reopens {
				equal(t, "guide.md as the editor opened it", fileBody(t, filepath.Join(seen, "guide.md")), typed["guide.md"])
				note, named = reopenedNote("guide.md", dir), partlyWarning(dir)
			}
			equal(t, "notes", strings.Join(infos(h, out.stderr), "\n"), note)
			equal(t, "the warning that names the directory", warnings(h, out.stderr)[0], named)
			equal(t, "what was typed for the conflict as it was", fileBody(t, filepath.Join(dir, "kit", "notes.md")), typed["notes.md"])
		})
	}
}

// TestSkillResolveLosesNothingWhenKilledAsItLaysOut kills an editor
// session's run with SIGKILL as it lays its files out, at each step, see
// editorLayoutHook, with and without what an earlier session whose run is
// gone typed in guide.md for it to open again. Killed before its files are
// in place, it leaves no directory of the skill, only the files it wrote
// so far, the last one cut short say, which no session takes for anything
// typed: the next one opens what the earlier session typed, or guide.md
// afresh, and removes what the killed run left. Killed once its files are
// in place and before it settles the earlier directories, it leaves what
// was typed in both, the earlier one untouched, since a session removes a
// directory it opened what was typed from only once its own holds it, and
// its copy saved when the earlier one was: the next session opens it from
// one of the two and removes both. Nothing is left in the temporary
// directory, but for an earlier directory that also holds a swap file its
// editor left: what was carried over leaves it, and it is kept for the
// swap file and named in a warning that speaks of what else is there.
func TestSkillResolveLosesNothingWhenKilledAsItLaysOut(t *testing.T) {
	t.Parallel()
	const typed = "typed before the hang-up\n"
	for _, c := range []struct {
		step        string
		typed, swap bool
	}{{"staged", false, false}, {"staged", true, false}, {"laid out", true, false}, {"laid out", true, true}} {
		t.Run(fmt.Sprintf("%s, typed %v, a swap file %v", c.step, c.typed, c.swap), func(t *testing.T) {
			t.Parallel()
			h, _, _ := resolveHarness(t)
			var earlier string
			want := kitText["guide.md"]
			if c.typed {
				earlier, want = typedDir(t, h, goneProcess(t), map[string]string{"guide.md": typed}), typed
				hourAgo := time.Now().Add(-time.Hour)
				if err := os.Chtimes(filepath.Join(earlier, "kit", "guide.md"), hourAgo, hourAgo); err != nil {
					t.Fatal(err)
				}
			}
			swap := filepath.Join(earlier, "kit", ".guide.md.swp")
			if c.swap {
				writeFile(t, swap, "unsaved typing\n")
			}
			left := killedSession(t, h, editorStub(t, "unreached-editor", "exit 1\n"), resolveKillAtEnv+"="+c.step)
			if c.typed {
				equal(t, "what was typed", fileBody(t, filepath.Join(earlier, "kit", "guide.md")), typed)
			}
			var note string
			if c.step == "staged" {
				nothingAt(t, "the skill's directory of the killed run", filepath.Join(left, "kit"))
				staged := filepath.Join(left, editorLayingOut, "guide.md")
				equal(t, "what the killed run wrote", fileBody(t, staged), want)
				// As a run killed as it wrote the file leaves it.
				writeFile(t, staged, want[:len(want)/2])
				if c.typed {
					note = reopenedNote("guide.md", earlier)
				}
			} else {
				carried := filepath.Join(left, "kit", "guide.md")
				equal(t, "what the killed run laid out", fileBody(t, carried), typed)
				// It keeps the time what it holds was typed, so the two copies
				// were saved at once, and the one whose directory sorts last is
				// opened.
				equal(t, "when what the killed run laid out was saved", modTime(t, carried), modTime(t, filepath.Join(earlier, "kit", "guide.md")))
				note = reopenedNote("guide.md", max(left, earlier))
			}

			stub, seen := seeingEditor(t, "exit 0\n")
			h.env["EDITOR"] = stub
			out := h.run("--json", "skill", "resolve", "kit", "--editor")
			equal(t, "exit", out.exit, 0)
			equal(t, "what the editor opened", fileBody(t, filepath.Join(seen, "guide.md")), want)
			equal(t, "the note", strings.Join(infos(h, out.stderr), "\n"), note)
			if c.typed {
				equal(t, "guide.md in the merge", inMerge(h, "guide.md"), "typed before the hang-up")
			}
			if !c.swap {
				equal(t, "what is left", strings.Join(editorDirs(t, h), " "), "")
				return
			}
			equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"),
				partlyWarning(earlier)+"\nnotes.md still holds conflict markers, so it was left unresolved")
			equal(t, "what is left", strings.Join(editorDirs(t, h), " "), earlier)
			equal(t, "the owner of what is left", ownerOf(t, earlier), "kept")
			nothingAt(t, "guide.md, carried over", filepath.Join(earlier, "kit", "guide.md"))
			equal(t, "the swap file", fileBody(t, swap), "unsaved typing\n")
		})
	}
}

// TestSkillResolveKeepsWhatWasTypedWhenKilledAsItKeptIt: a run marks a
// directory kept by writing its owner file anew beside it and renaming it
// over it, so one killed in between leaves the owner file as it was,
// naming the run that left the directory, and the new one beside it. Such
// a directory is still found, and what was typed there with it: giving the
// merge up keeps it and names it, and once an update conflicts the same
// way the next session opens what was typed there and removes it, the file
// left beside the owner file taken for nothing typed.
func TestSkillResolveKeepsWhatWasTypedWhenKilledAsItKeptIt(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	const typed = "typed before the hang-up\n"
	earlier := typedDir(t, h, goneProcess(t), map[string]string{"guide.md": typed})
	_, rest, _ := strings.Cut(fileBody(t, filepath.Join(earlier, editorOwner)), "\n")
	cutShort := editorOwner + ".2718281828"
	writeFile(t, filepath.Join(earlier, cutShort), editorKept+"\n"+rest)

	out := h.run("--json", "skill", "resolve", "kit", "--abort")
	equal(t, "exit of the abort", out.exit, 0)
	equal(t, "warnings of the abort", strings.Join(warnings(h, out.stderr), "\n"), abortWarning(earlier))
	equal(t, "the owner of what is kept", ownerOf(t, earlier), "kept")
	entries, err := os.ReadDir(earlier)
	if err != nil {
		t.Fatal(err)
	}
	var held []string
	for _, e := range entries {
		held = append(held, e.Name())
	}
	equal(t, "what the kept directory holds", strings.Join(held, " "), editorOwner+" "+cutShort+" kit")

	equal(t, "the update that conflicts the same way", h.run("skill", "update", "kit").exit, 4)
	stub, seen := seeingEditor(t, "exit 0\n")
	h.env["EDITOR"] = stub
	out = h.run("--json", "skill", "resolve", "kit", "--editor")
	equal(t, "exit", out.exit, 0)
	equal(t, "what the editor opened", fileBody(t, filepath.Join(seen, "guide.md")), typed)
	equal(t, "the note", strings.Join(infos(h, out.stderr), "\n"), reopenedNote("guide.md", earlier))
	equal(t, "guide.md in the merge", inMerge(h, "guide.md"), "typed before the hang-up")
	equal(t, "what is left", strings.Join(editorDirs(t, h), " "), "")
}

// TestSkillResolveKeepsWhatWasTypedWhenKilledAsItKeepsItsOwn kills a run
// with SIGKILL as it keeps its own directory, once its editor typed in
// guide.md and failed: between writing the owner file anew beside the old
// one and renaming it over it. The owner file is whole, as the run wrote it
// first, and still names the run, now gone, and the one written anew is
// beside it. The next session opens what was typed, writes it, and removes
// the directory.
func TestSkillResolveKeepsWhatWasTypedWhenKilledAsItKeepsItsOwn(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	merge := h.ref(lineage.MergeRef("kit"))
	left := killedSession(t, h, editorStub(t, "failing-editor", `printf 'typed\n' > "$1"; exit 1`+"\n"), resolveKillAtEnv+"=keeping")
	rest := h.agentx + "\n" + merge + "\n" + editorRecord("guide.md", []byte(kitText["guide.md"])) + "\n" + editorRecord("notes.md", []byte(kitText["notes.md"])) + "\n"
	equal(t, "the owner file", fileBody(t, filepath.Join(left, editorOwner)), ownerOf(t, left)+"\n"+rest)
	beside, err := filepath.Glob(filepath.Join(left, editorOwner+".*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(beside) != 1 {
		t.Fatalf("beside the owner file: %v, want the one written anew", beside)
	}
	if info, err := os.Lstat(beside[0]); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("%s is no regular file: %v", beside[0], err)
	}
	equal(t, "the owner file written anew", fileBody(t, beside[0]), editorKept+"\n"+rest)
	equal(t, "what the editor typed", fileBody(t, filepath.Join(left, "kit", "guide.md")), "typed\n")

	stub, seen := seeingEditor(t, "exit 0\n")
	h.env["EDITOR"] = stub
	out := h.run("--json", "skill", "resolve", "kit", "--editor")
	equal(t, "exit", out.exit, 0)
	equal(t, "what the editor opened", fileBody(t, filepath.Join(seen, "guide.md")), "typed\n")
	equal(t, "the note", strings.Join(infos(h, out.stderr), "\n"), reopenedNote("guide.md", left))
	equal(t, "guide.md in the merge", inMerge(h, "guide.md"), "typed")
	equal(t, "what is left", strings.Join(editorDirs(t, h), " "), "")
}

// TestSkillResolveReopensNothingItCannotTrust: a session never follows a
// symlink in an earlier session's directory, nor opens anything from a
// directory whose owner file records a path that is no file of a skill.
// A file that is a symlink, to what was typed elsewhere say, and such a
// directory are opened afresh, and the directory, its run gone, is kept
// and named in a warning; the file the link leads to is left as it is. A
// directory whose skill directory is itself a symlink is no directory of
// the skill, and is left as it is, and so is one other users can write in,
// which is never read, one whose owner file names another agentx home,
// its run gone, which is neither read nor named, and, where the test runs
// as root and can give it away, one another user owns.
func TestSkillResolveReopensNothingItCannotTrust(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	pid := goneProcess(t)
	elsewhere := filepath.Join(t.TempDir(), "guide.md")
	writeFile(t, elsewhere, "typed elsewhere\n")
	linked := typedDir(t, h, pid, map[string]string{"guide.md": "typed\n"})
	remove(t, filepath.Join(linked, "kit", "guide.md"))
	if err := os.Symlink(elsewhere, filepath.Join(linked, "kit", "guide.md")); err != nil {
		t.Fatal(err)
	}
	outside := typedDir(t, h, pid, map[string]string{"guide.md": "typed\n"})
	owner := filepath.Join(outside, editorOwner)
	writeFile(t, owner, fileBody(t, owner)+editorRecord("../guide.md", []byte(kitText["guide.md"]))+"\n")
	linkedSkill := typedDir(t, h, pid, map[string]string{"guide.md": "typed\n"})
	if err := os.Rename(filepath.Join(linkedSkill, "kit"), filepath.Join(linkedSkill, "skill")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(linkedSkill, "skill"), filepath.Join(linkedSkill, "kit")); err != nil {
		t.Fatal(err)
	}
	shared := typedDir(t, h, pid, map[string]string{"guide.md": "typed where anyone can write\n"})
	if err := os.Chmod(shared, 0o777); err != nil {
		t.Fatal(err)
	}
	otherHome := typedDir(t, h, pid, map[string]string{"guide.md": "typed for another home\n"})
	otherOwner := filepath.Join(otherHome, editorOwner)
	writeFile(t, otherOwner, strings.Replace(fileBody(t, otherOwner), "\n"+h.agentx+"\n", "\n"+filepath.Join(h.agentx, "elsewhere")+"\n", 1))
	ownedElsewhere := fileBody(t, otherOwner)
	untrusted := []string{linked, outside, linkedSkill, shared, otherHome}
	var foreign string
	if os.Getuid() == 0 {
		foreign = typedDir(t, h, pid, map[string]string{"guide.md": "typed by another user\n"})
		if err := os.Chown(foreign, 65534, 65534); err != nil {
			t.Fatal(err)
		}
		untrusted = append(untrusted, foreign)
	}

	stub, seen := seeingEditor(t, "exit 0\n")
	h.env["EDITOR"] = stub
	out := h.run("--json", "skill", "resolve", "kit", "--editor")
	equal(t, "exit", out.exit, 0)
	equal(t, "what the editor opened", fileBody(t, filepath.Join(seen, "guide.md")), kitText["guide.md"])
	equal(t, "notes", strings.Join(infos(h, out.stderr), "\n"), "")
	var want []string
	for _, dir := range sortedStrings(linked, outside) {
		want = append(want, notOpenedWarning(dir))
		equal(t, "the owner of "+dir, ownerOf(t, dir), "kept")
	}
	got := warnings(h, out.stderr)
	equal(t, "warnings", strings.Join(got[:min(len(got), len(want))], "\n"), strings.Join(want, "\n"))
	equal(t, "the owner of the directory whose skill directory is a symlink", ownerOf(t, linkedSkill), pid)
	equal(t, "the owner of the directory other users can write in", ownerOf(t, shared), pid)
	equal(t, "the owner file of another home's directory", fileBody(t, otherOwner), ownedElsewhere)
	equal(t, "what was typed for another home", fileBody(t, filepath.Join(otherHome, "kit", "guide.md")), "typed for another home\n")
	for _, w := range got {
		if strings.Contains(w, otherHome) {
			t.Errorf("a warning names another home's directory: %s", w)
		}
	}
	if foreign != "" {
		equal(t, "the owner of the directory another user owns", ownerOf(t, foreign), pid)
		equal(t, "what another user typed", fileBody(t, filepath.Join(foreign, "kit", "guide.md")), "typed by another user\n")
	}
	equal(t, "what the link leads to", fileBody(t, elsewhere), "typed elsewhere\n")
	equal(t, "what is left", strings.Join(editorDirs(t, h), " "), strings.Join(sortedStrings(untrusted...), " "))
}

// TestSkillResolveKeepsAFolderItCannotReadThrough: a directory of an
// earlier session that a run cannot read through, a folder in it the walk
// fails at before it reaches the file typed in, is never taken for one
// that holds nothing typed, however little of it was read: giving the
// merge up keeps it, and names it, a session keeps it as it starts, and
// names it as one it did not open, and completing the merge gives it up to
// the user, and names it; what was typed there stays through all three.
func TestSkillResolveKeepsAFolderItCannotReadThrough(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	dir := typedDir(t, h, goneProcess(t), map[string]string{"guide.md": "typed\n"})
	unreadableFolder(t, filepath.Join(dir, "kit", "a-drafts"))
	typed := filepath.Join(dir, "kit", "guide.md")

	out := h.run("--json", "skill", "resolve", "kit", "--abort")
	equal(t, "exit of the abort", out.exit, 0)
	equal(t, "warnings of the abort", strings.Join(warnings(h, out.stderr), "\n"), swapWarning(dir))
	equal(t, "the owner once the merge is given up", ownerOf(t, dir), "kept")
	equal(t, "what was typed, once the merge is given up", fileBody(t, typed), "typed\n")

	equal(t, "the update that conflicts again", h.run("skill", "update", "kit").exit, 4)
	stub, _ := seeingEditor(t, "exit 0\n")
	h.env["EDITOR"] = stub
	out = h.run("--json", "skill", "resolve", "kit", "--editor")
	equal(t, "exit of the session", out.exit, 0)
	equal(t, "warnings of the session", strings.Join(warnings(h, out.stderr), "\n"), notOpenedWarning(dir)+
		"\nguide.md still holds conflict markers, so it was left unresolved\nnotes.md still holds conflict markers, so it was left unresolved")
	equal(t, "the owner once a session started", ownerOf(t, dir), "kept")
	equal(t, "what was typed, once a session started", fileBody(t, typed), "typed\n")

	out = resolveKit(t, h, 0, append(slices.Clone(kitSides), "kept.md:1=theirs")...)
	equal(t, "warnings of the completion", strings.Join(notCopyWarnings(h, out.stderr), "\n"), leftWarning(dir))
	nothingAt(t, "the owner once the merge is complete", filepath.Join(dir, editorOwner))
	equal(t, "what was typed, once the merge is complete", fileBody(t, typed), "typed\n")
}

// unreadableFolder makes a folder at path that no walk reads through,
// root's included, as a folder whose mode lets nobody read it does for
// any other user: folders nested in it deeper than the longest path the
// system takes, each made from the one above, so that reading the
// innermost by its path fails.
func unreadableFolder(t *testing.T, path string) {
	t.Helper()
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	name := strings.Repeat("d", 200)
	for depth := 0; depth*len(name) < 8192; depth++ { // PATH_MAX is 4096 on Linux and 1024 on macOS
		err := root.Mkdir(name, 0o700)
		var next *os.Root
		if err == nil {
			next, err = root.OpenRoot(name)
		}
		root.Close()
		if err != nil {
			t.Fatal(err)
		}
		root = next
	}
	root.Close()
	if err := filepath.WalkDir(path, func(_ string, _ fs.DirEntry, err error) error { return err }); err == nil {
		t.Fatalf("%s reads through", path)
	}
}

// TestSkillResolveNeverOpensWhatASessionStillOpenHolds: a session still
// open is never read from: a second session started meanwhile opens the
// files afresh, and leaves the first one's directory, and what its editor
// typed there, as they are. The first then writes what its editor saved.
func TestSkillResolveNeverOpensWhatASessionStillOpenHolds(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	sleeper, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	logs := t.TempDir()
	first, goFile, seen := filepath.Join(logs, "first"), filepath.Join(logs, "go"), filepath.Join(logs, "seen")
	// The first session's editor types in guide.md, the first file it is
	// given, and waits; the second's records what it is given.
	h.env["EDITOR"] = editorStub(t, "sharing-editor", `if [ ! -e `+shellWord(first)+` ]; then
	: > `+shellWord(first)+`
	printf 'typed in the session still open\n' > "$1"
	echo "editor open" >&2
	i=0
	while [ ! -e `+shellWord(goFile)+` ] && [ $i -lt 1200 ]; do `+sleeper+` 0.05; i=$((i+1)); done
	exit 0
fi
while IFS= read -r line; do printf '%s\n' "$line"; done < "$1" > `+shellWord(seen)+`
`)
	p := h.start("editor open", "--json", "skill", "resolve", "kit", "--editor")
	p.await()
	open := editorDirs(t, h)
	if len(open) != 1 {
		t.Fatalf("the session still open has %v, want the one directory", open)
	}

	out := h.run("--json", "skill", "resolve", "kit", "--editor")
	equal(t, "exit of the second session", out.exit, 0)
	equal(t, "summary of the second session", h.one(out.stdout, "result")["summary"], "resolved nothing in the merge of kit: 5 files left to resolve")
	equal(t, "what the second session opened", fileBody(t, seen), kitText["guide.md"])
	equal(t, "notes of the second session", strings.Join(infos(h, out.stderr), "\n"), "")
	equal(t, "what is left", strings.Join(editorDirs(t, h), " "), open[0])
	equal(t, "the owner of the session still open", ownerOf(t, open[0]), ownPid)
	equal(t, "what its editor typed", fileBody(t, filepath.Join(open[0], "kit", "guide.md")), "typed in the session still open\n")

	writeFile(t, goFile, "")
	out = p.wait()
	equal(t, "exit of the first session", out.exit, 0)
	equal(t, "summary of the first session", h.one(out.stdout, "result")["summary"], "resolved guide.md in the merge of kit, 4 files left to resolve")
	equal(t, "guide.md in the merge", inMerge(h, "guide.md"), "typed in the session still open")
	equal(t, "what is left once it is written", strings.Join(editorDirs(t, h), " "), "")
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

// resolveKillAtEnv names the step of laying out an editor session's files,
// or of keeping its directory, see editorLayoutHook, at which that process
// kills itself with SIGKILL.
const resolveKillAtEnv = "AGENTX_TEST_RESOLVE_KILL_AT"

// TestResolveChildProcess is not a test: it is the body of that process.
// It does nothing when the variable that marks it is not set. The hook it
// sets is set in that process alone, never in one that runs tests.
func TestResolveChildProcess(t *testing.T) {
	args := os.Getenv(resolveChildEnv)
	if args == "" {
		t.Skip("not the resolve child process")
	}
	if at := os.Getenv(resolveKillAtEnv); at != "" {
		editorLayoutHook = func(step string) {
			if step == at {
				_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
				time.Sleep(time.Minute)
			}
		}
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
//     and nothing staged or retained left behind. A kept editor folder
//     with something typed in it is whole at every step: a completion
//     never removes one, killed or not.
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
				var typed string
				if c.check == nil {
					resolveKit(t, h, 0, kitSides...)
					typed = typedDir(t, h, editorKept, map[string]string{"notes.md": "typed before the kill\n"})
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
				equal(t, "what was typed in the kept folder", fileBody(t, filepath.Join(typed, "kit", "notes.md")), "typed before the kill\n")
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
// still be given up, and giving it up keeps what was typed in an editor
// session but promises nothing opens it again, since agentx cannot tell
// which conflicts the merge given up had.
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
				typed := typedDir(t, h, goneProcess(t), map[string]string{"guide.md": "typed\n"})
				out := h.run("--json", "skill", "resolve", "kit", "--abort")
				equal(t, "exit of the abort", out.exit, 0)
				equal(t, "the merge ref once given up", h.ref(lineage.MergeRef("kit")), "")
				equal(t, "warnings of the abort", strings.Join(warnings(h, out.stderr), "\n"), "the files you edited are kept in "+typed+
					"; what you typed there does not open again, because its conflict changed before the merge was given up, so copy what you need from it")
				equal(t, "the owner of what is kept", ownerOf(t, typed), "kept")
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

// blobIn is what an object of the account repo that is a blob holds, byte
// for byte, read back with plain git: accountGit takes off the newlines it
// ends with.
func blobIn(t *testing.T, h *harness, object string) string {
	t.Helper()
	r := gitx.New(h.env, false, func(string, ...any) {})
	out, err := r.IsolatedInput(context.Background(), gitx.AccountRepoPath(h.agentx), strings.NewReader(""), "cat-file", "blob", object)
	if err != nil {
		t.Fatalf("git cat-file blob %s: %v", object, err)
	}
	return out
}

// modeIn is the mode a commit of the account repo holds kit's file at path
// with, relative to the skill's directory, read back with plain git.
func modeIn(h *harness, commit, path string) string {
	h.t.Helper()
	mode, _, _ := strings.Cut(h.accountGit("ls-tree", commit+":kit-dir", path), " ")
	return mode
}

// TestSkillResolveAPartThatEndsAFileWithNoNewline: a hunk that ends a file
// ends as the side chosen for it ends the file, with no newline where that
// side has none, whether the other side has one or not: in the pending
// merge, and in the library once the merge completes. Both sides give
// mine's last line a newline of its own, so that theirs starts a line, and
// end as theirs does. So do the parts of a file whose lines end CRLF,
// where the line ending both gives mine's last line is a CRLF too.
func TestSkillResolveAPartThatEndsAFileWithNoNewline(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		base, mine, theirs string
		want               map[string]string // what tail.md holds once resolved to each side
	}{
		{base: "a\nb", mine: "a\nMINE", theirs: "a\nTHEIRS", want: map[string]string{sideMine: "a\nMINE", sideTheirs: "a\nTHEIRS", sideBoth: "a\nMINE\nTHEIRS"}},
		{base: "a\nb", mine: "a\nMINE", theirs: "a\nTHEIRS\n", want: map[string]string{sideMine: "a\nMINE", sideTheirs: "a\nTHEIRS\n", sideBoth: "a\nMINE\nTHEIRS\n"}},
		{base: "a\r\nb", mine: "a\r\nMINE", theirs: "a\r\nTHEIRS",
			want: map[string]string{sideMine: "a\r\nMINE", sideTheirs: "a\r\nTHEIRS", sideBoth: "a\r\nMINE\r\nTHEIRS"}},
		{base: "a\r\nb", mine: "a\r\nMINE", theirs: "a\r\nTHEIRS\r\n",
			want: map[string]string{sideMine: "a\r\nMINE", sideTheirs: "a\r\nTHEIRS\r\n", sideBoth: "a\r\nMINE\r\nTHEIRS\r\n"}},
	} {
		for _, side := range []string{sideMine, sideTheirs, sideBoth} {
			t.Run(fmt.Sprintf("%s, the update's ending in %q", side, c.theirs), func(t *testing.T) {
				t.Parallel()
				h := clashHarness(t, map[string]string{"tail.md": c.base},
					func(s *sourceRepo) { s.write("skills/kit-dir/tail.md", c.theirs) },
					func(lib string) { writeFile(t, filepath.Join(lib, "tail.md"), c.mine) })
				resolveKit(t, h, 0, "tail.md:1="+side)
				equal(t, "tail.md in the merge", blobIn(t, h, lineage.MergeRef("kit")+":kit-dir/tail.md"), c.want[side])
				resolveKit(t, h, 0, "notes.md:1=theirs")
				equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), "")
				equal(t, "tail.md in the library", fileBody(t, filepath.Join(h.library, "kit", "tail.md")), c.want[side])
			})
		}
	}
}

// TestSkillResolveKeepsTheModeEitherSideGaveAFile: a file resolved by its
// parts, or in an editor, takes the mode one side changed, as git merges
// modes. An update that makes the file executable keeps it so when the
// line both sides changed is resolved to mine: in the pending merge, in the
// library once the merge completes, and on the import branch. An
// executable bit set in the library stays when the line is resolved to the
// update's. A file neither side changes keeps the permissions it has in
// the library.
func TestSkillResolveKeepsTheModeEitherSideGaveAFile(t *testing.T) {
	t.Parallel()
	executable := func(t *testing.T, path string) bool {
		t.Helper()
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		return info.Mode()&0o111 != 0
	}
	t.Run("made executable by the update, resolved to mine", func(t *testing.T) {
		t.Parallel()
		h := clashHarness(t, map[string]string{"other.md": "o\n"},
			func(s *sourceRepo) {
				s.executable("skills/kit-dir/notes.md")
				s.write("skills/kit-dir/other.md", "o, upstream\n")
			},
			func(lib string) {
				writeFile(t, filepath.Join(lib, "other.md"), "o, mine\n")
				if err := os.Chmod(filepath.Join(lib, "SKILL.md"), 0o600); err != nil {
					t.Fatal(err)
				}
			})
		resolveKit(t, h, 0, "notes.md:1=mine")
		equal(t, "notes.md in the merge", modeIn(h, lineage.MergeRef("kit"), "notes.md"), "100755")
		resolveKit(t, h, 0, "other.md:1=theirs")
		lib := filepath.Join(h.library, "kit")
		equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), "")
		equal(t, "notes.md", fileBody(t, filepath.Join(lib, "notes.md")), "ONE\n")
		equal(t, "notes.md executable", executable(t, filepath.Join(lib, "notes.md")), true)
		equal(t, "notes.md on the import branch", modeIn(h, lineage.ManagedRef("kit"), "notes.md"), "100755")
		info, err := os.Stat(filepath.Join(lib, "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		equal(t, "the permissions of SKILL.md", info.Mode().Perm(), os.FileMode(0o600))
	})
	t.Run("made executable by the update, resolved in an editor", func(t *testing.T) {
		t.Parallel()
		h := clashHarness(t, nil, func(s *sourceRepo) { s.executable("skills/kit-dir/notes.md") }, func(string) {})
		stub, _, _ := resolvingEditor(t)
		h.env["EDITOR"] = stub
		h.mustRun("skill", "resolve", "kit", "--editor")
		lib := filepath.Join(h.library, "kit")
		equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), "")
		equal(t, "notes.md", fileBody(t, filepath.Join(lib, "notes.md")), "resolved in the editor\n")
		equal(t, "notes.md executable", executable(t, filepath.Join(lib, "notes.md")), true)
		equal(t, "notes.md on the import branch", modeIn(h, lineage.ManagedRef("kit"), "notes.md"), "100755")
	})
	t.Run("made executable in the library, resolved to theirs", func(t *testing.T) {
		t.Parallel()
		h := clashHarness(t, nil, func(*sourceRepo) {}, func(lib string) {
			if err := os.Chmod(filepath.Join(lib, "notes.md"), 0o755); err != nil {
				t.Fatal(err)
			}
		})
		resolveKit(t, h, 0, "notes.md:1=theirs")
		lib := filepath.Join(h.library, "kit")
		equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), "")
		equal(t, "notes.md", fileBody(t, filepath.Join(lib, "notes.md")), "uno\n")
		equal(t, "notes.md executable", executable(t, filepath.Join(lib, "notes.md")), true)
		equal(t, "notes.md on the import branch", modeIn(h, lineage.ManagedRef("kit"), "notes.md"), "100644")
	})
}

// TestSkillResolveInAnEditorASkillCalledOwner: the file that says whose an
// editor session's directory is sits beside the skill's directory under a
// hidden name, which no skill of the library can have, so a skill called
// owner opens in an editor and completes like any other, and what an
// earlier session of it left behind, its run gone, is removed by the next.
func TestSkillResolveInAnEditorASkillCalledOwner(t *testing.T) {
	t.Parallel()
	h := pendingHarness(t, "owner", nil, func(*sourceRepo) {}, func(string) {})
	candidate := h.ref(lineage.CandidateRef("owner"))
	stale := editorDir(t, h, "owner", goneProcess(t), h.agentx)
	stub, _, _ := resolvingEditor(t)
	h.env["EDITOR"] = stub

	out := h.run("--json", "skill", "resolve", "owner", "--editor")
	equal(t, "exit", out.exit, 0)
	contains(t, "summary", h.one(out.stdout, "result")["summary"].(string), "resolved owner and updated it from ")
	equal(t, "notes.md", fileBody(t, filepath.Join(h.library, "owner", "notes.md")), "resolved in the editor\n")
	equal(t, "the import branch", h.ref(lineage.ManagedRef("owner")), candidate)
	equal(t, "the merge ref", h.ref(lineage.MergeRef("owner")), "")
	nothingAt(t, "the stale session", stale)
	equal(t, "what the sessions left", strings.Join(editorDirs(t, h), " "), "")
}

// TestSkillResolveInAnEditorKeepsLinesLikeMarkers: a file whose own lines
// look like conflict markers is opened with markers longer than any of
// them, and only a marker of that size keeps it unresolved: a save that
// keeps the file's own lines as they are resolves it, and completes the
// merge with them byte for byte, and one that leaves the markers keeps the
// merge pending. Chosen by --hunk, the file keeps those lines too.
func TestSkillResolveInAnEditorKeepsLinesLikeMarkers(t *testing.T) {
	t.Parallel()
	markers := "a\n<<<<<<< HEAD\nb\n=========\nc\n>>>>>>>> x\n"
	pending := func(t *testing.T) *harness {
		t.Helper()
		h := clashHarness(t, map[string]string{"markers.md": markers},
			func(s *sourceRepo) {
				s.write("skills/kit-dir/markers.md", strings.Replace(markers, "c\n", "c, upstream\n", 1))
			},
			func(lib string) {
				writeFile(t, filepath.Join(lib, "markers.md"), strings.Replace(markers, "c\n", "c, mine\n", 1))
			})
		resolveKit(t, h, 0, "notes.md:1=theirs")
		return h
	}
	t.Run("a save that keeps them", func(t *testing.T) {
		t.Parallel()
		h := pending(t)
		candidate := h.ref(lineage.CandidateRef("kit"))
		seen := filepath.Join(t.TempDir(), "seen")
		saved := "a\n<<<<<<< HEAD\nb\n=========\nc, resolved\n>>>>>>>> x\n"
		h.env["EDITOR"] = editorStub(t, "saving-editor", `while IFS= read -r line; do printf '%s\n' "$line"; done < "$1" > `+shellWord(seen)+`
printf '%s' `+shellWord(saved)+` > "$1"
`)
		out := h.run("--json", "skill", "resolve", "kit", "--editor")
		equal(t, "exit", out.exit, 0)
		equal(t, "what the editor was given", fileBody(t, seen), "a\n<<<<<<< HEAD\nb\n=========\n"+
			strings.Repeat("<", 10)+" mine\nc, mine\n"+strings.Repeat("|", 10)+" base\nc\n"+strings.Repeat("=", 10)+"\nc, upstream\n"+strings.Repeat(">", 10)+" theirs\n"+
			">>>>>>>> x\n")
		contains(t, "summary", h.one(out.stdout, "result")["summary"].(string), "resolved kit and updated it from ")
		equal(t, "markers.md", fileBody(t, filepath.Join(h.library, "kit", "markers.md")), saved)
		equal(t, "the import branch", h.ref(lineage.ManagedRef("kit")), candidate)
		equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), "")
	})
	t.Run("a save that leaves the markers", func(t *testing.T) {
		t.Parallel()
		h := pending(t)
		merge := h.ref(lineage.MergeRef("kit"))
		h.env["EDITOR"] = editorStub(t, "idle-editor", "exit 0\n")
		out := h.run("--json", "skill", "resolve", "kit", "--editor")
		equal(t, "exit", out.exit, 0)
		equal(t, "summary", h.one(out.stdout, "result")["summary"], "resolved nothing in the merge of kit: 1 file left to resolve")
		equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), "markers.md still holds conflict markers, so it was left unresolved")
		equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), merge)
	})
	t.Run("the update's side chosen", func(t *testing.T) {
		t.Parallel()
		h := pending(t)
		resolveKit(t, h, 0, "markers.md:1=theirs")
		equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), "")
		equal(t, "markers.md", fileBody(t, filepath.Join(h.library, "kit", "markers.md")), strings.Replace(markers, "c\n", "c, upstream\n", 1))
	})
}

// TestSkillResolveRefreshesACopyOfTheEditedLibrary: completing the merge
// refreshes a copy that holds the library directory as the merge read it,
// the edits that conflicted included, as it refreshes one that holds the
// version installed; a copy edited where it is keeps its edits, with its
// warning.
func TestSkillResolveRefreshesACopyOfTheEditedLibrary(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	lib := filepath.Join(h.library, "kit")
	claude := filepath.Join(h.home, ".claude", "skills", "kit")
	cursor := filepath.Join(h.home, ".cursor", "skills", "kit")
	h.mustRun("skill", "remove", "kit", "--from", "claude-code")
	h.mustRun("skill", "place", "kit", "--to", "claude-code", "--copy")
	sameTree(t, "claude's copy, placed while the merge is pending", libraryTree(t, claude), libraryTree(t, lib))
	editCopy(t, cursor)
	edited := libraryTree(t, cursor)
	resolveKit(t, h, 0, kitSides...)

	out := resolveKit(t, h, 0, "kept.md:1=theirs")
	contains(t, "summary", h.one(out.stdout, "result")["summary"].(string), ", 1 copy placement refreshed, 1 placement skipped")
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"),
		keptCopyWarning(cursor, "kit", "agentx skill remove kit --from cursor", "agentx skill place kit --to cursor --copy"))
	want := kitTree(t, h, kitResolved)
	sameTree(t, "the library directory", libraryTree(t, lib), want)
	sameTree(t, "claude's copy", libraryTree(t, claude), want)
	sameTree(t, "cursor's copy", libraryTree(t, cursor), edited)
	for _, dir := range []string{h.library, filepath.Dir(claude), filepath.Dir(cursor)} {
		equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
	}
	equal(t, "journals", journalCount(t, h), 0)
}

// TestSkillResolveLeavesNoEmptyDirectory: a directory whose last file is
// resolved to the side that deleted it goes with the file. Completing the
// merge lays out no empty directory in the library, or in a copy it
// refreshes, as no side of the merge has one and git records none, and so
// too when the library directory was edited while the merge was pending
// and is merged again.
func TestSkillResolveLeavesNoEmptyDirectory(t *testing.T) {
	t.Parallel()
	for _, meanwhile := range []bool{false, true} {
		t.Run(fmt.Sprintf("edited meanwhile %v", meanwhile), func(t *testing.T) {
			t.Parallel()
			h := clashHarness(t, map[string]string{"docs/old.md": "old\n", "usage.md": "usage\n"},
				func(s *sourceRepo) { s.run("rm", "--quiet", "skills/kit-dir/docs/old.md") },
				func(lib string) { writeFile(t, filepath.Join(lib, "docs", "old.md"), "old, mine\n") })
			equal(t, "the conflicts", conflictFiles(h.one(h.mustRun("--json", "skill", "resolve", "kit").stdout, "conflict")), "docs/old.md:whole,notes.md:1")
			want := map[string]string{"SKILL.md": fileBody(t, filepath.Join(h.library, "kit", "SKILL.md")), "notes.md": "uno\n", "usage.md": "usage\n"}
			if meanwhile {
				editLibrary(t, h, "kit", "usage.md", "usage, edited meanwhile\n")
				want["usage.md"] = "usage, edited meanwhile\n"
			}
			claude := filepath.Join(h.home, ".claude", "skills", "kit")
			h.mustRun("skill", "remove", "kit", "--from", "claude-code")
			h.mustRun("skill", "place", "kit", "--to", "claude-code", "--copy")

			out := resolveKit(t, h, 0, "notes.md:1=theirs", "docs/old.md:1=theirs")
			summary := h.one(out.stdout, "result")["summary"].(string)
			contains(t, "summary", summary, ", 1 copy placement refreshed")
			if meanwhile {
				contains(t, "summary", summary, "resolved kit, merged the edits made to it meanwhile and updated it from ")
			}
			equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), "")
			sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "kit")), want)
			sameTree(t, "claude's copy", libraryTree(t, claude), want)
			nothingAt(t, "docs in the library directory", filepath.Join(h.library, "kit", "docs"))
			nothingAt(t, "docs in claude's copy", filepath.Join(claude, "docs"))
		})
	}
}

// TestSkillResolveSweepsStagingAKilledCompletionLeft stands in for a
// completion killed after it staged the merged version beside the library
// directory and a refreshed copy beside each copy placement, and before
// its journal was written: nothing names any of them, so the next
// completion sweeps them all before it stages anything, and leaves nothing
// beside the library or a copy.
func TestSkillResolveSweepsStagingAKilledCompletionLeft(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	claude := filepath.Join(h.home, ".claude", "skills", "kit")
	cursor := filepath.Join(h.home, ".cursor", "skills", "kit")
	editCopy(t, cursor)
	edited := libraryTree(t, cursor)
	candidate := h.ref(lineage.CandidateRef("kit"))
	resolveKit(t, h, 0, kitSides...)
	dirs := []string{h.library, filepath.Dir(claude), filepath.Dir(cursor)}
	for i, dir := range dirs {
		staged := filepath.Join(dir, fmt.Sprintf(".agentx-staged-deadbeef-%d", i+1))
		writeFile(t, mkdirs(t, staged, "SKILL.md"), skill("kit", "a version nothing names"))
	}

	resolveKit(t, h, 0, "kept.md:1=theirs")
	for _, dir := range dirs {
		equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
	}
	equal(t, "journals", journalCount(t, h), 0)
	equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), "")
	equal(t, "the candidate ref", h.ref(lineage.CandidateRef("kit")), "")
	equal(t, "the import branch", h.ref(lineage.ManagedRef("kit")), candidate)
	want := kitTree(t, h, kitResolved)
	sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "kit")), want)
	sameTree(t, "claude's copy", libraryTree(t, claude), want)
	sameTree(t, "cursor's copy", libraryTree(t, cursor), edited)
}

// TestSkillResolveOfAMergeWhosePathsDifferInCase: a merge resolved to a
// version that holds readme.md, a file the user added, and README.md, one
// the update adds, completes where the library tells the two apart. Where
// it does not, as macOS's by default does not, completing it is refused
// with exit code 6 before anything is written, and the hint says how out
// of it: resolve one of them to the other side, or give the merge up and
// rename the user's file. The merge stays pending as it was, and giving it
// up leaves the user's file where it is.
func TestSkillResolveOfAMergeWhosePathsDifferInCase(t *testing.T) {
	t.Parallel()
	h, s, _ := updateHarness(t)
	lib := filepath.Join(h.library, "alpha")
	editLibrary(t, h, "alpha", "notes.md", editedNotes)
	editLibrary(t, h, "alpha", "readme.md", "my own readme\n")
	s.write("skills/alpha-dir/README.md", "the upstream readme\n")
	newVersion(t, s)
	h.mustRun("skill", "check")
	if out := h.run("--json", "skill", "update", "alpha"); out.exit != 4 {
		t.Fatalf("the update that should conflict: exit %d\n%s%s", out.exit, out.stdout, out.stderr)
	}
	candidate := h.ref(lineage.CandidateRef("alpha"))

	if !foldsCaseAt(t, filepath.Join(lib, "SKILL.md")) {
		h.mustRun("skill", "resolve", "alpha", "--hunk", "notes.md:1=theirs")
		equal(t, "readme.md", fileBody(t, filepath.Join(lib, "readme.md")), "my own readme\n")
		equal(t, "README.md", fileBody(t, filepath.Join(lib, "README.md")), "the upstream readme\n")
		equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), candidate)
		equal(t, "the merge ref", h.ref(lineage.MergeRef("alpha")), "")
		return
	}
	refs := h.accountGit("for-each-ref", "--format=%(refname) %(objectname)")
	library, placed, before := onDisk(t, h.library), onDisk(t, h.home), mutationVersion(t, h)

	out := h.run("--json", "skill", "resolve", "alpha", "--hunk", "notes.md:1=theirs")
	equal(t, "exit", out.exit, 6)
	e := h.one(out.stdout, "error")
	equal(t, "code", e["code"], "refused")
	equal(t, "message", e["message"], "the merge of alpha holds both README.md and readme.md, which this file system cannot keep apart, so nothing was changed")
	equal(t, "hint", e["hint"], "resolve one of them to the other side, or run 'agentx skill resolve alpha --abort' to give the merge up and rename the one in "+
		quotedPath(lib)+" before you update again")
	equal(t, "the refs", h.accountGit("for-each-ref", "--format=%(refname) %(objectname)"), refs)
	equal(t, "the library", onDisk(t, h.library), library)
	equal(t, "the placements", onDisk(t, h.home), placed)
	equal(t, "mutations", mutationVersion(t, h), before)
	equal(t, "journals", journalCount(t, h), 0)
	equal(t, "what is left beside the library", strings.Join(hiddenEntries(t, h.library), " "), "")

	h.mustRun("skill", "resolve", "alpha", "--abort")
	equal(t, "the merge ref once given up", h.ref(lineage.MergeRef("alpha")), "")
	equal(t, "readme.md once given up", fileBody(t, filepath.Join(lib, "readme.md")), "my own readme\n")
}

// TestSkillResolveRefusesAnImportBranchMovedUnderTheLock: the import branch
// is read again under the lock, and one something moved after the run read
// it, or a fork branch made for the skill meanwhile, refuses the run with
// exit code 6 and nothing written, whether the run completes the merge or
// rewrites it: what it would write is a merge on a version the skill is no
// longer at. The merge ref, the candidate, the library and every copy are
// left as they were, and the run made again merges on the version the
// branch points at now.
func TestSkillResolveRefusesAnImportBranchMovedUnderTheLock(t *testing.T) {
	t.Parallel()
	hint := "run the command again to merge it on the version the branch points at now"
	for _, c := range []struct {
		name     string
		at       string   // the git command the branch is moved on, the last before the lock
		fork     bool     // a fork branch is made rather than the import branch moved
		resolved []string // what is resolved before the run
		hunk     string   // what the run resolves
		message  string
	}{
		{name: "a completion", at: `*" rev-parse "*"^{tree} "*`, resolved: kitSides, hunk: "kept.md:1=theirs",
			message: "the import branch refs/heads/managed/kit moved while the merge of kit was being completed, so nothing was written"},
		{name: "a completion, a fork made meanwhile", at: `*" rev-parse "*"^{tree} "*`, fork: true, resolved: kitSides, hunk: "kept.md:1=theirs",
			message: "the import branch refs/heads/managed/kit moved while the merge of kit was being completed, so nothing was written"},
		{name: "a rewrite", at: `*" commit-tree "*`, hunk: "logo.bin:1=mine",
			message: "the import branch refs/heads/managed/kit moved while the merge of kit was being resolved, so nothing was written"},
		{name: "a rewrite, a fork made meanwhile", at: `*" commit-tree "*`, fork: true, hunk: "logo.bin:1=mine",
			message: "the import branch refs/heads/managed/kit moved while the merge of kit was being resolved, so nothing was written"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, _, _ := resolveHarness(t)
			if len(c.resolved) > 0 {
				resolveKit(t, h, 0, c.resolved...)
			}
			merge, tip, candidate := h.ref(lineage.MergeRef("kit")), h.ref(lineage.ManagedRef("kit")), h.ref(lineage.CandidateRef("kit"))
			moved := versionOf(t, h, tip, nil)
			step := "update-ref " + lineage.ManagedRef("kit") + " " + moved
			if c.fork {
				step = "update-ref " + lineage.ForkRef("kit") + " " + tip
			}
			library, placed := onDisk(t, h.library), onDisk(t, h.home)
			stubGit(t, h, `#!/bin/sh
case " $* " in
`+c.at+`) `+realGit(t)+" --git-dir="+shellWord(gitx.AccountRepoPath(h.agentx))+" "+step+` || exit 1 ;;
esac
exec `+realGit(t)+` "$@"
`)
			out := resolveKit(t, h, 6, c.hunk)
			e := h.one(out.stdout, "error")
			equal(t, "message", e["message"], c.message)
			equal(t, "hint", e["hint"], hint)
			equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), merge)
			equal(t, "the candidate", h.ref(lineage.CandidateRef("kit")), candidate)
			equal(t, "the library", onDisk(t, h.library), library)
			equal(t, "the placements", onDisk(t, h.home), placed)
			equal(t, "journals", journalCount(t, h), 0)
			equal(t, "what is left beside the library", strings.Join(hiddenEntries(t, h.library), " "), "")
			if c.fork {
				return
			}
			equal(t, "the import branch", h.ref(lineage.ManagedRef("kit")), moved)

			h.env["PATH"] = gitOnlyPath(t) // the git that moves the branch is gone
			again := resolveKit(t, h, 0, c.hunk)
			if len(c.resolved) == 0 {
				equal(t, "summary once run again", h.one(again.stdout, "result")["summary"], "resolved logo.bin in the merge of kit, 4 files left to resolve")
				equal(t, "Agentx-Merge-Base once run again", trailer(t, h, h.ref(lineage.MergeRef("kit")), lineage.TrailerMergeBase), moved)
				return
			}
			sameTree(t, "the library directory once run again", libraryTree(t, filepath.Join(h.library, "kit")), kitTree(t, h, kitResolved))
			equal(t, "the import branch once run again", h.ref(lineage.ManagedRef("kit")), candidate)
			equal(t, "the merge ref once run again", h.ref(lineage.MergeRef("kit")), "")
		})
	}
}

// TestSkillResolveRefusesAMergeChangedUnderTheLock: the merge ref is read
// again under the lock, and one another run gave up or rewrote after this
// run read it refuses the run with exit code 6 and nothing written, whether
// the run completes the merge, rewrites it or lays its files out for an
// editor, which then never opens: what it would write, or open, is a merge
// that is no longer the one pending. The import branch, the candidate, the
// library and every copy are left as they were, nothing is staged beside
// the library, and the merge ref holds what the other run left.
func TestSkillResolveRefusesAMergeChangedUnderTheLock(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name     string
		at       string   // the git command the merge ref is changed on, the last before the lock
		gone     bool     // the merge is given up meanwhile rather than rewritten
		resolved []string // what is resolved before the run
		args     []string // what the run is asked to do
		while    string   // what the refusal says the run was doing
	}{
		{name: "a completion, the merge given up meanwhile", at: `*" rev-parse "*"^{tree} "*`, gone: true, resolved: kitSides,
			args: []string{"--hunk", "kept.md:1=theirs"}, while: "it was being completed"},
		{name: "a completion, the merge rewritten meanwhile", at: `*" rev-parse "*"^{tree} "*`, resolved: kitSides,
			args: []string{"--hunk", "kept.md:1=theirs"}, while: "it was being completed"},
		{name: "a rewrite, the merge rewritten meanwhile", at: `*" commit-tree "*`,
			args: []string{"--hunk", "logo.bin:1=mine"}, while: "it was being resolved"},
		{name: "files opened in an editor, the merge given up meanwhile", at: `*" merge-file "*`, gone: true,
			args: []string{"--editor"}, while: "the files were being opened"},
		{name: "files opened in an editor, the merge rewritten meanwhile", at: `*" merge-file "*`,
			args: []string{"--editor"}, while: "the files were being opened"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, _, _ := resolveHarness(t)
			if len(c.resolved) > 0 {
				resolveKit(t, h, 0, c.resolved...)
			}
			merge, tip, candidate := h.ref(lineage.MergeRef("kit")), h.ref(lineage.ManagedRef("kit")), h.ref(lineage.CandidateRef("kit"))
			// Another run's rewrite: a commit of the same merge that is not
			// the one this run read.
			args := []string{"commit-tree", merge + "^{tree}"}
			for _, parent := range strings.Fields(h.accountGit("rev-parse", merge+"^@")) {
				args = append(args, "-p", parent)
			}
			after := h.accountGitInput("", append(args, "-m", "written again", "-m", pendingMessage(h, "kit"))...)
			step := "update-ref " + lineage.MergeRef("kit") + " " + after
			if c.gone {
				after, step = "", "update-ref -d "+lineage.MergeRef("kit")
			}
			ran := filepath.Join(t.TempDir(), "ran")
			h.env["EDITOR"] = editorStub(t, "unwanted-editor", "echo ran > "+shellWord(ran)+"\nexit 99\n")
			library, placed := onDisk(t, h.library), onDisk(t, h.home)
			stubGit(t, h, `#!/bin/sh
case " $* " in
`+c.at+`) `+realGit(t)+" --git-dir="+shellWord(gitx.AccountRepoPath(h.agentx))+" "+step+` || exit 1 ;;
esac
exec `+realGit(t)+` "$@"
`)
			out := h.run(append([]string{"--json", "skill", "resolve", "kit"}, c.args...)...)
			h.env["PATH"] = gitOnlyPath(t) // the git that changes the merge is gone
			equal(t, "exit", out.exit, 6)
			e := h.one(out.stdout, "error")
			equal(t, "message", e["message"], "the merge of kit changed while "+c.while+", so nothing was written")
			equal(t, "hint", e["hint"], "run 'agentx skill resolve kit' to see the merge as it is now")
			equal(t, "the merge ref", h.ref(lineage.MergeRef("kit")), after)
			equal(t, "the import branch", h.ref(lineage.ManagedRef("kit")), tip)
			equal(t, "the candidate", h.ref(lineage.CandidateRef("kit")), candidate)
			equal(t, "the library", onDisk(t, h.library), library)
			equal(t, "the placements", onDisk(t, h.home), placed)
			equal(t, "journals", journalCount(t, h), 0)
			equal(t, "what is left beside the library", strings.Join(hiddenEntries(t, h.library), " "), "")
			nothingAt(t, "what the editor would have written", ran)
			equal(t, "what the run laid out for an editor", strings.Join(editorDirs(t, h), " "), "")
		})
	}
}
