package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// TestClassifyFork is the rule reconciliation, the listing's warnings and
// skill place share, one row per combination of what a fork's library
// entry and worktree can be that the rule tells apart.
func TestClassifyFork(t *testing.T) {
	t.Parallel()
	repair := func(worktree, content, link bool) forkVerdict {
		return forkVerdict{outcome: outcomeWorktreeMissing, worktree: worktree, content: content, link: link}
	}
	for _, tc := range []struct {
		name  string
		facts forkFacts
		want  forkVerdict
	}{
		{"placed and linked", forkFacts{lib: libOwn, root: rootWorktree, registered: true, onBranch: true, skillDir: true},
			forkVerdict{outcome: outcomeRestored}},
		{"placed and linked, on another branch", forkFacts{lib: libOwn, root: rootWorktree, registered: true, skillDir: true},
			forkVerdict{outcome: outcomeRestored}},
		{"library symlink deleted", forkFacts{lib: libAbsent, root: rootWorktree, registered: true, onBranch: true, skillDir: true},
			repair(false, false, true)},
		{"library symlink into the worktrees directory, leading nowhere", forkFacts{lib: libDangling, root: rootWorktree, registered: true, onBranch: true, skillDir: true},
			repair(false, false, true)},
		{"skill directory deleted from the worktree", forkFacts{lib: libDangling, root: rootWorktree, registered: true, onBranch: true},
			repair(true, true, true)},
		{"skill directory deleted, worktree on another branch", forkFacts{lib: libDangling, root: rootWorktree, registered: true},
			forkVerdict{outcome: outcomeWorktreeMissing}},
		{"skill directory deleted, a git merge stopped in the worktree", forkFacts{lib: libDangling, root: rootWorktree, registered: true, onBranch: true, busy: true},
			forkVerdict{outcome: outcomeWorktreeMissing}},
		{"library symlink deleted, a git merge stopped in the worktree", forkFacts{lib: libAbsent, root: rootWorktree, registered: true, onBranch: true, skillDir: true, busy: true},
			repair(false, false, true)},
		{"worktree deleted, its registration kept", forkFacts{lib: libDangling, root: rootAbsent, registered: true},
			repair(true, true, true)},
		{"worktree and registration gone, symlink left", forkFacts{lib: libDangling, root: rootAbsent},
			repair(true, true, true)},
		{"worktree deleted, library symlink too", forkFacts{lib: libAbsent, root: rootAbsent, registered: true},
			repair(true, true, true)},
		{"worktree emptied, nothing registered", forkFacts{lib: libDangling, root: rootEmpty},
			repair(true, true, true)},
		{"never placed on this machine", forkFacts{lib: libAbsent, root: rootAbsent},
			forkVerdict{outcome: outcomeInstallable}},
		{"pointers moved with agentx home", forkFacts{lib: libOwn, root: rootMoved},
			forkVerdict{outcome: outcomeWorktreeMissing, pointers: true}},
		{"worktree not registered", forkFacts{lib: libOwn, root: rootOrphan},
			forkVerdict{outcome: outcomeAdoptCandidate, inWay: "worktree"}},
		{"a directory at the library entry", forkFacts{lib: libDir, root: rootWorktree, registered: true, onBranch: true, skillDir: true},
			forkVerdict{outcome: outcomeAdoptCandidate, inWay: "library symlink"}},
		{"a directory at the library entry, never placed", forkFacts{lib: libDir, root: rootAbsent},
			forkVerdict{outcome: outcomeAdoptCandidate, inWay: "library symlink"}},
		{"a foreign symlink at the library entry, worktree not registered", forkFacts{lib: libForeign, root: rootOrphan},
			forkVerdict{outcome: outcomeAdoptCandidate, inWay: "library symlink"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			equal(t, "verdict", classifyFork(tc.facts), tc.want)
		})
	}
}

// TestForkWarningsSayWhatIsMissing is what a listing warns of a fork whose
// worktree is missing, one row per case worktreeMissingWarning tells apart,
// and what skill place refuses of an adopt candidate without --force, one
// row per case of adoptRefusal. The worktree that git is running in holds
// a .git file naming an admin directory that holds an index lock.
func TestForkWarningsSayWhatIsMissing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := filepath.Join(dir, "worktrees", "notes")
	admin := filepath.Join(dir, "store.git", "worktrees", "notes")
	writeFile(t, mkdirs(t, root, ".git"), "gitdir: "+admin+"\n")
	writeFile(t, mkdirs(t, admin, "index.lock"), "")
	f := forkSite{name: "notes", branch: "skills/notes", root: root, skillDir: filepath.Join(root, "notes"), libPath: filepath.Join(dir, "skills", "notes")}
	place := "run 'agentx skill place notes'"
	present := forkFacts{root: rootWorktree, registered: true, onBranch: true, skillDir: true}
	with := func(change func(*forkFacts)) forkFacts {
		facts := present
		change(&facts)
		return facts
	}
	for _, tc := range []struct {
		what  string
		facts forkFacts
		want  string
	}{
		{"pointers moved", with(func(x *forkFacts) { x.root = rootMoved }),
			"notes's worktree " + root + " needs repair; " + place + " to repair it"},
		{"worktree gone", with(func(x *forkFacts) { x.root = rootAbsent }),
			"notes's worktree " + root + " is missing; " + place + " to check it out again from its branch"},
		{"git running", with(func(x *forkFacts) { x.busy, x.skillDir = true, false }),
			"notes's skill directory " + f.skillDir + " is missing from its worktree, where git is running; " + place + " once it finishes; if no git is running, remove " + filepath.Join(admin, "index.lock")},
		{"off its branch", with(func(x *forkFacts) { x.onBranch, x.skillDir = false, false }),
			"notes's worktree " + root + " holds no skill directory and is not on its branch skills/notes; run 'git -C " + root + " switch skills/notes', then " + place},
		{"skill directory gone", with(func(x *forkFacts) { x.skillDir = false }),
			"notes's skill directory " + f.skillDir + " is missing from its worktree; " + place + " to lay it out again from its branch"},
		{"library symlink gone", with(func(x *forkFacts) { x.lib = libAbsent }),
			"notes's library symlink " + f.libPath + " is missing or leads nowhere; " + place + " to put it back"},
	} {
		equal(t, tc.what, worktreeMissingWarning(f, tc.facts), tc.want)
	}

	adopt := "run 'agentx skill place notes --force' to adopt it: its content becomes the skill's unpublished edits"
	for _, tc := range []struct {
		what, inWay      string
		lib              libKind
		wantWhat, wayOut string
	}{
		{"a directory at the worktree", "worktree", libOwn, root + " is in the way of notes's worktree", adopt},
		{"a directory at the library entry", "library symlink", libDir, f.libPath + " is in the way of notes's library symlink", adopt},
		{"a symlink of the user's", "library symlink", libForeign, f.libPath + " is in the way of notes's library symlink",
			"run 'agentx skill place notes --force' to replace it with notes's library symlink"},
	} {
		what, wayOut := adoptRefusal(f, forkVerdict{outcome: outcomeAdoptCandidate, inWay: tc.inWay}, forkFacts{lib: tc.lib})
		equal(t, tc.what, what, tc.wantWhat)
		equal(t, tc.what+", the way out", wayOut, tc.wayOut)
	}
}

// TestServeReconcilesAtStart starts serve over a home built by earlier
// commands, as agentx installed again over an existing agentx home finds
// it, and reads its first inventory, which no other event comes before: a
// fork placed as it was stays as it is, and stays its worktree's owner
// beside a copy of the worktree; one whose worktree was deleted by hand is
// checked out again and linked; one whose skill directory was deleted
// while a git merge waits in its worktree is left as it is, the merge
// kept; and one whose registration was removed by hand, its worktree then
// a directory git does not know, is left as it is. The inventory warns of
// the last two and of nothing it put back. Before that, git's registration
// of a worktree whose directory is gone is pruned, while a pending merge's
// locked checkout stays; the repair rewrites the version file.
func TestServeReconcilesAtStart(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--name", "alpha", "--fetch")
	for _, name := range []string{"kept", "gone", "orphan", "busy"} {
		h.mustRun("skill", "new", name)
	}
	worktrees := filepath.Join(h.agentx, "worktrees")
	gone, orphan, kept, busy := filepath.Join(worktrees, "gone"), filepath.Join(worktrees, "orphan"), filepath.Join(worktrees, "kept"), filepath.Join(worktrees, "busy")
	remove(t, gone)
	copyTree(t, kept, filepath.Join(worktrees, "kept-copy"))
	busyAdmin, _ := home.AdminDirOf(busy)
	mergeHead := filepath.Join(busyAdmin, "MERGE_HEAD")
	writeFile(t, mergeHead, h.ref(lineage.ForkRef("busy"))+"\n")
	remove(t, filepath.Join(busy, "busy"))
	admin, _ := home.AdminDirOf(orphan)
	remove(t, admin)
	orphanTree := libraryTree(t, orphan)
	keptTree := libraryTree(t, kept)
	stray := filepath.Join(t.TempDir(), "stray")
	h.storeGit("worktree", "add", "--quiet", "--detach", stray, h.ref(lineage.ForkRef("kept")))
	remove(t, stray)
	pending := pendingCheckout(h, "alpha")
	h.storeGit("worktree", "add", "--quiet", "--detach", "--lock", "--reason", pendingReason, pending, h.ref(lineage.ManagedRef("alpha")))
	version := mutationVersion(t, h)

	p := h.serve(t, "--json")
	inventory := p.next("inventory")
	equal(t, "exit", p.close(), 0)

	if !home.WorktreeAt(gone, "skills/gone") {
		t.Fatal("the deleted worktree was not checked out again")
	}
	equal(t, "git status in the repaired worktree", gitIn(t, h, gone, "status", "--porcelain"), "")
	forkLinked(t, filepath.Join(h.library, "gone"), filepath.Join(gone, "gone"))
	sameTree(t, "the fork placed as it was", libraryTree(t, kept), keptTree)
	forkLinked(t, filepath.Join(h.library, "kept"), filepath.Join(kept, "kept"))
	if !home.RegisteredIn(gitx.StorePath(h.agentx), kept) {
		t.Error("the copy of a worktree took its registration")
	}
	if !lexists(mergeHead) {
		t.Error("the merge waiting in a worktree was lost")
	}
	if lexists(filepath.Join(busy, "busy")) {
		t.Error("the skill directory was laid out where a git merge waits")
	}
	sameTree(t, "the adopt candidate", libraryTree(t, orphan), orphanTree)
	if home.RegisteredIn(gitx.StorePath(h.agentx), orphan) {
		t.Error("the adopt candidate was registered")
	}
	warned := strings.Join(stringsOf(inventory["warnings"]), "\n")
	contains(t, "the inventory's warnings", warned,
		orphan+" is in the way of orphan's worktree; run 'agentx skill place orphan --force' to adopt it")
	contains(t, "the inventory's warnings", warned,
		"busy's skill directory "+filepath.Join(busy, "busy")+" is missing from its worktree, where a git merge stopped part way")
	for _, name := range []string{"gone", "kept"} {
		if strings.Contains(warned, name+"'s ") {
			t.Errorf("the inventory warns of %s:\n%s", name, warned)
		}
	}
	list := h.storeGit("worktree", "list", "--porcelain")
	if strings.Contains(list, stray) {
		t.Errorf("the registration of a worktree whose directory is gone survived:\n%s", list)
	}
	contains(t, "the worktrees", list, pending)
	if !slices.Contains(entriesOf(t, pending), ".git") {
		t.Error("the pending merge's checkout is no checkout any more")
	}
	if mutationVersion(t, h) <= version {
		t.Error("the repair did not rewrite the version file")
	}
}

// stringsOf is a JSON array of strings as Go strings.
func stringsOf(v any) []string {
	var out []string
	list, _ := v.([]any)
	for _, s := range list {
		if str, ok := s.(string); ok {
			out = append(out, str)
		}
	}
	return out
}

// moveTo moves the harness's whole root, agentx home, the library and the
// user's home with it, to root, as a user moves a home directory to
// another disk, and points the harness there. Nothing in it is rewritten.
// The new root is taken on its real path, as newHarness takes the first.
func (h *harness) moveTo(t *testing.T, root string) {
	t.Helper()
	parent, err := filepath.EvalSymlinks(filepath.Dir(root))
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Join(parent, filepath.Base(root))
	from := filepath.Dir(h.home)
	if err := os.Rename(from, root); err != nil {
		t.Fatal(err)
	}
	moved := func(p string) string { return filepath.Join(root, strings.TrimPrefix(p, from+"/")) }
	h.home, h.agentx, h.library, h.config = moved(h.home), moved(h.agentx), moved(h.library), moved(h.config)
	h.env["HOME"], h.env["AGENTX_HOME"], h.env["AGENTX_LIBRARY"], h.env["XDG_CONFIG_HOME"] = h.home, h.agentx, h.library, h.config
}

// TestServeRepairsWorktreesOfAMovedHome moves a home with a fork as a
// whole, which leaves the worktree's pointers naming where it was, so that
// no git runs in it, and starts serve there: it repairs them with git,
// naming the worktree's path, so that the first inventory does not warn
// of the fork, and rewrites the version file. The home
// repo is one a git older than 2.48 made, which writes absolute pointers:
// a newer git makes the repo write them relative, and a move breaks none.
func TestServeRepairsWorktreesOfAMovedHome(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setHub(newHubRemote(t, h))
	_, _ = h.storeGitErr("config", "--unset", "worktree.useRelativePaths") // unset already on an older git
	h.mustRun("skill", "new", "notes")
	h.moveTo(t, filepath.Join(t.TempDir(), "moved"))
	root := filepath.Join(h.agentx, "worktrees", "notes")
	if !home.PointersMoved(root) {
		t.Fatal("the move left the worktree's pointers meeting; nothing to repair")
	}
	version := mutationVersion(t, h)
	p := h.serve(t, "--json")
	inventory := p.next("inventory")
	equal(t, "exit", p.close(), 0)
	if warned := strings.Join(stringsOf(inventory["warnings"]), "\n"); strings.Contains(warned, "notes's ") {
		t.Errorf("the inventory warns of the repaired fork:\n%s", warned)
	}
	if mutationVersion(t, h) <= version {
		t.Error("the repair did not rewrite the version file")
	}
	contains(t, "the worktrees", h.storeGit("worktree", "list", "--porcelain"), "worktree "+root+"\n")
	equal(t, "git status in the worktree", gitIn(t, h, root, "status", "--porcelain"), "")
}
