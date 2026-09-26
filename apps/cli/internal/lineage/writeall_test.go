package lineage

import (
	"context"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

// TestWriteAllMatchesCommitTree is the guarantee the batch rests on: the
// commit fast-import writes for a version is byte for byte the commit
// git commit-tree writes for it, so a skill installed on its own, a skill
// installed in a batch of one and the same skill installed among thirty all
// end at the same id. commit-tree is the oracle here and nothing else: it
// is the one writer whose output this project never chose.
//
// The versions cover what could tell the two apart: a plain skill, a skill
// whose message carries characters outside ASCII, so that a byte count
// taken as a character count would break, and a skill whose tree has to be
// written anew because it holds a symlink an import leaves out.
func TestWriteAllMatchesCommitTree(t *testing.T) {
	t.Parallel()
	requireGit(t)
	ctx := context.Background()
	r, gitDir := newRepo(t)

	plain := skillTree(t, ctx, r, gitDir, map[string]string{"SKILL.md": "---\nname: alpha\n---\n", "notes.md": "notes\n"}, "")
	linked := skillTree(t, ctx, r, gitDir, map[string]string{"SKILL.md": "---\nname: gamma\n---\n", "sub/keep.md": "keep\n"}, "sub/away")
	versions := []Version{
		{
			Import:  Import{Source: "https://github.com/example/skills", Path: "skills/alpha", Commit: commitID, Hash: hashID},
			Dir:     "alpha",
			Tree:    plain.tree,
			Entries: plain.entries,
			When:    "1700000000 +0000",
		},
		{
			// A source whose URL and path hold multi-byte characters: the
			// message is counted in bytes, not in characters.
			Import:  Import{Source: "https://github.com/exämple/skïlls", Path: "skills/bëta", Commit: commitID, Hash: hashID},
			Dir:     "bëta",
			Tree:    plain.tree,
			Entries: plain.entries,
			When:    "1700000001 +0000",
		},
		{
			Import:  Import{Source: "https://github.com/example/skills", Path: "skills/gamma", Commit: commitID, Hash: hashID},
			Dir:     "gamma",
			Tree:    linked.tree,
			Entries: linked.entries,
			When:    "1700000002 +0000",
		},
	}

	run := NewRun()
	got, _, err := WriteAll(ctx, r, gitDir, run, versions)
	if err != nil {
		t.Fatalf("write all: %v", err)
	}
	if len(got) != len(versions) {
		t.Fatalf("%d commits for %d versions", len(got), len(versions))
	}
	for i, v := range versions {
		want := commitTree(t, ctx, r, gitDir, v)
		if got[i] != want {
			t.Errorf("fast-import wrote %s for %s, commit-tree wrote %s", got[i], v.Dir, want)
		}
		// And the commit is what the contract says it is, whichever writer
		// made it: parentless, and carrying the tree the version was built
		// from rather than one fast-import assembled.
		if parents, err := r.Isolated(ctx, gitDir, "rev-list", "--count", got[i]); err != nil || parents != "1" {
			t.Errorf("%s has %s commits behind it, want it parentless: %v", v.Dir, parents, err)
		}
	}

	// The commits wait on this run's staging refs until the branches are
	// written, and one call takes every one of them away again.
	refs, err := r.Isolated(ctx, gitDir, "for-each-ref", "--format=%(refname)", ImportingPrefix)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(strings.Fields(refs)); n != len(versions) {
		t.Errorf("%d staging refs for %d versions:\n%s", n, len(versions), refs)
	}
	if err := DropImporting(ctx, r, gitDir, run, len(versions)); err != nil {
		t.Fatalf("dropping the staging refs: %v", err)
	}
	if left, err := r.Isolated(ctx, gitDir, "for-each-ref", "--format=%(refname)", ImportingPrefix); err != nil || left != "" {
		t.Errorf("staging refs left behind: %q %v", left, err)
	}
	// Dropping them again is not an error: cleanup runs after a run that
	// may already have had it done for it.
	if err := DropImporting(ctx, r, gitDir, run, len(versions)); err != nil {
		t.Errorf("dropping the staging refs again: %v", err)
	}
}

// TestWriteAllOfOneIsTheSameAsOfMany writes one version alone and the same
// version inside a batch, in two repositories, and compares: a batch of one
// and a batch of thirty write the same commit for the same version.
func TestWriteAllOfOneIsTheSameAsOfMany(t *testing.T) {
	t.Parallel()
	requireGit(t)
	ctx := context.Background()
	alone, aloneDir := newRepo(t)
	together, togetherDir := newRepo(t)

	build := func(r *gitx.Runner, gitDir string) []Version {
		one := skillTree(t, ctx, r, gitDir, map[string]string{"SKILL.md": "---\nname: alpha\n---\n"}, "")
		two := skillTree(t, ctx, r, gitDir, map[string]string{"SKILL.md": "---\nname: beta\n---\n", "notes.md": "b\n"}, "")
		return []Version{
			{Import: Import{Source: "https://github.com/example/skills", Path: "skills/alpha", Commit: commitID, Hash: hashID},
				Dir: "alpha", Tree: one.tree, Entries: one.entries, When: "1700000000 +0000"},
			{Import: Import{Source: "https://github.com/example/skills", Path: "skills/beta", Commit: commitID, Hash: hashID},
				Dir: "beta", Tree: two.tree, Entries: two.entries, When: "1700000000 +0000"},
		}
	}
	single, _, err := WriteAll(ctx, alone, aloneDir, NewRun(), build(alone, aloneDir)[:1])
	if err != nil {
		t.Fatalf("writing one: %v", err)
	}
	batch, _, err := WriteAll(ctx, together, togetherDir, NewRun(), build(together, togetherDir))
	if err != nil {
		t.Fatalf("writing a batch: %v", err)
	}
	if single[0] != batch[0] {
		t.Errorf("alone the version is %s, in a batch %s", single[0], batch[0])
	}
}

// commitTree writes the import commit of v the way a command that installs
// one skill at a time would: one mktree for the root and one commit-tree
// over it, with the dates of this one call fixed to the upstream's.
func commitTree(t *testing.T, ctx context.Context, r *gitx.Runner, gitDir string, v Version) string {
	t.Helper()
	root, err := writeTrees(ctx, r, gitDir, []Version{v})
	if err != nil {
		t.Fatalf("writing the tree: %v", err)
	}
	id, err := r.IsolatedAt(ctx, gitDir, v.When, "commit-tree", root[0], "-m", v.Import.Message())
	if err != nil {
		t.Fatalf("commit-tree: %v", err)
	}
	return id
}

// built is a skill directory written into a repository: its tree and the
// entries below it, as a source listing would have read them.
type built struct {
	tree    string
	entries []source.TreeEntry
}

// skillTree writes files as one skill directory and returns its tree, with
// link, when it is not empty, as a symlink an import has to leave out.
func skillTree(t *testing.T, ctx context.Context, r *gitx.Runner, gitDir string, files map[string]string, link string) built {
	t.Helper()
	var lines []string
	for _, path := range sorted(files) {
		oid, err := r.IsolatedInput(ctx, gitDir, strings.NewReader(files[path]), "hash-object", "-t", "blob", "-w", "--stdin")
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, source.FileMode+" blob "+strings.TrimSpace(oid)+"\t"+path)
	}
	if link != "" {
		oid, err := r.IsolatedInput(ctx, gitDir, strings.NewReader("elsewhere"), "hash-object", "-t", "blob", "-w", "--stdin")
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, "120000 blob "+strings.TrimSpace(oid)+"\t"+link)
	}
	// mktree --batch reads one definition; index-tree is what builds the
	// nested directories, so the lines go through update-index instead.
	tree := writeIndexTree(t, ctx, r, gitDir, lines)
	entries, err := source.ReadTree(ctx, r, gitDir, tree)
	if err != nil {
		t.Fatal(err)
	}
	return built{tree: tree, entries: entries}
}

// writeIndexTree builds a tree from ls-tree lines, nested paths included,
// through the repository's index, which is emptied first so that one test
// writing several skills does not pile them into one tree.
func writeIndexTree(t *testing.T, ctx context.Context, r *gitx.Runner, gitDir string, lines []string) string {
	t.Helper()
	if _, err := r.Isolated(ctx, gitDir, "read-tree", "--empty"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.IsolatedInput(ctx, gitDir, strings.NewReader(strings.Join(lines, "\n")+"\n"), "update-index", "--index-info"); err != nil {
		t.Fatal(err)
	}
	tree, err := r.Isolated(ctx, gitDir, "write-tree")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(tree)
}

func sorted(files map[string]string) []string {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func newRepo(t *testing.T) (*gitx.Runner, string) {
	t.Helper()
	gitDir := filepath.Join(t.TempDir(), "account.git")
	env := map[string]string{"PATH": os.Getenv("PATH"), "HOME": t.TempDir()}
	r := gitx.New(env, false, func(string, ...any) {})
	if _, err := r.Isolated(context.Background(), gitDir, "init", "--bare", "--quiet", gitDir); err != nil {
		t.Fatal(err)
	}
	return r, gitDir
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed; the import tests need it")
	}
}

// TestWriteTreesWritesCanonicalModes imports a version whose source stores
// its trees in forms git reads but no longer writes: a file of mode 100664,
// which ls-tree reads back as 100644, and a directory whose mode is padded
// with a zero. Reused whole, such a tree would keep an id no directory on
// disk could ever be compared equal to; the import tree is instead the one
// the same files get from a git of today, which is what a library directory
// holding them is compared with.
func TestWriteTreesWritesCanonicalModes(t *testing.T) {
	t.Parallel()
	requireGit(t)
	ctx := context.Background()
	r, gitDir := newRepo(t)
	blob := func(content string) string {
		oid, err := r.IsolatedInput(ctx, gitDir, strings.NewReader(content), "hash-object", "-t", "blob", "-w", "--stdin")
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(oid)
	}
	skill, notes, deep := blob("---\nname: alpha\n---\n"), blob("notes\n"), blob("deep\n")
	// The canonical form: what update-index and write-tree make of the files.
	canonical := skillTree(t, ctx, r, gitDir, map[string]string{
		"SKILL.md": "---\nname: alpha\n---\n", "notes.md": "notes\n", "sub/deep.md": "deep\n",
	}, "")

	mktree := func(lines ...string) string {
		out, err := r.IsolatedInput(ctx, gitDir, strings.NewReader(strings.Join(lines, "\n")+"\n"), "mktree")
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(out)
	}
	sub := mktree("100664 blob " + deep + "\tdeep.md")
	// mktree writes a directory's mode as 40000 whatever it is given, so the
	// padded one is written byte for byte.
	raw, err := hex.DecodeString(sub)
	if err != nil {
		t.Fatal(err)
	}
	body := "100644 SKILL.md\x00" + string(mustHex(t, skill)) + "100664 notes.md\x00" + string(mustHex(t, notes)) + "040000 sub\x00" + string(raw)
	out, err := r.IsolatedInput(ctx, gitDir, strings.NewReader(body), "hash-object", "-t", "tree", "--literally", "-w", "--stdin")
	if err != nil {
		t.Fatal(err)
	}
	legacy := strings.TrimSpace(out)
	if legacy == canonical.tree {
		t.Fatal("the legacy tree has the canonical id; the fixture proves nothing")
	}
	entries, err := source.ReadTree(ctx, r, gitDir, legacy)
	if err != nil {
		t.Fatal(err)
	}
	// ls-tree lists both with the same modes: only the ids of the trees
	// holding them differ.
	listed := func(entries []source.TreeEntry) []string {
		var lines []string
		for _, e := range entries {
			line := e.Mode + " " + e.Path
			if e.Mode != source.DirMode {
				line += " " + e.OID
			}
			lines = append(lines, line)
		}
		return lines
	}
	if got, want := listed(entries), listed(canonical.entries); !reflect.DeepEqual(got, want) {
		t.Fatalf("ls-tree reads the legacy tree as\n%v\nnot as the canonical\n%v", got, want)
	}

	v := Version{
		Import: Import{Source: "https://github.com/example/skills", Path: "skills/alpha", Commit: commitID, Hash: hashID},
		Dir:    "alpha", Tree: legacy, Entries: entries, When: "1700000000 +0000",
	}
	roots, err := writeTrees(ctx, r, gitDir, []Version{v})
	if err != nil {
		t.Fatalf("writing the tree: %v", err)
	}
	if want := treeid.Wrap("alpha", canonical.tree); roots[0] != want {
		t.Errorf("the import tree is %s, want %s: the skill under its canonical tree %s", roots[0], want, canonical.tree)
	}
}

func mustHex(t *testing.T, id string) []byte {
	t.Helper()
	raw, err := hex.DecodeString(id)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
