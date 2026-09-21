package source_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

func TestSearchMatchesNameAndDescriptionCaseInsensitively(t *testing.T) {
	t.Parallel()
	idx := &source.Index{Sources: []source.Indexed{
		{URL: "https://example.com/b", Skills: []source.Skill{
			{Subpath: "commit", Name: "commit", Description: "Write a commit message", Tree: "t1"},
			{Subpath: "old/commit", Name: "commit", Description: "The one before", Tree: "t2"},
		}},
		{URL: "https://example.com/a", Skills: []source.Skill{
			{Subpath: "review", Name: "review", Description: "Review a COMMIT before it lands", Tree: "t3"},
			{Subpath: "deploy", Name: "deploy", Description: "Ship it", Tree: "t4"},
		}},
	}}
	// Sorted by source canonical URL, then name, then subpath.
	want := []source.Match{
		{Source: "https://example.com/a", Subpath: "review", Name: "review", Description: "Review a COMMIT before it lands", Tree: "t3"},
		{Source: "https://example.com/b", Subpath: "commit", Name: "commit", Description: "Write a commit message", Tree: "t1"},
		{Source: "https://example.com/b", Subpath: "old/commit", Name: "commit", Description: "The one before", Tree: "t2"},
	}
	if got := idx.Search("commit"); !reflect.DeepEqual(got, want) {
		t.Errorf("Search(commit) = %#v\nwant %#v", got, want)
	}
	if got := idx.Search("SHIP"); len(got) != 1 || got[0].Name != "deploy" {
		t.Errorf("Search(SHIP) = %#v, want the deploy skill", got)
	}
	// Every skill matches the empty query, but serve never sends one.
	if got := idx.Search(""); len(got) != 4 {
		t.Errorf("Search() matched %d skills, want all 4", len(got))
	}
	for _, idx := range []*source.Index{idx, nil} {
		if got := idx.Search("nothing here"); got == nil || len(got) != 0 {
			t.Errorf("Search(nothing here) = %#v, want an empty, non-nil slice", got)
		}
	}
}

// repo is a bare repository with skill directories, reached by its file://
// URL, and fetched into an account repo the way source add does.
type repo struct {
	t      *testing.T
	git    *gitx.Runner
	gitDir string
	work   string
	url    string
}

func newRepo(t *testing.T, root, name string) *repo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed; the source tests need it")
	}
	r := &repo{
		t:      t,
		git:    gitx.New(map[string]string{"PATH": os.Getenv("PATH")}, false, func(string, ...any) {}),
		gitDir: filepath.Join(root, name+".git"),
		work:   filepath.Join(root, name),
	}
	r.url = "file://" + r.gitDir
	for _, dir := range []string{r.gitDir, r.work} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	r.run("init", "--bare", "--quiet", "--initial-branch=main")
	return r
}

func (r *repo) run(args ...string) string {
	r.t.Helper()
	if args[0] != "init" {
		args = append([]string{"--work-tree=" + r.work}, args...)
	}
	out, err := r.git.Isolated(context.Background(), r.gitDir, args...)
	if err != nil {
		r.t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return out
}

// skill writes the SKILL.md of one skill at dir.
func (r *repo) skill(dir, name, description string) {
	r.t.Helper()
	full := filepath.Join(r.work, dir)
	if err := os.MkdirAll(full, 0o755); err != nil {
		r.t.Fatal(err)
	}
	fm := "---\nname: " + name + "\ndescription: " + description + "\n---\n\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(full, "SKILL.md"), []byte(fm), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *repo) commit() string {
	r.t.Helper()
	r.run("add", "--all")
	r.run("commit", "--quiet", "--allow-empty", "--message", "skills")
	return r.run("rev-parse", "HEAD")
}

func (r *repo) tree(path string) string { r.t.Helper(); return r.run("rev-parse", "HEAD:"+path) }

// fetch puts the repository's HEAD under its source ref in gitDir, which is
// the state source add leaves behind.
func (r *repo) fetch(gitDir string) {
	r.t.Helper()
	src := source.Source{URL: r.url}
	if err := source.Configure(context.Background(), r.git, gitDir, src); err != nil {
		r.t.Fatal(err)
	}
	if _, err := source.Fetch(context.Background(), r.git, gitDir, src); err != nil {
		r.t.Fatal(err)
	}
}

// accountRepo is an account repo in a temporary agentx home.
func accountRepo(t *testing.T) (*gitx.Runner, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed; the source tests need it")
	}
	r := gitx.New(map[string]string{"PATH": os.Getenv("PATH"), "HOME": t.TempDir()}, false, func(string, ...any) {})
	gitDir, _, err := gitx.OpenAccountRepo(context.Background(), r, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return r, gitDir
}

func TestBuildIndexListsEverySkillOfEverySource(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	git, gitDir := accountRepo(t)
	ctx := context.Background()

	alpha := newRepo(t, root, "alpha")
	alpha.skill("commit", "commit", "Write a commit message")
	alpha.skill("tools/lint", "lint", "Lint the code")
	alpha.skill(".hidden/secret", "secret", "Never listed")
	alpha.skill("node_modules/dep", "dep", "A dependency")
	alpha.skill("deep/.cache/skill", "cached", "Under a hidden directory")
	alphaCommit := alpha.commit()
	beta := newRepo(t, root, "beta")
	beta.skill("deploy", "deploy", "Ship it")
	betaCommit := beta.commit()
	alpha.fetch(gitDir)
	beta.fetch(gitDir)
	never := "file://" + filepath.Join(root, "never-fetched")

	// The URLs arrive as the settings hold them, in any order and with a
	// repeat; the index is by canonical URL, once each.
	urls := []string{never, beta.url, alpha.url, alpha.url}
	idx, warnings, err := source.BuildIndex(ctx, git, gitDir, urls, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []source.Indexed{
		{URL: alpha.url, Commit: alphaCommit, Skills: []source.Skill{
			{Subpath: "commit", Name: "commit", Description: "Write a commit message", Tree: alpha.tree("commit")},
			{Subpath: "tools/lint", Name: "lint", Description: "Lint the code", Tree: alpha.tree("tools/lint")},
		}},
		{URL: beta.url, Commit: betaCommit, Skills: []source.Skill{
			{Subpath: "deploy", Name: "deploy", Description: "Ship it", Tree: beta.tree("deploy")},
		}},
		{URL: never, Commit: "", Skills: []source.Skill{}},
	}
	if !reflect.DeepEqual(idx.Sources, want) {
		t.Errorf("index = %#v\nwant %#v", idx.Sources, want)
	}
	if got, want := warnings, []string{"source " + never + " has not been fetched, its skills cannot be searched"}; !reflect.DeepEqual(got, want) {
		t.Errorf("warnings = %q, want %q", got, want)
	}
	if got := idx.Search("lint"); len(got) != 1 || got[0].Source != alpha.url || got[0].Subpath != "tools/lint" {
		t.Errorf("Search(lint) = %#v", got)
	}

	// Nothing changed: the index that was built stands, and no listing runs.
	again, warnings, err := source.BuildIndex(ctx, git, gitDir, urls, idx)
	if err != nil || again != idx || warnings != nil {
		t.Errorf("BuildIndex with an unchanged prev = %p, %v, %v; want prev %p and no warnings", again, warnings, err, idx)
	}

	// A source fetched again is a new index; a source dropped from the
	// settings leaves it.
	alpha.skill("commit", "commit", "Write a better commit message")
	alpha.commit()
	alpha.fetch(gitDir)
	idx2, _, err := source.BuildIndex(ctx, git, gitDir, []string{alpha.url}, idx)
	if err != nil {
		t.Fatal(err)
	}
	if got := idx2.Search("better"); len(got) != 1 || got[0].Tree != alpha.tree("commit") {
		t.Errorf("Search(better) after the fetch = %#v", got)
	}
	if len(idx2.Sources) != 1 {
		t.Errorf("sources after beta was dropped = %d, want 1", len(idx2.Sources))
	}
}

func TestBuildIndexWithoutSourcesRunsNoGit(t *testing.T) {
	t.Parallel()
	// No git on PATH at all: a machine with no source never reaches one.
	git := gitx.New(map[string]string{"PATH": t.TempDir()}, false, func(string, ...any) {})
	gitDir := filepath.Join(t.TempDir(), "account.git")
	idx, warnings, err := source.BuildIndex(context.Background(), git, gitDir, nil, nil)
	if err != nil || len(warnings) != 0 || len(idx.Sources) != 0 || len(idx.Search("anything")) != 0 {
		t.Errorf("BuildIndex() = %#v, %v, %v; want an empty index", idx, warnings, err)
	}

	// A source recorded before the account repo exists is one not fetched.
	idx, warnings, err = source.BuildIndex(context.Background(), git, gitDir, []string{"https://example.com/a"}, nil)
	if err != nil || len(warnings) != 1 || len(idx.Sources) != 1 || len(idx.Search("a")) != 0 {
		t.Errorf("BuildIndex() = %#v, %v, %v; want one unfetched source", idx, warnings, err)
	}
}
