package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestACopyIsNotPublishedUnlessItHashesToTheVersion is the guard between a
// directory laid out on disk and one renamed into a client's skills
// directory. The content hash is computed in process from what was read
// back, never from what was written, so it is the last thing that can tell
// this version from something else.
//
// A library skill of the user's own can hold a symlink, and a symlink is
// read for what it resolves to in the place it sits: one naming a file
// inside the library entry contributes that file's content there and
// resolves outside the copy once it is copied somewhere else. The copy
// would then be a different skill under the same name, which is exactly
// what the check exists to stop.
func TestACopyIsNotPublishedUnlessItHashesToTheVersion(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude", ".cursor"}})
	lib := filepath.Join(h.library, "mine")
	if err := os.MkdirAll(lib, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(lib, "SKILL.md"), skill("mine", "A skill of my own"))
	writeFile(t, filepath.Join(lib, "real.md"), "the real notes\n")
	if err := os.Symlink(filepath.Join(lib, "real.md"), filepath.Join(lib, "notes.md")); err != nil {
		t.Fatal(err)
	}

	place := filepath.Join(h.home, ".cursor", "skills", "mine")
	out := h.run("skill", "place", "mine", "--to", "cursor", "--copy")
	equal(t, "exit", out.exit, 0)
	contains(t, "the warning", out.stderr, "the staged copy of mine hashes to")
	contains(t, "the warning", out.stderr, "no placement was made for cursor")
	contains(t, "the result", out.stdout, "placed mine in 0 configurations, 1 placement skipped")
	nothingAt(t, "the copy", place)
	// Nothing of the refused copy is left beside the live path either.
	equal(t, "staged directories", len(stagingIn(t, filepath.Join(h.home, ".cursor", "skills"))), 0)
	equal(t, "copy_mode", copyModeOf(t, h, "mine"), "")
}

// TestRemovalCoversADisabledConfiguration: a removal covers every detected
// configuration, whatever its enabled state. Enabling decides where later
// installs go; it says nothing about what is already on disk, and a
// configuration disabled after a skill was installed still holds that
// skill's placement. Covering only the enabled ones would leave it behind
// with nothing to say it was left.
func TestRemovalCoversADisabledConfiguration(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha").exit, 0)
	place := filepath.Join(h.home, ".cursor", "skills", "alpha")
	if _, ok := isSymlink(t, place); !ok {
		t.Fatalf("the install made no placement at %s", place)
	}
	equal(t, "disable", h.run("config", "disable", "cursor").exit, 0)

	out := h.run("skill", "remove", "alpha")
	equal(t, "exit", out.exit, 0)
	nothingAt(t, "the placement of a disabled configuration", place)
	contains(t, "the output", out.stdout, "cursor")
}

// TestRemovalFailsWhenTheLibraryStillHoldsTheName is the check that the
// report is read from the machine and not from the plan. A removal without
// --from answers for the skill being gone, and the rescan it ends with is
// what it answers from: a library that still holds the name means the run
// did not do what it is about to say it did, and that is exit code 10
// rather than a confirmation.
//
// The lock keeps other agentx commands out, not everything else, so what
// puts the directory back here is another process writing it while the
// removal finishes its last step.
func TestRemovalFailsWhenTheLibraryStillHoldsTheName(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha").exit, 0)
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	// The ref deletions are the last step of a removal, so a wrapper that
	// writes the library directory again as update-ref returns puts it back
	// after the removal took it and before the rescan reads it.
	lib := filepath.Join(h.library, "alpha")
	stubGit(t, h, "#!/bin/sh\nPATH="+os.Getenv("PATH")+`
for a in "$@"; do
	case "$a" in
	update-ref)
		`+real+` "$@"; status=$?
		mkdir -p `+lib+`
		printf '%b' '---\nname: alpha\ndescription: written again by somebody else\n---\n' > `+lib+`/SKILL.md
		exit $status
		;;
	esac
done
exec `+real+` "$@"
`)

	out := h.run("skill", "remove", "alpha")
	equal(t, "exit", out.exit, 10)
	contains(t, "the error", out.stderr, "the library still holds alpha at "+lib+" after removing it")
}
