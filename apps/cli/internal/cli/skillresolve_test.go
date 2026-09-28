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
// new.md and mine.md are added on one side each, which merge cleanly. It
// returns the update's conflict event.
func resolveHarness(t *testing.T) (h *harness, s *sourceRepo, ev jsonEvent) {
	t.Helper()
	h = newHarness(t)
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

// kitSides resolves every file of kit's merge but kept.md, which the tests
// resolve last: the files resolve to kitResolved.
var kitSides = []string{"notes.md:1=mine", "notes.md:2=theirs", "guide.md:1=both", "logo.bin:1=mine", "gone.md:1=mine"}

// kitResolved is what the files that conflict hold once kitSides and
// kept.md:1=theirs resolve them.
var kitResolved = map[string]string{
	"notes.md": "ONE\ntwo\nthree\nfour\ncinco\n", "guide.md": "guide, mine\nguide, upstream\n",
	"logo.bin": "\x00mine", "gone.md": "gone, mine\n", "kept.md": "kept, upstream\n",
}

// kitWholeFiles resolves the files of kit's merge that conflict whole,
// which no editor can open, leaving guide.md and notes.md.
var kitWholeFiles = []string{"logo.bin:1=mine", "gone.md:1=mine", "kept.md:1=theirs"}

// upstreamOf is the upstream commit an import commit records, short, read
// back with plain git.
func upstreamOf(h *harness, commit string) string {
	return short(h.accountGit("log", "-1", "--format=%(trailers:key="+lineage.TrailerCommit+",valueonly)", commit))
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

// inCheckout is the file of kit's checkout at path, relative to the
// skill's directory.
func inCheckout(h *harness, path string) string {
	return filepath.Join(pendingCheckout(h, "kit"), "kit-dir", filepath.FromSlash(path))
}

// stagedAt is what the index of kit's checkout holds at path, relative to
// the skill's directory: one line per stage, as git ls-files -s prints it,
// "" for nothing.
func stagedAt(t *testing.T, h *harness, path string) string {
	t.Helper()
	return strings.TrimSpace(checkoutGit(t, h, "kit", "ls-files", "-s", "--", "kit-dir/"+path))
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

// completedKit fails the test unless kit's merge is complete with its
// files resolved as want says: the library directory holds want, the
// import branch is at candidate, and no candidate, checkout or journal is
// left.
func completedKit(t *testing.T, h *harness, candidate string, want map[string]string) {
	t.Helper()
	sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "kit")), want)
	equal(t, "the import branch", h.ref(lineage.ManagedRef("kit")), candidate)
	equal(t, "the candidate ref", h.ref(lineage.CandidateRef("kit")), "")
	noCheckout(t, h, "kit")
	equal(t, "journals", journalCount(t, h), 0)
}

// clashHarness is a machine with Claude Code and one skill, kit, whose
// first version holds base and notes.md, "one", and whose update and edits
// conflict in notes.md, one hunk, and wherever upstream and edit, given
// the source and the library directory, make them conflict otherwise. It
// returns once the update left the merge pending.
func clashHarness(t *testing.T, base map[string]string, upstream func(s *sourceRepo), edit func(lib string)) *harness {
	t.Helper()
	return pendingHarness(t, "kit", base, upstream, edit)
}

// pendingHarness is clashHarness for a skill called name, at
// skills/<name>-dir in its source.
func pendingHarness(t *testing.T, name string, base map[string]string, upstream func(s *sourceRepo), edit func(lib string)) *harness {
	t.Helper()
	h := newHarness(t)
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

// TestSkillResolveShowsTheMergeAsItStands: skill resolve with no flag, in
// an invocation of its own after the update, reports the merge pending and
// changes nothing: one conflict event whose files and hunks are exactly the
// ones the update reported, numbered alike, whose three versions are the
// checkout's HEAD^, HEAD and MERGE_HEAD, and in the text a line counting
// the files left, then every hunk under its file and number and every
// whole file with how it conflicts. Nothing is written: not the library,
// not a ref, not the checkout, not the version file.
func TestSkillResolveShowsTheMergeAsItStands(t *testing.T) {
	t.Parallel()
	h, s, updated := resolveHarness(t)
	refs := h.accountGit("for-each-ref", "--format=%(refname) %(objectname)")
	library, before, index := onDisk(t, h.library), mutationVersion(t, h), unmerged(t, h, "kit")
	first, second := upstreamOf(h, lineage.ManagedRef("kit")), short(s.run("rev-parse", "HEAD"))
	head, mergeHead, _ := mergeState(t, h, "kit")

	out := h.run("--json", "skill", "resolve", "kit")
	equal(t, "exit", out.exit, 0)
	ev := h.one(out.stdout, "conflict")
	equal(t, "the files", filesOf(t, ev), filesOf(t, updated))
	for _, field := range []string{"name", "kind", "base", "mine", "theirs"} {
		equal(t, field, ev[field], updated[field])
	}
	equal(t, "base", ev["base"], strings.TrimSpace(checkoutGit(t, h, "kit", "rev-parse", "HEAD^")))
	equal(t, "mine", ev["mine"], head)
	equal(t, "theirs", ev["theirs"], mergeHead)
	equal(t, "summary", h.one(out.stdout, "result")["summary"],
		"kit has a merge pending with its update from "+first+" to "+second+": 5 files unresolved")

	text := h.run("skill", "resolve", "kit")
	equal(t, "exit in text", text.exit, 0)
	equal(t, "the text", text.stdout, "kit has a merge pending with its update from "+first+" to "+second+": 5 files unresolved\n"+
		"gone.md: changed here, deleted by the update\n"+
		"guide.md:1\n<<<<<<< mine\nguide, mine\n||||||| base\nguide\n=======\nguide, upstream\n>>>>>>> theirs\n"+
		"kept.md: deleted here, changed by the update\n"+
		"logo.bin: binary, changed here and by the update\n"+
		"notes.md:1\n<<<<<<< mine\nONE\n||||||| base\none\n=======\nuno\n>>>>>>> theirs\n"+
		"notes.md:2\n<<<<<<< mine\nFIVE\n||||||| base\nfive\n=======\ncinco\n>>>>>>> theirs\n")
	equal(t, "stderr in text", text.stderr, "")

	equal(t, "the refs", h.accountGit("for-each-ref", "--format=%(refname) %(objectname)"), refs)
	equal(t, "the library", onDisk(t, h.library), library)
	equal(t, "what is unmerged", unmerged(t, h, "kit"), index)
	equal(t, "mutations", mutationVersion(t, h), before)
	equal(t, "journals", journalCount(t, h), 0)
}

// TestSkillResolveRefusesWithoutAMergeToResolve: a name the library does
// not hold and no lineage names is exit code 5; a managed skill with no
// checkout of a pending merge, and an unmanaged one, are exit code 6,
// whatever the run was asked to do, and nothing is written.
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
// cannot take is exit code 1 and writes nothing: a file that is not
// unmerged, a hunk a file does not have, both sides of a file that
// conflicts whole, two sides for one hunk, and a file some of whose hunks
// are given no side, which names every one of them.
func TestSkillResolveRefusesAChoiceTheMergeCannotTake(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	index := unmerged(t, h, "kit")
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
		equal(t, what+": what is unmerged", unmerged(t, h, "kit"), index)
	}
	equal(t, "journals", journalCount(t, h), 0)

	// A file staged already is not unmerged any more.
	resolveKit(t, h, 0, "guide.md:1=mine")
	out := resolveKit(t, h, 1, "guide.md:1=theirs")
	equal(t, "a file staged already", h.one(out.stdout, "error")["message"], "guide.md does not conflict in the merge of kit")
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
	noCheckout(t, h, "kit")
	equal(t, "a:b=c.md", fileBody(t, filepath.Join(h.library, "kit", "a:b=c.md")), "a, upstream\n")
	equal(t, "notes.md", fileBody(t, filepath.Join(h.library, "kit", "notes.md")), "uno\n")
}

// TestSkillResolveEachSideOfEveryKind resolves kit's merge every way a file
// can be: each hunk of a text file to mine, theirs or both, mine then
// theirs; a binary file to either side; a file one side deleted to the
// side that kept it or to the one that deleted it, which removes it.
//
// Each choice is made in the checkout and staged, as resolving a file in
// git is: the file holds what was chosen, and git's index has it at stage
// 0, or not at all where the side chosen deleted it, and no longer lists
// it as unmerged. A later skill resolve shows the files left, and skill
// list shows the merge pending. Resolving the last one completes the merge
// in the same run, whether the files were resolved over two runs or all
// in one: the library directory holds the merged version, the import
// branch is at the update, and the candidate and the checkout are gone,
// git's registration of the checkout included.
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
			name: "in two runs, theirs first",
			runs: [][]string{
				{"notes.md:1=theirs", "notes.md:2=both", "guide.md:1=theirs", "logo.bin:1=theirs", "gone.md:1=theirs"},
				{"kept.md:1=theirs"},
			},
			resolved: map[string]string{
				"notes.md": "uno\ntwo\nthree\nfour\nFIVE\ncinco\n", "guide.md": "guide, upstream\n",
				"logo.bin": "\x00upstream", "gone.md": "", "kept.md": "kept, upstream\n",
			},
		},
		{
			name:     "in one run",
			runs:     [][]string{{"notes.md:1=mine", "notes.md:2=theirs", "guide.md:1=both", "logo.bin:1=mine", "gone.md:1=mine", "kept.md:1=theirs"}},
			resolved: kitResolved,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, _, _ := resolveHarness(t)
			tip, candidate := h.ref(lineage.ManagedRef("kit")), h.ref(lineage.CandidateRef("kit"))

			if len(c.runs) == 2 {
				out := resolveKit(t, h, 0, c.runs[0]...)
				equal(t, "summary of the first run", h.one(out.stdout, "result")["summary"], "resolved 4 files in the merge of kit, 1 file left to resolve")
				equal(t, "what the first run left", conflictFiles(h.one(out.stdout, "conflict")), "kept.md:whole")
				if got := h.eventsOfType(out.stdout, "library_skill"); len(got) != 0 {
					t.Errorf("a run that left the merge pending reported %v", got)
				}
				equal(t, "what is unmerged after the first run", unmerged(t, h, "kit"), "1 kit-dir/kept.md\n3 kit-dir/kept.md")
				for path, body := range c.resolved {
					switch {
					case path == "kept.md":
					case body == "":
						nothingAt(t, path+" in the checkout", inCheckout(h, path))
						equal(t, path+" in the index", stagedAt(t, h, path), "")
					default:
						equal(t, path+" in the checkout", fileBody(t, inCheckout(h, path)), body)
						if staged := stagedAt(t, h, path); !strings.HasSuffix(staged, " 0\tkit-dir/"+path) {
							t.Errorf("%s in the index is %q, not one entry at stage 0", path, staged)
						}
					}
				}
				equal(t, "the import branch after the first run", h.ref(lineage.ManagedRef("kit")), tip)
				equal(t, "pending_merge", h.listed("kit")["pending_merge"], true)
				equal(t, "what is left, shown", conflictFiles(h.one(h.mustRun("--json", "skill", "resolve", "kit").stdout, "conflict")), "kept.md:whole")
			}

			out := resolveKit(t, h, 0, c.runs[len(c.runs)-1]...)
			if got := h.eventsOfType(out.stdout, "conflict"); len(got) != 0 {
				t.Errorf("the run that completed the merge reported %v", got)
			}
			contains(t, "summary", h.one(out.stdout, "result")["summary"].(string),
				"resolved kit and updated it from "+upstreamOf(h, tip)+" to "+upstreamOf(h, candidate)+", 2 copy placements refreshed")
			completedKit(t, h, candidate, kitTree(t, h, c.resolved))
			ev := h.one(out.stdout, "library_skill")
			equal(t, "state", ev["state"], stateModified)
			for _, field := range []string{"candidate", "pending_merge"} {
				if _, ok := ev[field]; ok {
					t.Errorf("the completed skill carries %s: %v", field, ev[field])
				}
			}
			sameEvent(t, "the completion's library_skill and skill list's", ev, h.listed("kit"))
		})
	}
}

// TestSkillResolveKeepsAnEditedCopyThroughCompletion: completing the merge
// refreshes the copy that still holds the version installed and keeps,
// byte for byte, the one edited where it is, which a warning names and the
// result counts as skipped.
func TestSkillResolveKeepsAnEditedCopyThroughCompletion(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	claude := filepath.Join(h.home, ".claude", "skills", "kit")
	cursor := filepath.Join(h.home, ".cursor", "skills", "kit")
	editCopy(t, cursor)
	edited := onDisk(t, cursor)
	candidate := h.ref(lineage.CandidateRef("kit"))

	out := resolveKit(t, h, 0, append(slices.Clone(kitSides), "kept.md:1=theirs")...)
	contains(t, "summary", h.one(out.stdout, "result")["summary"].(string), ", 1 copy placement refreshed, 1 placement skipped")
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"),
		keptCopyWarning(cursor, "kit", "agentx skill remove kit --from cursor", "agentx skill place kit --to cursor --copy"))
	want := kitTree(t, h, kitResolved)
	completedKit(t, h, candidate, want)
	sameTree(t, "claude's copy", libraryTree(t, claude), want)
	equal(t, "cursor's copy", onDisk(t, cursor), edited)
	for _, dir := range []string{h.library, filepath.Dir(claude), filepath.Dir(cursor)} {
		equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
	}
}

// TestSkillResolveKeepsANewerUpdate: a check that finds a newer version
// while the merge is pending moves the candidate. Completing the merge
// moves the import branch to the update the merge merged, the one the
// trailer of its MERGE_MSG names, and never to the candidate, which stays
// as the skill's next update: skill list shows it, and the text says so.
func TestSkillResolveKeepsANewerUpdate(t *testing.T) {
	t.Parallel()
	h, s, _ := resolveHarness(t)
	_, theirs, msg := mergeState(t, h, "kit")
	target, ok := lineage.BaseOf(msg)
	equal(t, "the import MERGE_MSG names", target+fmt.Sprint(ok), theirs+"true")
	s.write("skills/kit-dir/usage.md", "usage, revised upstream\n")
	third := s.commit("third version")
	h.mustRun("skill", "check")
	newer := h.ref(lineage.CandidateRef("kit"))
	if newer == theirs || newer == "" {
		t.Fatalf("the check left the candidate at %q", newer)
	}

	out := resolveKit(t, h, 0, append(slices.Clone(kitSides), "kept.md:1=theirs")...)
	contains(t, "summary", h.one(out.stdout, "result")["summary"].(string), "; the newer update the last check found stays for the next update")
	equal(t, "the import branch", h.ref(lineage.ManagedRef("kit")), theirs)
	equal(t, "the candidate", h.ref(lineage.CandidateRef("kit")), newer)
	noCheckout(t, h, "kit")
	sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "kit")), kitTree(t, h, kitResolved))
	equal(t, "the update listed", h.listed("kit")["candidate"].(map[string]any)["upstream_commit"], third)
	contains(t, "skill list", h.mustRun("skill", "list").stdout, updateAvailable)
}

// TestSkillResolveMergesAnEditMadeMeanwhile: an edit of the library
// directory made while the merge was pending, by any tool, is never lost.
// Completing commits the finished merge anyway, then commits the library
// directory as it is on the mine the merge started from and merges the
// two. An edit the finished merge does not overlap merges cleanly, and the
// merge completes in the same run with the edit and every resolution in
// it; a copy placed from the library directory once it was edited is
// refreshed with the merge. An edit that overlaps a resolution starts the
// merge anew in the checkout, exit code 4: HEAD is the library directory
// as it is, HEAD^ the mine before, MERGE_HEAD the finished merge, whose
// parents are that mine and the update, and only the overlap is unmerged,
// every other resolution staged. Resolving it completes the merge with
// both edits.
func TestSkillResolveMergesAnEditMadeMeanwhile(t *testing.T) {
	t.Parallel()
	t.Run("an edit the finished merge does not overlap", func(t *testing.T) {
		t.Parallel()
		h, _, _ := resolveHarness(t)
		candidate := h.ref(lineage.CandidateRef("kit"))
		resolveKit(t, h, 0, kitSides...)
		editLibrary(t, h, "kit", "usage.md", "usage, edited meanwhile\n")
		claude := filepath.Join(h.home, ".claude", "skills", "kit")
		h.mustRun("skill", "remove", "kit", "--from", "claude-code")
		h.mustRun("skill", "place", "kit", "--to", "claude-code", "--copy")

		out := resolveKit(t, h, 0, "kept.md:1=theirs")
		summary := h.one(out.stdout, "result")["summary"].(string)
		contains(t, "summary", summary, "resolved kit, merged the edits made to it meanwhile and updated it from ")
		contains(t, "summary", summary, ", 2 copy placements refreshed")
		want := kitTree(t, h, kitResolved)
		want["usage.md"] = "usage, edited meanwhile\n"
		completedKit(t, h, candidate, want)
		sameTree(t, "claude's copy", libraryTree(t, claude), want)
	})
	t.Run("an edit that overlaps a resolution", func(t *testing.T) {
		t.Parallel()
		h, _, updated := resolveHarness(t)
		tip, candidate := h.ref(lineage.ManagedRef("kit")), h.ref(lineage.CandidateRef("kit"))
		resolveKit(t, h, 0, kitSides...)
		editLibrary(t, h, "kit", "notes.md", "ONE\ntwo\nthree\nfour\nFIVE, again\n")
		editLibrary(t, h, "kit", "usage.md", "usage, edited meanwhile\n")
		library := onDisk(t, h.library)

		out := resolveKit(t, h, 4, "kept.md:1=theirs")
		e := h.one(out.stdout, "error")
		equal(t, "code", e["code"], "pending_merge")
		equal(t, "message", e["message"], "kit was edited while its merge was pending, and merging the edit into the finished merge conflicts in 1 file, so the merge is still pending")
		equal(t, "hint", e["hint"], "run 'agentx skill resolve kit' to see what is left")
		ev := h.one(out.stdout, "conflict")
		equal(t, "what conflicts anew", conflictFiles(ev), "notes.md:1")
		equal(t, "the hunk", hunkOf(t, ev, "notes.md", 1), "FIVE, again\n|FIVE\n|cinco\n")
		head, finished, _ := mergeState(t, h, "kit")
		equal(t, "the event's versions", fmt.Sprint(ev["base"], " ", ev["mine"], " ", ev["theirs"]), updated["mine"].(string)+" "+head+" "+finished)
		equal(t, "HEAD^", strings.TrimSpace(checkoutGit(t, h, "kit", "rev-parse", "HEAD^")), updated["mine"])
		equal(t, "the parents of MERGE_HEAD", strings.TrimSpace(checkoutGit(t, h, "kit", "rev-parse", "MERGE_HEAD^@")), updated["mine"].(string)+"\n"+candidate)
		equal(t, "what is unmerged", unmerged(t, h, "kit"), "1 kit-dir/notes.md\n2 kit-dir/notes.md\n3 kit-dir/notes.md")
		equal(t, "guide.md, staged", fileBody(t, inCheckout(h, "guide.md")), kitResolved["guide.md"])
		equal(t, "usage.md, merged", fileBody(t, inCheckout(h, "usage.md")), "usage, edited meanwhile\n")
		equal(t, "the library", onDisk(t, h.library), library)
		equal(t, "the import branch", h.ref(lineage.ManagedRef("kit")), tip)
		equal(t, "the candidate", h.ref(lineage.CandidateRef("kit")), candidate)
		equal(t, "what is left, shown", filesOf(t, h.one(h.mustRun("--json", "skill", "resolve", "kit").stdout, "conflict")), filesOf(t, ev))

		resolveKit(t, h, 0, "notes.md:1=mine")
		want := kitTree(t, h, kitResolved)
		want["notes.md"], want["usage.md"] = "ONE\ntwo\nthree\nfour\nFIVE, again\n", "usage, edited meanwhile\n"
		completedKit(t, h, candidate, want)
	})
}

// TestSkillResolveCompletesAPlainGitCommitInTheCheckout: the checkout is
// an ordinary merge in progress, so it can be resolved with plain git,
// staged or committed, and skill resolve with no flag then completes it,
// committing it first when nothing did.
func TestSkillResolveCompletesAPlainGitCommitInTheCheckout(t *testing.T) {
	t.Parallel()
	for _, commit := range []bool{false, true} {
		t.Run(fmt.Sprintf("committed %v", commit), func(t *testing.T) {
			t.Parallel()
			h, _, _ := resolveHarness(t)
			candidate := h.ref(lineage.CandidateRef("kit"))
			checkoutGit(t, h, "kit", "checkout", "--theirs", "--", "kit-dir/notes.md", "kit-dir/guide.md", "kit-dir/logo.bin", "kit-dir/kept.md")
			checkoutGit(t, h, "kit", "add", "--", "kit-dir/notes.md", "kit-dir/guide.md", "kit-dir/logo.bin", "kit-dir/kept.md")
			checkoutGit(t, h, "kit", "rm", "-q", "--", "kit-dir/gone.md")
			if commit {
				checkoutGit(t, h, "kit", "commit", "-q", "--no-edit")
			}

			out := h.mustRun("--json", "skill", "resolve", "kit")
			contains(t, "summary", h.one(out.stdout, "result")["summary"].(string), "resolved kit and updated it from ")
			completedKit(t, h, candidate, kitTree(t, h, map[string]string{
				"notes.md": kitTheirs["notes.md"], "guide.md": kitTheirs["guide.md"], "logo.bin": kitTheirs["logo.bin"], "kept.md": kitTheirs["kept.md"],
			}))
		})
	}
}

// TestSkillResolveCommitsTheMergeAsYou: the commit that completes the
// merge is the user's, with their user.name and user.email and the time
// it is made, or agentx's identity where they set none, and carries the
// message the update wrote in MERGE_MSG, the trailer naming the import the
// branch moves to included; its parents are mine and the update. A
// completion killed once it committed leaves the merge committed, and the
// next skill resolve completes it without committing again.
func TestSkillResolveCommitsTheMergeAsYou(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, config, who string
	}{
		{name: "with an identity", config: "[user]\n\tname = Ada Lovelace\n\temail = ada@example.com\n", who: "Ada Lovelace <ada@example.com>"},
		{name: "with none", who: gitx.Identity},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, _, _ := resolveHarness(t)
			// The user's configuration is the harness's alone, whatever the
			// machine's system configuration or the test's own environment
			// holds, the killed child's included.
			h.env["GIT_CONFIG_GLOBAL"] = filepath.Join(h.home, ".gitconfig")
			h.env["GIT_CONFIG_NOSYSTEM"] = "1"
			if c.config != "" {
				writeFile(t, filepath.Join(h.home, ".gitconfig"), c.config)
			}
			candidate := h.ref(lineage.CandidateRef("kit"))
			mine, theirs, msg := mergeState(t, h, "kit")
			resolveKit(t, h, 0, kitSides...)
			out := killedChild(t, h, "TestResolveChildProcess", resolveChildEnv, "kit|--hunk|kept.md:1=theirs", `
case " $* " in
*" commit "*)
	%GIT% "$@"
	kill -9 $PPID
	exit 1
	;;
esac
exec %GIT% "$@"
`)
			equal(t, "the parents", strings.TrimSpace(checkoutGit(t, h, "kit", "rev-parse", "HEAD^@")), mine+"\n"+theirs)
			nothingAt(t, "MERGE_HEAD once committed", strings.TrimSpace(checkoutGit(t, h, "kit", "rev-parse", "--git-path", "MERGE_HEAD")))
			equal(t, "the author", checkoutGit(t, h, "kit", "log", "-1", "--format=%an <%ae>"), c.who+"\n")
			equal(t, "the committer", checkoutGit(t, h, "kit", "log", "-1", "--format=%cn <%ce>"), c.who+"\n")
			if at := strings.TrimSpace(checkoutGit(t, h, "kit", "log", "-1", "--format=%at")); at == strings.Fields(gitx.FixedDate)[0] {
				t.Errorf("the commit has agentx's fixed date %s", at)
			}
			equal(t, "the message", checkoutGit(t, h, "kit", "log", "-1", "--format=%B"), msg+"\n")
			equal(t, "the import branch once killed", h.ref(lineage.ManagedRef("kit")), strings.TrimSpace(checkoutGit(t, h, "kit", "rev-parse", "HEAD^^")))
			if strings.Contains(out, "resolved kit") {
				t.Fatalf("the completion was not killed after it committed:\n%s", out)
			}

			next := h.mustRun("--json", "skill", "resolve", "kit")
			contains(t, "summary", h.one(next.stdout, "result")["summary"].(string), "resolved kit and updated it from ")
			completedKit(t, h, candidate, kitTree(t, h, kitResolved))
		})
	}
}

// TestSkillResolveKeepsIgnoredFilesThroughCompletion: what git ignores in
// the library directory, a .DS_Store and a directory its own .gitignore
// names, is no part of the merge and is carried into the library directory
// the merge completes to, but for a file the update adds at a path the
// library ignores, which is the update's.
func TestSkillResolveKeepsIgnoredFilesThroughCompletion(t *testing.T) {
	t.Parallel()
	h := clashHarness(t, nil,
		func(s *sourceRepo) { s.write("skills/kit-dir/cache/data.txt", "upstream data\n") },
		func(lib string) {
			writeFile(t, filepath.Join(lib, ".gitignore"), "build/\ncache/\n")
			writeFile(t, filepath.Join(lib, ".DS_Store"), "finder data\n")
			writeFile(t, mkdirs(t, filepath.Join(lib, "build"), "out.txt"), "built here\n")
			writeFile(t, mkdirs(t, filepath.Join(lib, "cache"), "data.txt"), "local cache\n")
		})
	candidate := h.ref(lineage.CandidateRef("kit"))
	resolveKit(t, h, 0, "notes.md:1=mine")
	completedKit(t, h, candidate, map[string]string{
		"SKILL.md":       fileBody(t, filepath.Join(h.library, "kit", "SKILL.md")),
		"notes.md":       "ONE\n",
		".gitignore":     "build/\ncache/\n",
		".DS_Store":      "finder data\n",
		"build/out.txt":  "built here\n",
		"cache/data.txt": "upstream data\n",
	})
	equal(t, "state", h.listed("kit")["state"], stateModified)
}

// TestSkillResolveRefusesToCompleteOnAMovedImportBranch: an import branch
// moved while the merge was pending, which only something outside agentx
// does, keeps the merge from completing. Showing it warns of that and
// changes nothing; the run that resolves its last file stages that file
// and refuses to complete, exit code 6, leaving the checkout with every
// resolution staged and the library, the branch and the candidate as they
// were; and giving the merge up then works, and leaves the branch where it
// was moved.
func TestSkillResolveRefusesToCompleteOnAMovedImportBranch(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	tip, candidate := h.ref(lineage.ManagedRef("kit")), h.ref(lineage.CandidateRef("kit"))
	resolveKit(t, h, 0, kitSides...)
	moved := h.accountGitInput("", "commit-tree", tip+"^{tree}", "-m", "written again", "-m", h.accountGit("log", "-1", "--format=%B", tip))
	h.accountGit("update-ref", lineage.ManagedRef("kit"), moved, tip)
	library, placed := onDisk(t, h.library), onDisk(t, h.home)
	const refusal = "the import branch refs/heads/managed/kit moved outside agentx while the merge was pending, so it cannot be completed"
	const hint = "run 'agentx skill resolve kit --abort' to give it up"

	shown := h.mustRun("--json", "skill", "resolve", "kit")
	equal(t, "the warning", strings.Join(warnings(h, shown.stderr), "\n"), refusal+"; "+hint)
	equal(t, "what is left, shown", conflictFiles(h.one(shown.stdout, "conflict")), "kept.md:whole")

	out := resolveKit(t, h, 6, "kept.md:1=theirs")
	e := h.one(out.stdout, "error")
	equal(t, "message", e["message"], refusal)
	equal(t, "hint", e["hint"], hint)
	equal(t, "what is unmerged", unmerged(t, h, "kit"), "")
	equal(t, "kept.md, staged", fileBody(t, inCheckout(h, "kept.md")), kitResolved["kept.md"])
	equal(t, "guide.md, staged", fileBody(t, inCheckout(h, "guide.md")), kitResolved["guide.md"])
	equal(t, "MERGE_HEAD", strings.TrimSpace(checkoutGit(t, h, "kit", "rev-parse", "MERGE_HEAD")), candidate)
	equal(t, "the library", onDisk(t, h.library), library)
	equal(t, "the placements", onDisk(t, h.home), placed)
	equal(t, "the import branch", h.ref(lineage.ManagedRef("kit")), moved)
	equal(t, "the candidate", h.ref(lineage.CandidateRef("kit")), candidate)
	equal(t, "journals", journalCount(t, h), 0)
	again := h.run("--json", "skill", "resolve", "kit")
	equal(t, "exit of a run with nothing unmerged", again.exit, 6)
	equal(t, "its message", h.one(again.stdout, "error")["message"], refusal)

	h.mustRun("skill", "resolve", "kit", "--abort")
	noCheckout(t, h, "kit")
	equal(t, "the import branch once given up", h.ref(lineage.ManagedRef("kit")), moved)
	equal(t, "the library once given up", onDisk(t, h.library), library)
}

// TestSkillResolveAbortLeavesTheLibraryAsItWas: giving the merge up removes
// its checkout and git's registration of it, whatever was resolved there,
// and nothing else: the library, every copy, the import branch and the
// candidate are byte for byte as they were, and the skill no longer has a
// merge pending. A second --abort finds none, and the update conflicts
// again as it did.
func TestSkillResolveAbortLeavesTheLibraryAsItWas(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	resolveKit(t, h, 0, kitSides...)
	tip, candidate := h.ref(lineage.ManagedRef("kit")), h.ref(lineage.CandidateRef("kit"))
	library, placed, before := onDisk(t, h.library), onDisk(t, h.home), mutationVersion(t, h)

	out := h.run("--json", "skill", "resolve", "kit", "--abort")
	equal(t, "exit", out.exit, 0)
	equal(t, "summary", h.one(out.stdout, "result")["summary"], "gave up the merge of kit; the library directory is as it was")
	ev := h.one(out.stdout, "library_skill")
	if _, ok := ev["pending_merge"]; ok {
		t.Errorf("the skill still carries pending_merge: %v", ev["pending_merge"])
	}
	noCheckout(t, h, "kit")
	equal(t, "what is left in the merges directory", strings.Join(hiddenEntries(t, filepath.Join(h.agentx, "merges")), " "), "")
	equal(t, "the import branch", h.ref(lineage.ManagedRef("kit")), tip)
	equal(t, "the candidate", h.ref(lineage.CandidateRef("kit")), candidate)
	equal(t, "the library", onDisk(t, h.library), library)
	equal(t, "the placements", onDisk(t, h.home), placed)
	equal(t, "mutations", mutationVersion(t, h), before+1)
	equal(t, "journals", journalCount(t, h), 0)
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), "")
	if strings.Contains(h.mustRun("skill", "list").stdout, "merge pending") {
		t.Error("skill list still shows a merge pending")
	}

	again := h.run("--json", "skill", "resolve", "kit", "--abort")
	equal(t, "exit of a second abort", again.exit, 6)
	equal(t, "message of a second abort", h.one(again.stdout, "error")["message"], "kit has no merge pending")
	equal(t, "the update once given up conflicts again", h.run("skill", "update", "kit").exit, 4)
}

// TestSkillResolveRefusesACheckoutThatIsNoLongerAMerge: a checkout whose
// merge was given up inside it with plain git, git merge --abort, is no
// longer a merge agentx can resolve or complete: every run but --abort is
// exit code 6, with the hint that gives it up, and --abort still does.
func TestSkillResolveRefusesACheckoutThatIsNoLongerAMerge(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	checkoutGit(t, h, "kit", "merge", "--abort")
	library := onDisk(t, h.library)
	for _, args := range [][]string{{}, {"--hunk", "logo.bin:1=mine"}} {
		what := strings.Join(args, " ")
		out := h.run(append([]string{"--json", "skill", "resolve", "kit"}, args...)...)
		equal(t, what+": exit", out.exit, 6)
		e := h.one(out.stdout, "error")
		equal(t, what+": message", e["message"], "the pending merge of kit in "+pendingCheckout(h, "kit")+" is no longer a merge in progress")
		equal(t, what+": hint", e["hint"], "run 'agentx skill resolve kit --abort' to give it up")
	}
	equal(t, "the library", onDisk(t, h.library), library)
	h.mustRun("skill", "resolve", "kit", "--abort")
	noCheckout(t, h, "kit")
}

// TestSkillResolveReadsAFileRemovedFromTheCheckout: a text file that
// conflicts, removed from the checkout with plain rm rather than git rm,
// conflicts whole, and a side chosen for it, or for another file, resolves
// it as for any file that conflicts whole.
func TestSkillResolveReadsAFileRemovedFromTheCheckout(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	remove(t, inCheckout(h, "notes.md"))

	out := h.mustRun("--json", "skill", "resolve", "kit")
	equal(t, "the conflicts", conflictFiles(h.one(out.stdout, "conflict")), "gone.md:whole,guide.md:1,kept.md:whole,logo.bin:binary,notes.md:whole")

	resolveKit(t, h, 0, "guide.md:1=theirs")
	resolveKit(t, h, 0, "notes.md:1=mine")
	equal(t, "notes.md in the checkout", fileBody(t, inCheckout(h, "notes.md")), kitMine["notes.md"])
	if staged := stagedAt(t, h, "notes.md"); !strings.HasSuffix(staged, " 0\tkit-dir/notes.md") {
		t.Errorf("notes.md in the index is %q, not one entry at stage 0", staged)
	}
}

// TestSkillResolveRefusesToCompleteWhatItCannotReplace: completing the
// merge replaces the library directory, so one it cannot replace as it is
// is refused with exit code 6 and nothing written: a library entry that is
// a symlink, and one that is gone. So is a merge that resolves to a
// directory with no SKILL.md, which would take the skill out of every
// client. The checkout stays with every resolution staged, the library,
// every copy, the import branch and the candidate are left as they were,
// and the run can be made again once that is put right, or the merge
// given up.
func TestSkillResolveRefusesToCompleteWhatItCannotReplace(t *testing.T) {
	t.Parallel()
	// refused completes kit's merge, with every file but kept.md resolved
	// already, in a run that is refused, and checks that it wrote nothing
	// but kept.md's resolution.
	refused := func(t *testing.T, h *harness) jsonEvent {
		t.Helper()
		refs := h.accountGit("for-each-ref", "--format=%(refname) %(objectname)")
		placed := onDisk(t, h.home)
		out := resolveKit(t, h, 6, "kept.md:1=theirs")
		equal(t, "the refs", h.accountGit("for-each-ref", "--format=%(refname) %(objectname)"), refs)
		equal(t, "the library and its copies", onDisk(t, h.home), placed)
		equal(t, "what is unmerged", unmerged(t, h, "kit"), "")
		equal(t, "kept.md, staged", fileBody(t, inCheckout(h, "kept.md")), kitResolved["kept.md"])
		equal(t, "journals", journalCount(t, h), 0)
		equal(t, "what is left beside the library", strings.Join(hiddenEntries(t, h.library), " "), "")
		return h.one(out.stdout, "error")
	}
	t.Run("a library entry that is a symlink", func(t *testing.T) {
		t.Parallel()
		h, _, _ := resolveHarness(t)
		candidate := h.ref(lineage.CandidateRef("kit"))
		resolveKit(t, h, 0, kitSides...)
		lib := filepath.Join(h.library, "kit")
		moved := filepath.Join(t.TempDir(), "kit")
		if err := os.Rename(lib, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(moved, lib); err != nil {
			t.Fatal(err)
		}
		e := refused(t, h)
		equal(t, "message", e["message"], quotedPath(lib)+" is a symlink to "+quotedPath(moved)+
			"; completing the merge replaces the library directory and would drop the link without touching the files it leads to")
		equal(t, "hint", e["hint"], "replace the link with the directory it points to, then run 'agentx skill resolve kit' again")

		remove(t, lib)
		if err := os.Rename(moved, lib); err != nil {
			t.Fatal(err)
		}
		h.mustRun("skill", "resolve", "kit")
		completedKit(t, h, candidate, kitTree(t, h, kitResolved))
	})
	t.Run("a merge that resolves to no SKILL.md", func(t *testing.T) {
		t.Parallel()
		h, _, _ := resolveHarness(t)
		resolveKit(t, h, 0, kitSides...)
		checkoutGit(t, h, "kit", "rm", "-q", "--", "kit-dir/SKILL.md")
		e := refused(t, h)
		equal(t, "message", e["message"], "the merge of kit resolves to a directory with no SKILL.md, which no client would read as a skill, so nothing was changed")
		equal(t, "hint", e["hint"], "put SKILL.md back in the merge in "+quotedPath(pendingCheckout(h, "kit"))+" with git, or run 'agentx skill resolve kit --abort' to give the merge up")
		equal(t, "MERGE_HEAD", strings.TrimSpace(checkoutGit(t, h, "kit", "rev-parse", "MERGE_HEAD")), h.ref(lineage.CandidateRef("kit")))
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
		noCheckout(t, h, "kit")
	})
}

// TestSkillResolveWarnsOfAnUpdateThatNamesTheSkillOtherwise: a completed
// merge whose update names the skill otherwise in its SKILL.md keeps the
// library name, with the warning an update gives.
func TestSkillResolveWarnsOfAnUpdateThatNamesTheSkillOtherwise(t *testing.T) {
	t.Parallel()
	h := clashHarness(t, nil,
		func(s *sourceRepo) {
			s.write("skills/kit-dir/SKILL.md", skill("kit-renamed", "A skill whose update conflicts with its edits"))
		},
		func(string) {})
	out := resolveKit(t, h, 0, "notes.md:1=theirs")
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), `kit: the update names the skill "kit-renamed"; updating it keeps the name kit`)
	equal(t, "the library's SKILL.md", fileBody(t, filepath.Join(h.library, "kit", "SKILL.md")), skill("kit-renamed", "A skill whose update conflicts with its edits"))
	equal(t, "the name listed", h.listed("kit")["name"], "kit")
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
// side has none, whether the other side has one or not: in the checkout,
// and in the library once the merge completes. Both sides give mine's last
// line a newline of its own, so that theirs starts a line, and end as
// theirs does. So do the parts of a file whose lines end CRLF, where the
// line ending both gives mine's last line is a CRLF too.
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
				equal(t, "tail.md in the checkout", fileBody(t, inCheckout(h, "tail.md")), c.want[side])
				resolveKit(t, h, 0, "notes.md:1=theirs")
				noCheckout(t, h, "kit")
				equal(t, "tail.md in the library", fileBody(t, filepath.Join(h.library, "kit", "tail.md")), c.want[side])
			})
		}
	}
}

// TestSkillResolveKeepsTheModeEitherSideGaveAFile: a file resolved by its
// parts, or in an editor, takes the mode one side changed, as git merges
// modes. An update that makes the file executable keeps it so when the
// line both sides changed is resolved to mine: in the checkout's index, in
// the library once the merge completes, and on the import branch. An
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
		equal(t, "notes.md in the index", strings.Fields(stagedAt(t, h, "notes.md"))[0], "100755")
		resolveKit(t, h, 0, "other.md:1=theirs")
		lib := filepath.Join(h.library, "kit")
		noCheckout(t, h, "kit")
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
		noCheckout(t, h, "kit")
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
		noCheckout(t, h, "kit")
		equal(t, "notes.md", fileBody(t, filepath.Join(lib, "notes.md")), "uno\n")
		equal(t, "notes.md executable", executable(t, filepath.Join(lib, "notes.md")), true)
		equal(t, "notes.md on the import branch", modeIn(h, lineage.ManagedRef("kit"), "notes.md"), "100644")
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
	candidate := h.ref(lineage.CandidateRef("kit"))
	h.mustRun("skill", "remove", "kit", "--from", "claude-code")
	h.mustRun("skill", "place", "kit", "--to", "claude-code", "--copy")
	sameTree(t, "claude's copy, placed while the merge is pending", libraryTree(t, claude), libraryTree(t, lib))
	editCopy(t, cursor)
	edited := libraryTree(t, cursor)
	resolveKit(t, h, 0, kitSides...)

	out := resolveKit(t, h, 0, "kept.md:1=theirs")
	contains(t, "summary", h.one(out.stdout, "result")["summary"].(string), ", 1 copy placement refreshed, 1 placement skipped")
	want := kitTree(t, h, kitResolved)
	completedKit(t, h, candidate, want)
	sameTree(t, "claude's copy", libraryTree(t, claude), want)
	sameTree(t, "cursor's copy", libraryTree(t, cursor), edited)
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
			noCheckout(t, h, "kit")
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
	completedKit(t, h, candidate, kitTree(t, h, kitResolved))
}

// TestSkillResolveOfAMergeWhosePathsDifferInCase: a merge resolved to a
// version that holds readme.md, a file the user added, and README.md, one
// the update adds, completes where the library tells the two apart. A file
// system that folds case, as macOS's by default does, cannot hold both in
// the merge's checkout either, so there it is git's to refuse the merge.
func TestSkillResolveOfAMergeWhosePathsDifferInCase(t *testing.T) {
	t.Parallel()
	h, s, _ := updateHarness(t)
	lib := filepath.Join(h.library, "alpha")
	if foldsCaseAt(t, filepath.Join(lib, "SKILL.md")) {
		t.Skip("this file system folds case")
	}
	editLibrary(t, h, "alpha", "notes.md", editedNotes)
	editLibrary(t, h, "alpha", "readme.md", "my own readme\n")
	s.write("skills/alpha-dir/README.md", "the upstream readme\n")
	newVersion(t, s)
	h.mustRun("skill", "check")
	if out := h.run("--json", "skill", "update", "alpha"); out.exit != 4 {
		t.Fatalf("the update that should conflict: exit %d\n%s%s", out.exit, out.stdout, out.stderr)
	}
	candidate := h.ref(lineage.CandidateRef("alpha"))

	h.mustRun("skill", "resolve", "alpha", "--hunk", "notes.md:1=theirs")
	equal(t, "readme.md", fileBody(t, filepath.Join(lib, "readme.md")), "my own readme\n")
	equal(t, "README.md", fileBody(t, filepath.Join(lib, "README.md")), "the upstream readme\n")
	equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), candidate)
	noCheckout(t, h, "alpha")
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

// idleEditor is an editor that saves nothing.
func idleEditor(t *testing.T) string {
	t.Helper()
	return editorStub(t, "idle-editor", "exit 0\n")
}

// TestSkillResolveInAnEditor opens the text files left to resolve in the
// user's editor, a stub named by EDITOR, in place in the checkout, with
// the markers the merge wrote.
//
// An editor that takes the markers out of every file stages them and
// completes the merge; one that leaves them stages nothing, and the merge
// stays pending; one that takes them out of one file stages that one. A
// file named after --editor is the only one opened. A text file whose
// markers are gone, saved but not staged, is opened and staged like any
// other, and so is one resolved to a quote, a line starting with "> ",
// which is no marker. A file that conflicts whole cannot be edited, which is exit code
// 1 when it is named and exit code 6 when every file left is one. An
// editor that fails stages nothing, and what it saved stays in the
// checkout.
func TestSkillResolveInAnEditor(t *testing.T) {
	t.Parallel()
	t.Run("an editor that resolves every file", func(t *testing.T) {
		t.Parallel()
		h, _, _ := resolveHarness(t)
		candidate := h.ref(lineage.CandidateRef("kit"))
		resolveKit(t, h, 0, kitWholeFiles...)
		given := fileBody(t, inCheckout(h, "guide.md")) + fileBody(t, inCheckout(h, "notes.md"))
		stub, args, seen := resolvingEditor(t)
		h.env["EDITOR"] = stub

		out := h.run("--json", "skill", "resolve", "kit", "--editor")
		equal(t, "exit", out.exit, 0)
		contains(t, "summary", h.one(out.stdout, "result")["summary"].(string), "resolved kit and updated it from ")
		equal(t, "what the editor opened", fileBody(t, args), inCheckout(h, "guide.md")+"\n"+inCheckout(h, "notes.md")+"\n")
		equal(t, "what the editor was given", fileBody(t, seen), given)
		contains(t, "what the editor was given", given, "\nguide, mine\n|||||||")
		resolved := map[string]string{"notes.md": "resolved in the editor\n", "guide.md": "resolved in the editor\n", "logo.bin": "\x00mine", "gone.md": "gone, mine\n", "kept.md": "kept, upstream\n"}
		completedKit(t, h, candidate, kitTree(t, h, resolved))
	})
	t.Run("an editor that leaves the markers", func(t *testing.T) {
		t.Parallel()
		h, _, _ := resolveHarness(t)
		index := unmerged(t, h, "kit")
		h.env["EDITOR"] = idleEditor(t)

		out := h.run("--json", "skill", "resolve", "kit", "--editor")
		equal(t, "exit", out.exit, 0)
		equal(t, "summary", h.one(out.stdout, "result")["summary"], "resolved nothing in the merge of kit: 5 files left to resolve")
		equal(t, "what is left", conflictFiles(h.one(out.stdout, "conflict")), kitConflicts)
		equal(t, "what is unmerged", unmerged(t, h, "kit"), index)
		equal(t, "journals", journalCount(t, h), 0)
	})
	t.Run("an editor that leaves markers in one file", func(t *testing.T) {
		t.Parallel()
		h, _, _ := resolveHarness(t)
		h.env["EDITOR"] = editorStub(t, "careful-editor", `for f in "$@"; do
	case "$f" in
	*/guide.md) printf 'resolved in the editor\n' > "$f" ;;
	*) printf 'MY CAREFUL WORK\n' >> "$f" ;;
	esac
done
`)
		typed := fileBody(t, inCheckout(h, "notes.md")) + "MY CAREFUL WORK\n"
		out := h.run("--json", "skill", "resolve", "kit", "--editor")
		equal(t, "exit", out.exit, 0)
		equal(t, "summary", h.one(out.stdout, "result")["summary"], "resolved guide.md in the merge of kit, 4 files left to resolve")
		equal(t, "what is left", conflictFiles(h.one(out.stdout, "conflict")), "gone.md:whole,kept.md:whole,logo.bin:binary,notes.md:2")
		if staged := stagedAt(t, h, "guide.md"); !strings.HasSuffix(staged, " 0\tkit-dir/guide.md") {
			t.Errorf("guide.md in the index is %q", staged)
		}
		equal(t, "notes.md as the editor saved it", fileBody(t, inCheckout(h, "notes.md")), typed)
	})
	t.Run("an editor that resolves a file to a quote", func(t *testing.T) {
		t.Parallel()
		h, _, _ := resolveHarness(t)
		h.env["EDITOR"] = editorStub(t, "quoting-editor", `printf '> a quote, which is no marker\n' > "$1"`+"\n")
		out := h.run("--json", "skill", "resolve", "kit", "--editor", "guide.md")
		equal(t, "exit", out.exit, 0)
		equal(t, "summary", h.one(out.stdout, "result")["summary"], "resolved guide.md in the merge of kit, 4 files left to resolve")
	})
	t.Run("a file named after --editor", func(t *testing.T) {
		t.Parallel()
		h, _, _ := resolveHarness(t)
		stub, args, _ := resolvingEditor(t)
		h.env["EDITOR"] = stub
		out := h.run("--json", "skill", "resolve", "kit", "--editor", "guide.md")
		equal(t, "exit", out.exit, 0)
		equal(t, "summary", h.one(out.stdout, "result")["summary"], "resolved guide.md in the merge of kit, 4 files left to resolve")
		equal(t, "what the editor opened", fileBody(t, args), inCheckout(h, "guide.md")+"\n")
	})
	t.Run("a text file saved with no markers and not staged", func(t *testing.T) {
		t.Parallel()
		h, _, _ := resolveHarness(t)
		writeFile(t, inCheckout(h, "guide.md"), "saved without markers\n")
		equal(t, "the file, shown", conflictFiles(h.one(h.mustRun("--json", "skill", "resolve", "kit").stdout, "conflict")), "gone.md:whole,guide.md:whole,kept.md:whole,logo.bin:binary,notes.md:2")
		h.env["EDITOR"] = idleEditor(t)
		out := h.run("--json", "skill", "resolve", "kit", "--editor", "guide.md")
		equal(t, "exit", out.exit, 0)
		equal(t, "summary", h.one(out.stdout, "result")["summary"], "resolved guide.md in the merge of kit, 4 files left to resolve")
		equal(t, "guide.md", fileBody(t, inCheckout(h, "guide.md")), "saved without markers\n")
	})
	t.Run("files no editor can open", func(t *testing.T) {
		t.Parallel()
		h, _, _ := resolveHarness(t)
		h.env["EDITOR"] = idleEditor(t)
		out := h.run("--json", "skill", "resolve", "kit", "--editor", "logo.bin")
		equal(t, "exit of a whole file named", out.exit, 1)
		e := h.one(out.stdout, "error")
		equal(t, "message", e["message"], "logo.bin conflicts as a whole file, so there is no text to edit")
		equal(t, "hint", e["hint"], "choose a side for it with --hunk logo.bin:1=mine or --hunk logo.bin:1=theirs")
		out = h.run("--json", "skill", "resolve", "kit", "--editor", "usage.md")
		equal(t, "exit of a file that does not conflict", out.exit, 1)
		equal(t, "its message", h.one(out.stdout, "error")["message"], "usage.md does not conflict in the merge of kit")

		resolveKit(t, h, 0, "notes.md:1=mine", "notes.md:2=mine", "guide.md:1=mine")
		out = h.run("--json", "skill", "resolve", "kit", "--editor")
		equal(t, "exit with only whole files left", out.exit, 6)
		equal(t, "its message", h.one(out.stdout, "error")["message"], "every file left to resolve in the merge of kit conflicts as a whole file, so there is no text to edit")
	})
	t.Run("an editor that fails", func(t *testing.T) {
		t.Parallel()
		h, _, _ := resolveHarness(t)
		index := unmerged(t, h, "kit")
		h.env["EDITOR"] = editorStub(t, "failing-editor", `for f in "$@"; do printf 'saved before failing\n' > "$f"; done
exit 3
`)
		out := h.run("--json", "skill", "resolve", "kit", "--editor")
		equal(t, "exit", out.exit, 6)
		e := h.one(out.stdout, "error")
		equal(t, "message", e["message"], "the editor "+h.env["EDITOR"]+" exited with status 3, so nothing was staged; what it saved stays in the merge")
		equal(t, "hint", e["hint"], "run 'agentx skill resolve kit --editor' again once the editor works, or set GIT_EDITOR or EDITOR to another")
		equal(t, "what is unmerged", unmerged(t, h, "kit"), index)
		equal(t, "guide.md as the editor saved it", fileBody(t, inCheckout(h, "guide.md")), "saved before failing\n")
	})
	t.Run("an editor whose sh is killed by a signal", func(t *testing.T) {
		t.Parallel()
		h, _, _ := resolveHarness(t)
		h.env["EDITOR"] = editorStub(t, "killing-editor", "kill -9 $PPID\n")
		out := h.run("--json", "skill", "resolve", "kit", "--editor")
		equal(t, "exit", out.exit, 6)
		equal(t, "message", h.one(out.stdout, "error")["message"], "the editor "+h.env["EDITOR"]+" was stopped by SIGKILL, so nothing was staged; what it saved stays in the merge")
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
// editor waits. Once it exits, what it resolved is staged.
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
}

// TestSkillResolveInAnEditorRefusesABusyLock: the run takes the lock before
// the editor opens, without waiting for it, to finish what an earlier
// command left. Held by another command, a scan included, the run is exit
// code 7 before any editor starts, and the merge is as it was.
func TestSkillResolveInAnEditorRefusesABusyLock(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	index := unmerged(t, h, "kit")
	ran := filepath.Join(t.TempDir(), "ran")
	h.env["EDITOR"] = editorStub(t, "marking-editor", "echo ran > "+shellWord(ran)+"\n")
	holdReadLock(t, h)

	out := h.run("--json", "skill", "resolve", "kit", "--editor")
	equal(t, "exit", out.exit, 7)
	equal(t, "code", h.one(out.stdout, "error")["code"], "locked")
	nothingAt(t, "what the editor marks when it runs", ran)
	equal(t, "what is unmerged", unmerged(t, h, "kit"), index)
	equal(t, "journals", journalCount(t, h), 0)
}

// TestSkillResolveInAnEditorRefusesABusyLockAfterward: the run takes the
// lock again once the editor exits, without waiting for it. Held by
// another command then, one that writes or a scan alike, the run is exit
// code 7 and stages nothing: what the editor saved stays in the checkout,
// unmerged, and the same command run again, with an editor that saves
// nothing, stages it.
func TestSkillResolveInAnEditorRefusesABusyLockAfterward(t *testing.T) {
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
			index := unmerged(t, h, "kit")
			stub, goFile := waitingEditor(t)
			h.env["EDITOR"] = stub
			p := h.start("editor open", "--json", "skill", "resolve", "kit", "--editor", "guide.md")
			p.await()
			release := c.hold(t, h)
			writeFile(t, goFile, "")
			out := p.wait()
			release()
			equal(t, "exit", out.exit, 7)
			e := h.one(out.stdout, "error")
			equal(t, "code", e["code"], "locked")
			equal(t, "message", e["message"], "another agentx command holds the lock, so what the editor saved stays in the merge, unstaged")
			equal(t, "hint", e["hint"], "run 'agentx skill resolve kit --editor guide.md' again to stage it once that command is done")
			equal(t, "what is unmerged", unmerged(t, h, "kit"), index)
			equal(t, "what the editor saved", fileBody(t, inCheckout(h, "guide.md")), "resolved in the editor\n")

			h.env["EDITOR"] = idleEditor(t)
			out = h.run("--json", "skill", "resolve", "kit", "--editor", "guide.md")
			equal(t, "exit of the next session", out.exit, 0)
			equal(t, "summary of the next session", h.one(out.stdout, "result")["summary"], "resolved guide.md in the merge of kit, 4 files left to resolve")
			equal(t, "guide.md", fileBody(t, inCheckout(h, "guide.md")), "resolved in the editor\n")
		})
	}
}

// TestSkillResolveReportsAnAbortDuringTheEditorSession: a merge given up
// while its editor was open, its checkout gone, is reported once the
// editor exits, exit code 6, with nothing written: not a checkout, not a
// file, not the library.
func TestSkillResolveReportsAnAbortDuringTheEditorSession(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	stub, goFile := waitThen(t, "exit 0\n")
	h.env["EDITOR"] = stub
	p := h.start("editor open", "--json", "skill", "resolve", "kit", "--editor")
	p.await()
	h.mustRun("skill", "resolve", "kit", "--abort")
	library := onDisk(t, h.library)
	writeFile(t, goFile, "")
	out := p.wait()
	equal(t, "exit", out.exit, 6)
	e := h.one(out.stdout, "error")
	equal(t, "message", e["message"], "kit has no merge pending: it was given up or completed while the editor was open, so nothing was staged")
	equal(t, "hint", e["hint"], "run 'agentx skill list' to see which skills have one")
	if got := h.eventsOfType(out.stdout, "conflict"); len(got) != 0 {
		t.Errorf("the run reported %v", got)
	}
	noCheckout(t, h, "kit")
	equal(t, "the library", onDisk(t, h.library), library)
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
	equal(t, "notes.md", fileBody(t, filepath.Join(h.library, "kit", "notes.md")), "resolved in the editor\n")
	equal(t, "the import branch", h.ref(lineage.ManagedRef("kit")), candidate)
	noCheckout(t, h, "kit")
}

// TestSkillResolveStoppedWhileTheEditorIsOpenKeepsTheFiles: a SIGTERM still
// stops a run whose editor is open, exit code 9, and stages nothing: what
// the editor saved before the stop stays in the checkout, unmerged.
func TestSkillResolveStoppedWhileTheEditorIsOpenKeepsTheFiles(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	index := unmerged(t, h, "kit")
	stub, _ := waitThen(t, "exit 0\n")
	ready := filepath.Join(t.TempDir(), "open")
	h.env["PATH"] = gitOnlyPath(t) // as TestSkillResolveLeavesACtrlCToTheEditor says
	h.env["GIT_EDITOR"] = editorStub(t, "reporting-editor", `for f in "$@"; do printf 'typed before the stop\n' > "$f"; done
echo open > `+shellWord(ready)+"\nexec "+shellWord(stub)+` "$@"`+"\n")

	code, stderr := signalled(t, h, stopRun{ready: ready, sigs: []syscall.Signal{syscall.SIGTERM}, group: true,
		args: []string{"skill", "resolve", "kit", "--editor", "--color", "off"}})
	equal(t, "exit", code, exitInterrupted.exit)
	contains(t, "stderr", stderr, "error: interrupted")
	equal(t, "what the editor typed", fileBody(t, inCheckout(h, "notes.md")), "typed before the stop\n")
	equal(t, "what is unmerged", unmerged(t, h, "kit"), index)
	equal(t, "journals", journalCount(t, h), 0)
}

// TestSkillResolveStoppedWhileTheEditorIsOpenStopsTheEditor: a SIGTERM
// sent to agentx alone, as a supervisor or the app sends one, reaches the
// editor too, and not only the sh that runs it: the editor stops before the
// work it would do next, rather than going on after the run is over. The
// run exits 9 and stages nothing.
func TestSkillResolveStoppedWhileTheEditorIsOpenStopsTheEditor(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	index := unmerged(t, h, "kit")
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
	equal(t, "what is unmerged", unmerged(t, h, "kit"), index)
	equal(t, "journals", journalCount(t, h), 0)
}

// TestSkillResolveInAnEditorKeepsLinesLikeMarkers: a file whose own lines
// look like conflict markers is written with markers longer than any of
// them, and only a marker of that size keeps it unresolved: a save that
// keeps the file's own lines as they are resolves it, and completes the
// merge with them byte for byte, and one that leaves the markers stages
// nothing. Chosen by --hunk, the file keeps those lines too.
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
		{
			h := pending(t)
			saved := "a\n<<<<<<< HEAD\nb\n=========\nc, resolved\n>>>>>>>> x\n"
			candidate := h.ref(lineage.CandidateRef("kit"))
			given := fileBody(t, inCheckout(h, "markers.md"))
			contains(t, "the markers the merge wrote", given, "\n"+strings.Repeat("<", 10)+" ")
			seen := filepath.Join(t.TempDir(), "seen")
			h.env["EDITOR"] = editorStub(t, "saving-editor", `while IFS= read -r line; do printf '%s\n' "$line"; done < "$1" > `+shellWord(seen)+`
printf '%s' `+shellWord(saved)+` > "$1"
`)
			out := h.run("--json", "skill", "resolve", "kit", "--editor")
			equal(t, "exit", out.exit, 0)
			equal(t, "what the editor was given", fileBody(t, seen), given)
			contains(t, "summary", h.one(out.stdout, "result")["summary"].(string), "resolved kit and updated it from ")
			equal(t, "markers.md", fileBody(t, filepath.Join(h.library, "kit", "markers.md")), saved)
			equal(t, "the import branch", h.ref(lineage.ManagedRef("kit")), candidate)
			noCheckout(t, h, "kit")
		}
	})
	t.Run("a save that leaves the markers", func(t *testing.T) {
		t.Parallel()
		h := pending(t)
		h.env["EDITOR"] = idleEditor(t)
		out := h.run("--json", "skill", "resolve", "kit", "--editor")
		equal(t, "exit", out.exit, 0)
		equal(t, "summary", h.one(out.stdout, "result")["summary"], "resolved nothing in the merge of kit: 1 file left to resolve")
		equal(t, "what is unmerged", unmerged(t, h, "kit"), "1 kit-dir/markers.md\n2 kit-dir/markers.md\n3 kit-dir/markers.md")
	})
	t.Run("the update's side chosen", func(t *testing.T) {
		t.Parallel()
		h := pending(t)
		resolveKit(t, h, 0, "markers.md:1=theirs")
		noCheckout(t, h, "kit")
		equal(t, "markers.md", fileBody(t, filepath.Join(h.library, "kit", "markers.md")), strings.Replace(markers, "c\n", "c, upstream\n", 1))
	})
}

// TestSkillResolveInAnEditorKeepsASkillsOwnSmallerMarkers: a skill whose
// own .gitattributes sets a marker size smaller than git's seven has its
// conflicts written with markers of that size, and a save that leaves them
// stages nothing and completes nothing.
func TestSkillResolveInAnEditorKeepsASkillsOwnSmallerMarkers(t *testing.T) {
	t.Parallel()
	h := clashHarness(t, map[string]string{".gitattributes": "*.md conflict-marker-size=5\n"}, func(*sourceRepo) {}, func(string) {})
	if given := fileBody(t, inCheckout(h, "notes.md")); !strings.HasPrefix(given, "<<<<< ") {
		t.Fatalf("the merge wrote no markers of five:\n%s", given)
	}
	h.env["EDITOR"] = idleEditor(t)
	out := h.run("--json", "skill", "resolve", "kit", "--editor")
	equal(t, "exit", out.exit, 0)
	equal(t, "summary", h.one(out.stdout, "result")["summary"], "resolved nothing in the merge of kit: 1 file left to resolve")
	equal(t, "what is unmerged", unmerged(t, h, "kit"), "1 kit-dir/notes.md\n2 kit-dir/notes.md\n3 kit-dir/notes.md")
	equal(t, "notes.md", fileBody(t, filepath.Join(h.library, "kit", "notes.md")), "ONE\n")
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
// live write. The next command recovers each one, and the merge is then
// complete: the import branch moved, the library directory retained and
// replaced, the unedited copy retained and refreshed, the checkout
// retained and removed, then the candidate deleted; the branch at the
// update, the library and the copy holding the merged version, the edited
// copy kept, no candidate, no checkout, no registration of one, and
// nothing staged or retained left behind. Giving the merge up, killed
// once its checkout is removed and before git's registration of it goes,
// leaves the registration to the next command, which prunes it.
func TestSkillResolveRecoversAtEveryBoundary(t *testing.T) {
	t.Parallel()
	const kinds = "ref, remove, publish, remove, publish, remove, ref"
	steps := len(strings.Split(kinds, ", "))
	for stop := 0; stop <= steps+1; stop++ {
		name := fmt.Sprintf("a completion after %d steps", stop)
		if stop > steps {
			name = "a completion killed after its last live write"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h, _, _ := resolveHarness(t)
			claude := filepath.Join(h.home, ".claude", "skills", "kit")
			cursor := filepath.Join(h.home, ".cursor", "skills", "kit")
			editCopy(t, cursor)
			edited := libraryTree(t, cursor)
			resolveKit(t, h, 0, kitSides...)
			candidate := h.ref(lineage.CandidateRef("kit"))
			if stop > steps {
				out := killedChild(t, h, "TestResolveChildProcess", resolveChildEnv, "kit|--hunk|kept.md:1=theirs", lastWriteScript)
				if h.ref(lineage.CandidateRef("kit")) != "" {
					t.Fatalf("the completion was not killed after it deleted the candidate:\n%s", out)
				}
				equal(t, "journals the killed run left", journalCount(t, h), 1)
			} else {
				killedChild(t, h, "TestResolveChildProcess", resolveChildEnv, "kit|--hunk|kept.md:1=theirs", killedUpdateScript)
				journal := readJournal(t, h)
				var got []string
				for _, s := range journal {
					got = append(got, s.Kind)
				}
				equal(t, "the journal's steps", strings.Join(got, ", "), kinds)
				applyUpdateSteps(t, h, journal, stop)
			}

			if got := h.run("config", "set", "label", "recovered"); got.exit != 0 {
				t.Fatalf("the command after the killed run: exit %d\n%s", got.exit, got.stderr)
			}
			want := kitTree(t, h, kitResolved)
			completedKit(t, h, candidate, want)
			sameTree(t, "claude's copy", libraryTree(t, claude), want)
			sameTree(t, "cursor's copy", libraryTree(t, cursor), edited)
			for _, dir := range []string{h.library, filepath.Dir(claude), filepath.Dir(cursor), filepath.Join(h.agentx, "merges")} {
				equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
			}
			equal(t, "state", h.listed("kit")["state"], stateModified)
		})
	}
	t.Run("giving the merge up", func(t *testing.T) {
		t.Parallel()
		h, _, _ := resolveHarness(t)
		library := onDisk(t, h.library)
		killedChild(t, h, "TestResolveChildProcess", resolveChildEnv, "kit|--abort", `
case " $* " in
*" worktree remove "*)
	kill -9 $PPID
	exit 1
	;;
esac
exec %GIT% "$@"
`)
		nothingAt(t, "the checkout once killed", pendingCheckout(h, "kit"))
		contains(t, "git's registration once killed", h.accountGit("worktree", "list", "--porcelain"), pendingReason)
		h.mustRun("config", "set", "label", "recovered")
		noCheckout(t, h, "kit")
		equal(t, "the library", onDisk(t, h.library), library)
	})
}

// TestSkillResolveRecoveryKeepsTheMergeWhenTheLibraryChanged kills a
// completion once its journal is on disk, and the library directory is
// edited before the next command. That command's recovery moves the import
// branch, finds the directory holding something the completion did not
// capture, and refuses: the directory keeps the edit, and the candidate
// and the checkout are still there, since recovery removes them after the
// library directory as the completion does, so nothing of the merge is
// lost. Restoring the directory lets the next command finish it.
func TestSkillResolveRecoveryKeepsTheMergeWhenTheLibraryChanged(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	resolveKit(t, h, 0, kitSides...)
	candidate := h.ref(lineage.CandidateRef("kit"))
	killedChild(t, h, "TestResolveChildProcess", resolveChildEnv, "kit|--hunk|kept.md:1=theirs", killedUpdateScript)
	equal(t, "journals the killed completion left", journalCount(t, h), 1)
	usage := filepath.Join(h.library, "kit", "usage.md")
	writeFile(t, usage, "an edit made after the completion stopped\n")

	out := h.run("--json", "config", "set", "label", "recovered")
	equal(t, "exit", out.exit, 6)
	contains(t, "message", h.one(out.stdout, "error")["message"].(string), "recovery required")
	equal(t, "the import branch, moved before the paths", h.ref(lineage.ManagedRef("kit")), candidate)
	equal(t, "the candidate ref", h.ref(lineage.CandidateRef("kit")), candidate)
	equal(t, "the edit", fileBody(t, usage), "an edit made after the completion stopped\n")
	if _, err := os.Stat(filepath.Join(pendingCheckout(h, "kit"), ".git")); err != nil {
		t.Errorf("the checkout went: %v", err)
	}

	writeFile(t, usage, "usage\n")
	h.mustRun("config", "set", "label", "recovered")
	completedKit(t, h, candidate, kitTree(t, h, kitResolved))
}

// TestSkillResolveKeepsTheCheckoutWhenTheRemoveRefuses: a completion whose
// retaining remove of the library directory refuses, the directory edited
// once the journal was on disk, stops there with the journal left for
// recovery: the checkout and the candidate are still there, and the edit
// is kept. Once the directory is as the completion captured it, the next
// command finishes the journal and the merge is complete.
func TestSkillResolveKeepsTheCheckoutWhenTheRemoveRefuses(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	resolveKit(t, h, 0, kitSides...)
	candidate := h.ref(lineage.CandidateRef("kit"))
	usage := filepath.Join(h.library, "kit", "usage.md")
	stubGit(t, h, `#!/bin/sh
case " $* " in
*" update-ref "*) printf 'an edit made meanwhile\n' > `+shellWord(usage)+` ;;
esac
exec `+realGit(t)+` "$@"
`)
	out := resolveKit(t, h, 6, "kept.md:1=theirs")
	h.env["PATH"] = gitOnlyPath(t) // the git that edits the directory is gone
	contains(t, "message", h.one(out.stdout, "error")["message"].(string), "recovery required")
	equal(t, "journals", journalCount(t, h), 1)
	equal(t, "the edit", fileBody(t, usage), "an edit made meanwhile\n")
	equal(t, "the candidate ref", h.ref(lineage.CandidateRef("kit")), candidate)
	if _, err := os.Stat(filepath.Join(pendingCheckout(h, "kit"), ".git")); err != nil {
		t.Errorf("the checkout went: %v", err)
	}

	writeFile(t, usage, "usage\n")
	h.mustRun("config", "set", "label", "recovered")
	completedKit(t, h, candidate, kitTree(t, h, kitResolved))
}

// TestSkillResolveRefusesAnImportBranchMovedUnderTheLock: the import
// branch is read again under the lock, and one something moved after the
// run read it refuses the completion with exit code 6 and nothing
// written: the checkout keeps every resolution, and the library, every
// copy and the candidate are left as they were.
func TestSkillResolveRefusesAnImportBranchMovedUnderTheLock(t *testing.T) {
	t.Parallel()
	h, _, _ := resolveHarness(t)
	resolveKit(t, h, 0, kitSides...)
	tip, candidate := h.ref(lineage.ManagedRef("kit")), h.ref(lineage.CandidateRef("kit"))
	moved := h.accountGitInput("", "commit-tree", tip+"^{tree}", "-m", "written again", "-m", h.accountGit("log", "-1", "--format=%B", tip))
	library, placed := onDisk(t, h.library), onDisk(t, h.home)
	stubGit(t, h, `#!/bin/sh
case " $* " in
*" ls-files "*) `+realGit(t)+" --git-dir="+shellWord(gitx.AccountRepoPath(h.agentx))+" update-ref "+lineage.ManagedRef("kit")+" "+moved+` ;;
esac
exec `+realGit(t)+` "$@"
`)
	out := resolveKit(t, h, 6, "kept.md:1=theirs")
	h.env["PATH"] = gitOnlyPath(t) // the git that moves the branch is gone
	equal(t, "message", h.one(out.stdout, "error")["message"], "the import branch refs/heads/managed/kit moved outside agentx while the merge was pending, so it cannot be completed")
	equal(t, "the import branch", h.ref(lineage.ManagedRef("kit")), moved)
	equal(t, "the candidate", h.ref(lineage.CandidateRef("kit")), candidate)
	equal(t, "the library", onDisk(t, h.library), library)
	equal(t, "the placements", onDisk(t, h.home), placed)
	equal(t, "what is unmerged", unmerged(t, h, "kit"), "")
	equal(t, "journals", journalCount(t, h), 0)
	equal(t, "what is left beside the library", strings.Join(hiddenEntries(t, h.library), " "), "")
}
