package source_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
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
// URL, and fetched into a store the way source add does.
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
	if err := source.Configure(context.Background(), r.git, gitDir, home.Source{URL: src.URL, Pin: src.Ref}); err != nil {
		r.t.Fatal(err)
	}
	if _, err := source.Fetch(context.Background(), r.git, gitDir, src); err != nil {
		r.t.Fatal(err)
	}
}

// store is a store in a temporary agentx home.
func store(t *testing.T) (*gitx.Runner, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed; the source tests need it")
	}
	r := gitx.New(map[string]string{"PATH": os.Getenv("PATH"), "HOME": t.TempDir()}, false, func(string, ...any) {})
	gitDir, _, err := gitx.OpenStore(context.Background(), r, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return r, gitDir
}

// countingStore is store whose runner counts the git processes
// it starts, so a test can hold BuildIndex to the one for-each-ref a
// rebuild that finds nothing changed is allowed. The runner logs one line
// per process and one more for a child's stderr; only the first has this
// format.
func countingStore(t *testing.T) (*gitx.Runner, string, func() int) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed; the source tests need it")
	}
	var mu sync.Mutex
	started := 0
	r := gitx.New(map[string]string{"PATH": os.Getenv("PATH"), "HOME": t.TempDir()}, false, func(format string, _ ...any) {
		if format != "git %s" {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		started++
	})
	gitDir, _, err := gitx.OpenStore(context.Background(), r, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return r, gitDir, func() int { mu.Lock(); defer mu.Unlock(); return started }
}

// TestBuildIndexListsASourceItCouldNotListAgain covers a build that met a
// source the store could not answer for. That answer belongs to the
// moment, not to the commit, so it is not remembered: the next build lists
// the source again and its skills come back without its ref having moved.
// A build that could list everything is still remembered whole, so a
// rebuild with nothing changed costs the one for-each-ref and no more.
func TestBuildIndexListsASourceItCouldNotListAgain(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	git, gitDir, started := countingStore(t)
	ctx := context.Background()

	r := newRepo(t, root, "alpha")
	r.run("config", "uploadpack.allowFilter", "true") // a server that really filters
	r.skill("commit", "commit", "Write a commit message")
	commit := r.commit()
	urls := []string{r.url}

	// A source ref at a commit whose SKILL.md blobs are not here. A fetch no
	// longer leaves this behind: it stages until it is whole, and the source
	// ref is published last. What is left is everything outside that path –
	// a killed process, objects reclaimed while the ref stands, another
	// writer of the store – so the state is planted here rather than
	// fetched into being, since the point of the index is to recover from it
	// however it arose. The source repository is then moved aside, so that
	// no git old enough to ignore GIT_NO_LAZY_FETCH can fill the blobs in
	// behind the test's back.
	src := source.Source{URL: r.url}
	if err := source.Configure(ctx, git, gitDir, home.Source{URL: src.URL, Pin: src.Ref}); err != nil {
		t.Fatal(err)
	}
	if _, err := git.User(ctx, gitDir, "fetch", "--quiet", "--no-tags", "--filter=blob:none", source.RemoteName(src.ID())); err != nil {
		t.Fatal(err)
	}
	if _, err := git.Isolated(ctx, gitDir, "update-ref", source.Ref(src.ID()), commit); err != nil {
		t.Fatal(err)
	}
	aside := r.gitDir + ".aside"
	if err := os.Rename(r.gitDir, aside); err != nil {
		t.Fatal(err)
	}

	idx, warnings, err := source.BuildIndex(ctx, git, gitDir, urls, source.Hub{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "the fetched source is incomplete") {
		t.Fatalf("warnings = %q, want one naming an incomplete source", warnings)
	}
	if got := idx.Search("commit"); len(got) != 0 {
		t.Fatalf("results while the source cannot be listed = %#v, want none", got)
	}

	// The blobs arrive. The ref does not move: only the skills the home
	// repo can now read are new.
	if err := os.Rename(aside, r.gitDir); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Fetch(ctx, git, gitDir, src); err != nil {
		t.Fatal(err)
	}
	if got, err := git.Isolated(ctx, gitDir, "rev-parse", source.Ref(src.ID())); err != nil || got != commit {
		t.Fatalf("the source ref = %q, %v; want the commit %q it already held", got, err, commit)
	}
	next, warnings, err := source.BuildIndex(ctx, git, gitDir, urls, source.Hub{}, idx)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings after the blobs arrived = %q, want none", warnings)
	}
	want := []source.Match{{Source: r.url, Subpath: "commit", Name: "commit", Description: "Write a commit message", Tree: r.tree("commit")}}
	if got := next.Search("commit"); !reflect.DeepEqual(got, want) {
		t.Fatalf("results after the blobs arrived = %#v\nwant %#v", got, want)
	}

	// Everything listed, so the build is remembered whole: the rebuild that
	// follows reads the refs and stops there.
	before := started()
	again, warnings, err := source.BuildIndex(ctx, git, gitDir, urls, source.Hub{}, next)
	if err != nil || again != next || warnings != nil {
		t.Errorf("BuildIndex with an unchanged prev = %p, %v, %v; want prev %p and no warnings", again, warnings, err, next)
	}
	if got := started() - before; got != 1 {
		t.Errorf("a rebuild with nothing changed started %d git processes, want 1: the for-each-ref", got)
	}
}

func TestBuildIndexListsEverySkillOfEverySource(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	git, gitDir := store(t)
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
	idx, warnings, err := source.BuildIndex(ctx, git, gitDir, urls, source.Hub{}, nil)
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
	if len(warnings) != 0 {
		t.Errorf("warnings = %q, want none: a source not fetched is no warning", warnings)
	}
	if got := idx.Search("lint"); len(got) != 1 || got[0].Source != alpha.url || got[0].Subpath != "tools/lint" {
		t.Errorf("Search(lint) = %#v", got)
	}

	// Nothing changed: the index that was built stands, and no listing runs.
	again, warnings, err := source.BuildIndex(ctx, git, gitDir, urls, source.Hub{}, idx)
	if err != nil || again != idx || warnings != nil {
		t.Errorf("BuildIndex with an unchanged prev = %p, %v, %v; want prev %p and no warnings", again, warnings, err, idx)
	}

	// A source fetched again is a new index; a source dropped from the
	// settings leaves it.
	alpha.skill("commit", "commit", "Write a better commit message")
	alpha.commit()
	alpha.fetch(gitDir)
	idx2, _, err := source.BuildIndex(ctx, git, gitDir, []string{alpha.url}, source.Hub{}, idx)
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

// TestBuildIndexNeverWarnsAboutAnUnfetchedSource covers the rebuilds that
// follow a source never fetched while another source's ref moves, as the
// serve child's update checks move it. No build warns about the source
// not fetched, also when it leaves the settings and comes back: the update
// check that fetches it is what says when it cannot.
func TestBuildIndexNeverWarnsAboutAnUnfetchedSource(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	git, gitDir := store(t)
	ctx := context.Background()

	alpha := newRepo(t, root, "alpha")
	alpha.skill("commit", "commit", "Write a commit message")
	alpha.commit()
	alpha.fetch(gitDir)
	never := "file://" + filepath.Join(root, "never-fetched")
	urls := []string{alpha.url, never}

	idx, warnings, err := source.BuildIndex(ctx, git, gitDir, urls, source.Hub{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings of the first build = %q, want none", warnings)
	}

	// alpha moves twice; each rebuild is a new index, and neither warns.
	for _, name := range []string{"lint", "deploy"} {
		alpha.skill(name, name, "Added upstream")
		alpha.commit()
		alpha.fetch(gitDir)
		next, warnings, err := source.BuildIndex(ctx, git, gitDir, urls, source.Hub{}, idx)
		if err != nil {
			t.Fatal(err)
		}
		if next == idx {
			t.Fatalf("the build after alpha moved to %s returned prev", name)
		}
		if len(warnings) != 0 {
			t.Errorf("warnings of the build after alpha moved to %s = %q, want none", name, warnings)
		}
		if got := next.Search(name); len(got) != 1 || got[0].Source != alpha.url {
			t.Errorf("Search(%s) after alpha moved = %#v", name, got)
		}
		idx = next
	}

	// Gone from the settings and back, unfetched still: no warning either.
	idx, warnings, err = source.BuildIndex(ctx, git, gitDir, []string{alpha.url}, source.Hub{}, idx)
	if err != nil || len(warnings) != 0 {
		t.Fatalf("the build without the unfetched source = %v, %q; want no warnings", err, warnings)
	}
	_, warnings, err = source.BuildIndex(ctx, git, gitDir, urls, source.Hub{}, idx)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings of the build it came back in = %q, want none", warnings)
	}
}

func TestBuildIndexWithoutSourcesRunsNoGit(t *testing.T) {
	t.Parallel()
	// No git on PATH at all: a machine with no source never reaches one.
	git := gitx.New(map[string]string{"PATH": t.TempDir()}, false, func(string, ...any) {})
	gitDir := filepath.Join(t.TempDir(), "store.git")
	idx, warnings, err := source.BuildIndex(context.Background(), git, gitDir, nil, source.Hub{}, nil)
	if err != nil || len(warnings) != 0 || len(idx.Sources) != 0 || len(idx.Search("anything")) != 0 {
		t.Errorf("BuildIndex() = %#v, %v, %v; want an empty index", idx, warnings, err)
	}

	// A source recorded before the store exists is one not fetched.
	idx, warnings, err = source.BuildIndex(context.Background(), git, gitDir, []string{"https://example.com/a"}, source.Hub{}, nil)
	if err != nil || len(warnings) != 0 || len(idx.Sources) != 1 || len(idx.Search("a")) != 0 {
		t.Errorf("BuildIndex() = %#v, %v, %v; want one unfetched source", idx, warnings, err)
	}
}

// fork commits a hub fork's branch, skills/<name>, holding one skill
// directory, dir, as the one entry of its root, as a fork commit does, and
// returns its tip.
func (r *repo) fork(name, dir, description string) string {
	r.t.Helper()
	r.run("symbolic-ref", "HEAD", "refs/heads/skills/"+name)
	r.run("read-tree", "--empty")
	if err := os.RemoveAll(r.work); err != nil {
		r.t.Fatal(err)
	}
	r.skill(dir, name, description)
	return r.commit()
}

func TestBuildIndexListsTheHubsForksByBranch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	git, gitDir, started := countingStore(t)
	ctx := context.Background()

	hub := newRepo(t, root, "hub")
	pdf := hub.fork("pdf-tools", "pdf", "Fill PDF forms")
	notes := hub.fork("notes", "notes", "Keep meeting notes")
	if _, err := git.Isolated(ctx, gitDir, "fetch", "--quiet", hub.url, "+refs/heads/skills/*:refs/remotes/hub/skills/*"); err != nil {
		t.Fatal(err)
	}
	tree := func(tip, dir string) string { return hub.run("rev-parse", tip+":"+dir) }
	h := source.Hub{URL: hub.url, Tips: map[string]string{"pdf-tools": pdf, "notes": notes}}

	idx, warnings, err := source.BuildIndex(ctx, git, gitDir, nil, h, nil)
	if err != nil || len(warnings) != 0 {
		t.Fatalf("BuildIndex() = %v, %q", err, warnings)
	}
	want := []source.Indexed{{URL: hub.url, Skills: []source.Skill{
		{Name: "notes", Description: "Keep meeting notes", Tree: tree(notes, "notes")},
		{Name: "pdf-tools", Description: "Fill PDF forms", Tree: tree(pdf, "pdf")},
	}}}
	if !reflect.DeepEqual(idx.Sources, want) {
		t.Errorf("index = %#v\nwant %#v", idx.Sources, want)
	}
	if got := idx.Search("forms pdf"); len(got) != 0 {
		t.Errorf("Search(forms pdf) = %#v, want none", got)
	}
	if got := idx.Search("FORMS"); len(got) != 1 || got[0].Source != hub.url || got[0].Name != "pdf-tools" {
		t.Errorf("Search(FORMS) = %#v, want pdf-tools on the hub", got)
	}

	// The same tips: the index stands, and no git runs.
	before := started()
	again, _, err := source.BuildIndex(ctx, git, gitDir, nil, source.Hub{URL: hub.url, Tips: map[string]string{"notes": notes, "pdf-tools": pdf}}, idx)
	if err != nil || again != idx {
		t.Errorf("BuildIndex with the same tips = %p, %v; want prev %p", again, err, idx)
	}
	if got := started() - before; got != 0 {
		t.Errorf("a rebuild with the same tips started %d git processes, want 0", got)
	}

	// A fork published again is read again.
	newer := hub.fork("notes", "notes", "Keep and search meeting notes")
	if _, err := git.Isolated(ctx, gitDir, "fetch", "--quiet", hub.url, "+refs/heads/skills/*:refs/remotes/hub/skills/*"); err != nil {
		t.Fatal(err)
	}
	next, _, err := source.BuildIndex(ctx, git, gitDir, nil, source.Hub{URL: hub.url, Tips: map[string]string{"notes": newer, "pdf-tools": pdf}}, idx)
	if err != nil {
		t.Fatal(err)
	}
	if got := next.Search("search meeting"); len(got) != 1 || got[0].Tree != tree(newer, "notes") {
		t.Errorf("Search(search meeting) after the publish = %#v", got)
	}
}
