package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/scan"
)

// libraryDecoys fills the library with n skills the run under test does not
// install: they are there because a read of the library is a read of every
// directory it holds, so the cost a run puts on the library is a cost it
// puts on skills it has nothing to do with.
func libraryDecoys(t *testing.T, h *harness, n int) {
	t.Helper()
	for i := range n {
		dir := filepath.Join(h.library, fmt.Sprintf("decoy%02d", i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, "SKILL.md"), fmt.Sprintf("---\nname: decoy%02d\ndescription: a decoy\n---\n\ndecoy\n", i))
	}
}

// libraryReads runs fn with every read of the library counted and answers
// how many it made. readLibrary is the one way this package reads the
// library, so this counts the reads of a run whatever asked for them,
// rather than inferring them from how long the run took: a count is the
// same on a fast machine and a loaded one. Swapping it is safe because a
// test that counts what a run costs does not run beside another; see
// suiteParallel in harness_test.go.
func libraryReads(t *testing.T, fn func()) int64 {
	t.Helper()
	real := readLibrary
	defer func() { readLibrary = real }()
	var n atomic.Int64
	readLibrary = func(dir string) ([]scan.LibrarySkill, []string) {
		n.Add(1)
		return real(dir)
	}
	fn()
	return n.Load()
}

// installAll installs every skill of a source of its own into a library
// that already holds a few skills the run does not touch, and answers how
// many times the run read the library.
func installAll(t *testing.T, name string, skills int) int64 {
	t.Helper()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo(name, true)
	for i := range skills {
		s.skill(fmt.Sprintf("skills/s%02d", i), fmt.Sprintf("s%02d", i), "One of a batch", nil)
	}
	s.commit(fmt.Sprintf("%d skills", skills))
	if out := h.run("source", "add", s.url); out.exit != 0 {
		t.Fatalf("source add: exit %d\n%s", out.exit, out.stderr)
	}
	libraryDecoys(t, h, 4)
	return libraryReads(t, func() {
		if out := h.run("skill", "add", s.url, "--all"); out.exit != 0 {
			t.Fatalf("skill add --all: exit %d\n%s\n%s", out.exit, out.stdout, out.stderr)
		}
	})
}

// TestSkillAddReadsTheLibraryOnceForTheRun holds a batch's cost to the
// library it installs into. Reporting an install reads the library, and
// reading the library hashes every directory it holds, so reading it once
// per installed skill costs a run of twelve twelve passes over a library
// that has nothing to do with them: super-linear in the count, and paid
// again by every later install as the library grows.
//
// The two runs install from sources that differ only in how many skills
// they hold. What each run reads of the library must not depend on that:
// one read is one read whether a run installs one skill or twelve. The
// assertion is on the count rather than on how long the runs took, because
// a ratio of two durations answers for the machine as much as for the code
// and a run whose fixed work is slow reads as a run that reads too often.
func TestSkillAddReadsTheLibraryOnceForTheRun(t *testing.T) {
	// Not parallel, for the reason harness_test.go gives above
	// suiteParallel: it swaps a package variable for the length of a run.
	one := installAll(t, "costone", 1)
	twelve := installAll(t, "costtwelve", 12)
	if one == 0 {
		t.Fatal("installing one skill read the library no times at all: the count is not seeing the read it is there to count")
	}
	if twelve != one {
		t.Errorf("installing one skill read the library %d time(s) and installing twelve read it %d:"+
			" the library is being read once per installed skill, not once for the run", one, twelve)
	}
	t.Logf("library reads: %d installing one skill, %d installing twelve", one, twelve)
}
