package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
)

// Most tests start from a source built the same way: the same files, the
// same commits and, since every fixture commit is made in the isolated
// environment with its fixed identity and date, the same commit ids. Built
// with git, that is a dozen processes a test before its first assertion, so
// each such source is built once per test binary and every later test gets
// a copy of it, written from memory without starting a process.
//
// A copy is what the test would have built: the git directory and the work
// tree byte for byte, modes and symlinks included, at the test's own path,
// so its file:// URL is the test's own as before. Nothing in a source's git
// directory names the path it lives at – the work tree is passed on every
// call and never recorded – so a copy needs no rewriting. Only the index's
// record of when and where each file was written differs, and git reads a
// difference there as a reason to hash the file again, never as a change.

// fixtureGitConfig is what every git call of a fixture helper sets on top of
// the isolated environment: no fsync, which a fixture that is thrown away
// with its test has no use for, and no automatic gc or maintenance, whose
// check after every commit is a process of its own.
func fixtureGitConfig(args ...string) []string {
	return append([]string{"-c", "core.fsync=none", "-c", "gc.auto=0", "-c", "maintenance.auto=false"}, args...)
}

// fixtureRunner is the runner a source image is built with: the test
// process's PATH and nothing else of any test's environment, so that the
// image does not depend on which test happened to build it, whatever git
// wrapper or configuration that test has set up.
func fixtureRunner() *gitx.Runner {
	return gitx.New(map[string]string{"PATH": os.Getenv("PATH")}, false, func(string, ...any) {})
}

// sourceImage is one state of a source: its git directory and work tree
// and the ids the build that made it returned. The first test to need it
// builds it, and every test after that copies it.
type sourceImage struct {
	once   sync.Once
	built  bool // the build finished; a build that failed leaves it false
	gitDir dirImage
	work   dirImage
	ids    []string
}

var sourceImages = struct {
	sync.Mutex
	byState map[string]*sourceImage
}{byState: map[string]*sourceImage{}}

func sourceImageOf(state string) *sourceImage {
	sourceImages.Lock()
	defer sourceImages.Unlock()
	img := sourceImages.byState[state]
	if img == nil {
		img = &sourceImage{}
		sourceImages.byState[state] = img
	}
	return img
}

// advance brings s from the state it is in to the one build makes of it,
// and returns what build returned. The first test to get there builds it
// with git in its own source; every later test gets a copy of that result.
//
// A state is named by the steps that led to it, so step must name build
// alone, and build must depend on nothing but the source it is handed: a
// test asking for the same step from the same state gets what the first
// build made. A source whose state is unknown, because something wrote to
// it since it was created or last advanced, is built here, with git.
func (s *sourceRepo) advance(step string, build func(s *sourceRepo) []string) []string {
	s.t.Helper()
	if s.state == "" {
		return build(s)
	}
	state := s.state + "/" + step
	img := sourceImageOf(state)
	here := false
	img.once.Do(func() {
		b := *s
		b.git = fixtureRunner()
		ids := build(&b)
		gitDir, err := imageOf(s.gitDir)
		if err != nil {
			s.t.Fatal(err)
		}
		work, err := imageOf(s.work)
		if err != nil {
			s.t.Fatal(err)
		}
		img.gitDir, img.work, img.ids, img.built, here = gitDir, work, ids, true, true
	})
	if !img.built {
		s.t.Fatalf("the fixture source %s failed to build in another test", state)
	}
	if !here {
		for _, c := range []struct {
			img dirImage
			dir string
		}{{img.gitDir, s.gitDir}, {img.work, s.work}} {
			if err := c.img.replace(c.dir); err != nil {
				s.t.Fatal(err)
			}
		}
	}
	s.state = state
	return slices.Clone(img.ids)
}

// dirImage is a directory tree held in memory, entry by entry in the order
// a walk visits them, every directory before what it holds.
type dirImage []imageEntry

type imageEntry struct {
	path string      // relative to the root of the tree
	mode fs.FileMode // type and permission bits
	data []byte      // the content of a file, the target of a symlink
}

// imageOf reads the tree under root into memory.
func imageOf(root string) (dirImage, error) {
	var img dirImage
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == root {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		e := imageEntry{path: rel, mode: info.Mode()}
		switch {
		case info.IsDir():
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			e.data = []byte(target)
		case info.Mode().IsRegular():
			if e.data, err = os.ReadFile(path); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%s: a fixture image holds no %s", path, info.Mode().Type())
		}
		img = append(img, e)
		return nil
	})
	return img, err
}

// replace makes the tree under root the image, whatever root held before.
func (img dirImage) replace(root string) error {
	if err := os.RemoveAll(root); err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	return img.writeTo(root)
}

// writeTo writes the image under root, which must hold none of it yet.
// Every mode is set as it was read, whatever the umask: git records the
// executable bit of a work tree file, and it keeps its objects read-only.
func (img dirImage) writeTo(root string) error {
	for _, e := range img {
		path := filepath.Join(root, e.path)
		var err error
		switch {
		case e.mode.IsDir():
			err = os.Mkdir(path, 0o700)
		case e.mode&fs.ModeSymlink != 0:
			err = os.Symlink(string(e.data), path)
		default:
			err = writeExactly(path, e.data, e.mode.Perm())
		}
		if err != nil {
			return err
		}
	}
	// Directories last, deepest first, so that one that is not writable is
	// filled before it gets its mode.
	for i := len(img) - 1; i >= 0; i-- {
		if e := img[i]; e.mode.IsDir() {
			if err := os.Chmod(filepath.Join(root, e.path), e.mode.Perm()); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeExactly(path string, data []byte, perm fs.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	return errors.Join(err, f.Chmod(perm), f.Close())
}

// executableSource is standardSource with a third commit that makes
// skills/alpha/scripts/run.sh executable, since a source holds modes too:
// the source installHarness and placementHarness add.
func (h *harness) executableSource() *sourceRepo {
	h.t.Helper()
	s := h.newSourceRepo("skills", true)
	s.advance("executableSource", func(s *sourceRepo) []string {
		buildStandard(s)
		s.executable("skills/alpha/scripts/run.sh")
		s.commit("an executable script")
		return nil
	})
	return s
}
