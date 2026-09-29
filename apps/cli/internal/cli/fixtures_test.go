package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

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
	state  string // the steps that led to it, which name it
	once   sync.Once
	built  bool // the build finished; a build that failed leaves it false
	gitDir dirImage
	work   dirImage
	ids    []string
}

// newSource is the state of a source newSourceRepo has made the empty
// directories of and nothing else yet.
var newSource = &sourceImage{state: "new", built: true}

var sourceImages = struct {
	sync.Mutex
	byState map[string]*sourceImage
}{byState: map[string]*sourceImage{}}

func sourceImageOf(state string) *sourceImage {
	sourceImages.Lock()
	defer sourceImages.Unlock()
	img := sourceImages.byState[state]
	if img == nil {
		img = &sourceImage{state: state}
		sourceImages.byState[state] = img
	}
	return img
}

// advance brings s from the state it is in to the one build makes of it,
// and returns what build returned. The first test to get there builds it
// with git in its own source; every later test gets a copy of that result,
// written over what s holds as the difference between the two states.
//
// A state is named by the steps that led to it, so step must name build
// alone, and build must depend on nothing but the source it is handed: a
// test asking for the same step from the same state gets what the first
// build made. A source whose state is unknown, because something wrote to
// it since it was created or last advanced, is built here, with git.
func (s *sourceRepo) advance(step string, build func(s *sourceRepo) []string) []string {
	s.t.Helper()
	from := s.at
	if from == nil {
		return build(s)
	}
	img := sourceImageOf(from.state + "/" + step)
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
		s.t.Fatalf("the fixture source %s failed to build in another test", img.state)
	}
	if !here {
		if err := img.gitDir.writeOver(from.gitDir, s.gitDir); err != nil {
			s.t.Fatal(err)
		}
		if err := img.work.writeOver(from.work, s.work); err != nil {
			s.t.Fatal(err)
		}
	}
	s.at = img
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

// writeOver makes the tree under root, which holds prev, hold the image
// instead: what the image does not hold goes, what it holds otherwise is
// written, and the rest is left as it is. Every mode is set as it was read,
// whatever the umask: git records the executable bit of a work tree file,
// and it keeps its objects read-only.
func (img dirImage) writeOver(prev dirImage, root string) error {
	next := make(map[string]imageEntry, len(img))
	for _, e := range img {
		next[e.path] = e
	}
	kept := make(map[string]imageEntry, len(prev))
	for i := len(prev) - 1; i >= 0; i-- { // what a directory holds before the directory
		e := prev[i]
		if n, ok := next[e.path]; ok && n.mode.Type() == e.mode.Type() && (e.mode.IsDir() || bytes.Equal(n.data, e.data)) {
			kept[e.path] = e
			continue
		}
		if err := os.RemoveAll(filepath.Join(root, e.path)); err != nil {
			return err
		}
	}
	for _, e := range img {
		path := filepath.Join(root, e.path)
		k, ok := kept[e.path]
		var err error
		switch {
		case ok && e.mode.IsRegular() && k.mode != e.mode:
			err = os.Chmod(path, e.mode.Perm())
		case ok:
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
		e := img[i]
		if k, ok := kept[e.path]; !e.mode.IsDir() || ok && k.mode == e.mode {
			continue
		}
		if err := os.Chmod(filepath.Join(root, e.path), e.mode.Perm()); err != nil {
			return err
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

// buildExecutable is buildStandard with a third commit that makes
// skills/alpha/scripts/run.sh executable, since a source holds modes too:
// the source installHarness and placementHarness add.
func buildExecutable(s *sourceRepo) []string {
	s.t.Helper()
	buildStandard(s)
	s.executable("skills/alpha/scripts/run.sh")
	s.commit("an executable script")
	return nil
}

// A builder such as installHarness runs the same commands over the same
// source for every test that calls it: a source add, an install, a
// placement, each a dozen git processes and more. Such a home is built
// once per test binary as well, and every later test gets a copy of it.
//
// A home names the path it was built at in two places. The source's URL
// is recorded in the settings, in the account repo's remote and in the
// trailer of every import commit, so a fixture source is added under a
// URL of its own, fixtureURL, the same in every test, which the harness
// user's git configuration maps to the local repository, as h.rewrite
// does for a developer with a mirror. The placement symlinks name the
// library by its absolute path, and are rewritten as the copy is written.
// Nothing else in a home depends on where it was built: the machine id is
// fixed, the version counter and last_fetched are what the same commands
// would have written, and a finished command leaves no journal behind.

// fixtureHome is one such home: the client directories, the one source,
// and the commands build runs over them, which happens in the first test
// to ask; every later test gets a copy written from memory, with a copy of
// the source of its own to commit to.
type fixtureHome struct {
	source string   // the name of the source, which fixtureURL names it by
	dirs   []string // the client directories of the home
	// build fills the source and runs the commands, once, in the first
	// test to ask; what it returns, every copy gets too.
	build func(h *harness, s *sourceRepo) []string

	once  sync.Once
	built bool     // the build finished; a build that failed leaves it false
	root  string   // the root of the harness the build ran in, which its symlinks name
	image dirImage // everything under that root
	ids   []string
}

// fixtureURL is the URL a fixture source is added under: a GitHub URL,
// the form most sources take, with the subpath and pin syntax a file URL
// has. No such repository exists; every fetch of it goes through the
// harness's mapping to the test's own.
func fixtureURL(name string) string { return "https://github.com/fixtures/" + name }

// copy gives t a harness holding the home, the source at the state build
// left it in, and what build returned.
func (f *fixtureHome) copy(t *testing.T) (*harness, *sourceRepo, []string) {
	t.Helper()
	h := newHarness(t)
	h.build(t, fixture{dirs: f.dirs})
	s := h.newSourceRepo(f.source, true)
	s.url = fixtureURL(f.source)
	root := filepath.Dir(h.home)
	here := false
	f.once.Do(func() {
		h.rewrite(s)
		ids := f.build(h, s)
		image, err := imageOf(root)
		if err != nil {
			t.Fatal(err)
		}
		f.root, f.image, f.ids, f.built, here = root, image, ids, true, true
	})
	if !f.built {
		t.Fatalf("the fixture home over the source %s failed to build in another test", f.source)
	}
	if !here {
		prev, err := imageOf(root)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.image.rebased(f.root, root).writeOver(prev, root); err != nil {
			t.Fatal(err)
		}
		// The copied configuration maps the URL to the source of the home
		// the build ran in; this test fetches from its own.
		h.rewrite(s)
		s.at = nil // the source holds what build committed, a state no image names
	}
	return h, s, slices.Clone(f.ids)
}

// rebased is the image with every symlink into the tree at from pointing
// into the tree at to instead.
func (img dirImage) rebased(from, to string) dirImage {
	out := slices.Clone(img)
	for i, e := range out {
		if e.mode&fs.ModeSymlink == 0 {
			continue
		}
		if target, ok := strings.CutPrefix(string(e.data), from+"/"); ok {
			out[i].data = []byte(filepath.Join(to, target))
		}
	}
	return out
}
