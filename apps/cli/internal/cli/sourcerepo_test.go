package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
