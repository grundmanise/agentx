package cli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
)

// requireGit skips a test that fetches from a real repository when the
// test machine has no git.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed; the source tests need it")
	}
}

// sourceRepo is a source built for a test: a local bare repository with
// skill directories and commits, written through a work tree next to it.
type sourceRepo struct {
	t      *testing.T
	git    *gitx.Runner
	gitDir string
	work   string
	url    string // the file:// URL of the bare repository
}

// newSourceRepo creates the bare repository <name>.git under the harness
// root with main as its branch. partial lets it serve blobless fetches, as
// a hosting service does; without it every fetch is a full one.
func (h *harness) newSourceRepo(name string, partial bool) *sourceRepo {
	h.t.Helper()
	requireGit(h.t)
	root := filepath.Dir(h.home)
	s := &sourceRepo{
		t:      h.t,
		git:    gitx.New(h.env, false, func(string, ...any) {}),
		gitDir: filepath.Join(root, "sources", name+".git"),
		work:   filepath.Join(root, "sources", name),
	}
	s.url = "file://" + s.gitDir
	for _, dir := range []string{s.gitDir, s.work} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			h.t.Fatal(err)
		}
	}
	s.bare("init", "--bare", "--quiet", "--initial-branch=main")
	if partial {
		s.bare("config", "uploadpack.allowFilter", "true")
	}
	return s
}

// run runs git on the bare repository with the work tree attached, which
// lets add and commit write into it.
func (s *sourceRepo) run(args ...string) string {
	s.t.Helper()
	return s.bare(append([]string{"--work-tree=" + s.work}, args...)...)
}

// bare runs git on the bare repository alone.
func (s *sourceRepo) bare(args ...string) string {
	s.t.Helper()
	out, err := s.git.Isolated(context.Background(), s.gitDir, args...)
	if err != nil {
		s.t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return out
}

// write puts content at path inside the work tree.
func (s *sourceRepo) write(path, content string) {
	s.t.Helper()
	full := filepath.Join(s.work, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		s.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		s.t.Fatal(err)
	}
}

// skill writes a SKILL.md with the name and description at dir, plus any
// extra files, so the directory is one installable skill.
func (s *sourceRepo) skill(dir, name, description string, extra map[string]string) {
	s.t.Helper()
	fm := "---\n"
	if name != "" {
		fm += "name: " + name + "\n"
	}
	if description != "" {
		fm += "description: " + description + "\n"
	}
	s.write(filepath.Join(dir, "SKILL.md"), fm+"---\n\n# "+name+"\n")
	for path, content := range extra {
		s.write(filepath.Join(dir, path), content)
	}
}

// commit commits the work tree and returns the commit id.
func (s *sourceRepo) commit(message string) string {
	s.t.Helper()
	s.run("add", "--all")
	s.run("commit", "--quiet", "--allow-empty", "--message", message)
	return s.run("rev-parse", "HEAD")
}

func (s *sourceRepo) tag(name string) { s.t.Helper(); s.run("tag", name) }

// commitAt commits the work tree with an author and committer time of its
// own. Every other fixture commit is made through the isolated environment,
// which fixes the same date agentx falls back to, so a test that wants to
// tell the upstream's committer time from that fallback has to set one.
func (s *sourceRepo) commitAt(message, when string) string {
	s.t.Helper()
	s.run("add", "--all")
	_, err := s.git.IsolatedAt(context.Background(), s.gitDir, when,
		"--work-tree="+s.work, "commit", "--quiet", "--allow-empty", "--message", message)
	if err != nil {
		s.t.Fatalf("git commit: %v", err)
	}
	return s.run("rev-parse", "HEAD")
}

// executable marks a file of the work tree executable, so that the source
// holds it with mode 100755.
func (s *sourceRepo) executable(path string) {
	s.t.Helper()
	if err := os.Chmod(filepath.Join(s.work, path), 0o755); err != nil {
		s.t.Fatal(err)
	}
}

// tree is the tree id of path at the current commit.
func (s *sourceRepo) tree(path string) string { s.t.Helper(); return s.run("rev-parse", "HEAD:"+path) }

// standardSource is the source most tests use: two skills under skills/,
// tagged v1, then a second commit that changes one of them.
func (h *harness) standardSource(partial bool) (*sourceRepo, string, string) {
	h.t.Helper()
	s := h.newSourceRepo("skills", partial)
	s.skill("skills/alpha", "alpha", "The first skill", map[string]string{"notes.md": "alpha notes\n", "scripts/run.sh": "#!/bin/sh\n"})
	s.skill("skills/beta", "beta", "The second skill", nil)
	s.write("README.md", "# skills\n")
	v1 := s.commit("first version")
	s.tag("v1")
	s.skill("skills/alpha", "alpha", "The first skill, revised", map[string]string{"notes.md": "alpha notes, revised\n"})
	head := s.commit("second version")
	return s, v1, head
}

// accountGit runs git on the harness account repo in the isolated
// environment and returns stdout.
func (h *harness) accountGit(args ...string) string {
	h.t.Helper()
	r := gitx.New(h.env, false, func(string, ...any) {})
	out, err := r.Isolated(context.Background(), gitx.AccountRepoPath(h.agentx), args...)
	if err != nil {
		h.t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return out
}

// accountGitErr is accountGit for a command expected to fail.
func (h *harness) accountGitErr(args ...string) (string, error) {
	h.t.Helper()
	r := gitx.New(h.env, false, func(string, ...any) {})
	return r.Isolated(context.Background(), gitx.AccountRepoPath(h.agentx), args...)
}

// fetches counts the git fetch commands in the verbose stderr of one run.
func fetches(stderr string) int {
	n := 0
	for _, line := range strings.Split(stderr, "\n") {
		if strings.HasPrefix(line, "debug: git ") && strings.Contains(line, " fetch --") {
			n++
		}
	}
	return n
}

// rewrite makes the user's git configuration of the harness map every
// canonical URL to the local repository, the way url.<base>.insteadOf does
// for a developer with a mirror, so a GitHub or GitLab URL fetches locally.
func (h *harness) rewrite(s *sourceRepo, canonical ...string) {
	h.t.Helper()
	var b strings.Builder
	b.WriteString("[url \"" + s.url + "\"]\n")
	for _, c := range canonical {
		b.WriteString("\tinsteadOf = " + c + "\n")
	}
	if err := os.WriteFile(filepath.Join(h.home, ".gitconfig"), []byte(b.String()), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

// annotatedTag puts an annotated tag on the current commit, so the source
// ref fetched from it names a tag object rather than the commit itself.
func (s *sourceRepo) annotatedTag(name, message string) {
	s.t.Helper()
	s.run("tag", "--annotate", "--message", message, name)
}

// refuseObjectFetch puts a git wrapper alone on the harness PATH that hands
// every invocation to the real git except one: a fetch carrying --stdin,
// the by-object-id batch that fills in the SKILL.md blobs of a partial
// clone. That one it fails the way a server refusing a want for an
// unadvertised object does, which is what drives the --refetch --no-filter
// fallback of source.Fetch.
//
// No bare repository can be configured to behave this way. git's
// upload-pack turns on allow-any-sha1-in-want whenever
// uploadpack.allowFilter is set — a partial clone would be unusable
// otherwise — and setting uploadpack.allowAnySHA1InWant,
// allowReachableSHA1InWant and allowTipSHA1InWant to false does not take it
// back. A server that serves a filtered fetch therefore always serves
// single objects too, so the refusal has to come from the wrapper.
func refuseObjectFetch(t *testing.T, h *harness) {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	stubGit(t, h, `#!/bin/sh
sub= ; stdin=
for arg in "$@"; do
	case "$arg" in
	fetch) sub=fetch ;;
	--stdin) stdin=1 ;;
	esac
done
if [ "$sub" = fetch ] && [ -n "$stdin" ]; then
	while read -r _; do :; done   # drain the object ids: PATH holds git alone, so no cat
	echo "error: Server does not allow request for unadvertised object" >&2
	exit 128
fi
exec `+real+` "$@"
`)
}

// missingObjects counts the objects of ref that the account repo does not
// hold, which rev-list prints with a leading question mark.
func (h *harness) missingObjects(ref string) int {
	h.t.Helper()
	n := 0
	for _, line := range strings.Split(h.accountGit("rev-list", "--objects", "--missing=print", ref), "\n") {
		if strings.HasPrefix(line, "?") {
			n++
		}
	}
	return n
}

// gateGit puts a git wrapper alone on the harness PATH that can hold one
// git invocation open, so that a test can look at agentx home from the
// middle of a command. test is the shell that decides which invocation:
// it sets gate=1 for the one to hold.
//
// The wrapper gates only while the release fifo exists, which is what lets
// it be installed before serve starts: the runner looks git up in the
// harness environment's PATH on every call, so a wrapper put there later
// would gate the serve child's own git too.
//
// The returned arm opens the gate for the next such invocation and hands
// back reached, which waits until it is inside the wrapper, and release,
// which lets it run and shuts the gate behind it. Both ends meet on a fifo:
// nothing sleeps and nothing spins.
func gateGit(t *testing.T, h *harness, test string) (arm func() (reached, release func())) {
	t.Helper()
	requireGit(t)
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	at, let := filepath.Join(dir, "at"), filepath.Join(dir, "release")
	stubGit(t, h, `#!/bin/sh
gate=
`+test+`
if [ -n "$gate" ] && [ -p `+let+` ]; then
	echo at > `+at+`
	read _ < `+let+`
fi
exec `+real+` "$@"
`)
	return func() (func(), func()) {
		t.Helper()
		for _, p := range []string{at, let} {
			if err := syscall.Mkfifo(p, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return func() { readFifo(t, at) }, func() {
			writeFifo(t, let)
			for _, p := range []string{at, let} {
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}

// gateObjectFetch holds the by-object-id blob batch of source.Fetch open:
// the point between the fetch that brings a source's new commit and the one
// that brings that commit's SKILL.md blobs, which is where a source is in
// the account repo and not yet listable.
func gateObjectFetch(t *testing.T, h *harness) (arm func() (reached, release func())) {
	t.Helper()
	return gateGit(t, h, `sub= ; stdin=
for arg in "$@"; do
	case "$arg" in
	fetch) sub=fetch ;;
	--stdin) stdin=1 ;;
	esac
done
[ "$sub" = fetch ] && [ -n "$stdin" ] && gate=1`)
}

// gatePublish holds open the update-ref that puts a whole fetch on the
// source ref, which is the last thing a fetch does: a command parked there
// has everything it needs and has told nobody yet. A deletion carries -d
// and is never held, so a removal can run while a fetch waits here.
func gatePublish(t *testing.T, h *harness) (arm func() (reached, release func())) {
	t.Helper()
	return gateGit(t, h, `sub= ; del=
for arg in "$@"; do
	case "$arg" in
	update-ref) sub=update-ref ;;
	-d) del=1 ;;
	esac
done
[ "$sub" = update-ref ] && [ -z "$del" ] && gate=1`)
}

// readFifo waits for the other end to write, and writeFifo lets it
// through. Opening a fifo waits for the other end, so the open runs in a
// goroutine and the wait carries the deadline of the rest of the harness
// rather than hanging a test whose wrapper is never reached.
func readFifo(t *testing.T, path string) {
	t.Helper()
	awaitFifo(t, path, func(f *os.File) error {
		_, err := f.Read(make([]byte, 1))
		return err
	})
}

func writeFifo(t *testing.T, path string) {
	t.Helper()
	awaitFifo(t, path, func(f *os.File) error {
		_, err := f.Write([]byte("go\n"))
		return err
	})
}

func awaitFifo(t *testing.T, path string, use func(*os.File) error) {
	t.Helper()
	flag := os.O_RDONLY
	if strings.HasSuffix(path, "release") {
		flag = os.O_WRONLY
	}
	done := make(chan error, 1)
	go func() {
		f, err := os.OpenFile(path, flag, 0)
		if err != nil {
			done <- err
			return
		}
		done <- errors.Join(use(f), f.Close())
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(serveDeadline):
		t.Fatalf("no other end on %s within %s", path, serveDeadline)
	}
}

// failObjectFetch puts a git wrapper alone on the harness PATH that fails
// both fetches source.Fetch can fill a partial clone's SKILL.md blobs with:
// the by-object-id batch and the --refetch --no-filter fallback behind it.
// Every other invocation is the real git, so the blobless fetch of the ref
// still lands and the fetch fails with the new commit in the account repo
// and its blobs not, which is the state a source ref may never name.
//
// With remotes named, only those sources fail and the rest of a run fetches
// normally, which is what a partial run over several sources looks like.
func failObjectFetch(t *testing.T, h *harness, remotes ...string) {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	mine, arm := "1", ""
	if len(remotes) > 0 {
		mine, arm = "", "\t"+strings.Join(remotes, "|")+") mine=1 ;;\n"
	}
	stubGit(t, h, `#!/bin/sh
sub= ; stdin= ; refetch= ; mine=`+mine+`
for arg in "$@"; do
	case "$arg" in
	fetch) sub=fetch ;;
	--stdin) stdin=1 ;;
	--refetch) refetch=1 ;;
`+arm+`	esac
done
if [ "$sub" = fetch ] && [ -n "$mine" ] && { [ -n "$stdin" ] || [ -n "$refetch" ]; }; then
	if [ -n "$stdin" ]; then
		while read -r _; do :; done   # drain the object ids: PATH holds git alone, so no cat
	fi
	echo "error: Server does not allow request for unadvertised object" >&2
	exit 128
fi
exec `+real+` "$@"
`)
}

// dropObjectFetch puts a git wrapper alone on the harness PATH that answers
// the by-object-id blob batch without doing anything: exit 0, no objects.
// No git server behaves this way; what it stands for is anything that
// reports a batch as served without every object arriving, which would
// leave a source ref naming a commit whose skills no listing can read.
func dropObjectFetch(t *testing.T, h *harness) {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	stubGit(t, h, `#!/bin/sh
sub= ; stdin=
for arg in "$@"; do
	case "$arg" in
	fetch) sub=fetch ;;
	--stdin) stdin=1 ;;
	esac
done
if [ "$sub" = fetch ] && [ -n "$stdin" ]; then
	while read -r _; do :; done   # drain the object ids and bring none of them
	exit 0
fi
exec `+real+` "$@"
`)
}

// agentxRefs lists every ref the account repo holds under refs/agentx.
func (h *harness) agentxRefs() string {
	h.t.Helper()
	return h.accountGit("for-each-ref", "--format=%(refname)", "refs/agentx/")
}

// failLocalGit puts a git wrapper alone on the harness PATH that fails one
// local subcommand and hands every other invocation to the real git, which
// is how a test reaches the account repo's own git failing on a read: the
// exit code table calls that 8, whatever command ran into it.
func failLocalGit(t *testing.T, h *harness, sub string) {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	stubGit(t, h, `#!/bin/sh
for arg in "$@"; do
	case "$arg" in
	-c|--git-dir=*) ;;
	`+sub+`)
		echo "fatal: simulated local git failure" >&2
		exit 128 ;;
	esac
done
exec `+real+` "$@"
`)
}

// lastError is the error event of a run, which follows whatever the run
// emitted before it failed.
func lastError(t *testing.T, events []jsonEvent) jsonEvent {
	t.Helper()
	for i := len(events) - 1; i >= 0; i-- {
		if events[i]["type"] == "error" {
			return events[i]
		}
	}
	t.Fatalf("no error event in %v", events)
	return nil
}
