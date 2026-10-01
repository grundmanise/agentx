package lineage

import (
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"strings"
	"testing"
)

const (
	aForkID  = "01234567-89ab-4def-8123-456789abcdef"
	aMachine = "0123456789abcdef0123456789abcdef"
)

func TestParseForkAndForkMessage(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		message string
		want    ForkTrailers
		err     bool
	}{
		{"all three", "s\n\nAgentx-Base: " + commitID + "\nAgentx-Fork-ID: " + aForkID + "\nAgentx-Machine: " + aMachine + "\n",
			ForkTrailers{Base: commitID, ForkID: aForkID, Machine: aMachine}, false},
		{"one paragraph has no trailers", "Agentx-Machine: " + aMachine + "\n", ForkTrailers{}, false},
		{"quoted in the body, not the last paragraph", "s\n\nAgentx-Fork-ID: " + aForkID + "\n\nSigned-off-by: me\n", ForkTrailers{}, false},
		{"an import's trailers and unknown ones are ignored", message("https://github.com/example/skills", ".", commitID, hashID) + "Agentx-Later: x\n", ForkTrailers{}, false},
		{"given twice", "s\n\nAgentx-Machine: " + aMachine + "\nAgentx-Machine: " + aMachine + "\n", ForkTrailers{}, true},
		{"a fork id that is no UUID", "s\n\nAgentx-Fork-ID: 1234\n", ForkTrailers{}, true},
		{"an uppercase fork id", "s\n\nAgentx-Fork-ID: " + strings.ToUpper(aForkID) + "\n", ForkTrailers{}, true},
		{"a machine of the wrong length", "s\n\nAgentx-Machine: abc\n", ForkTrailers{}, true},
		{"a base that is no object id", "s\n\nAgentx-Base: main\n", ForkTrailers{}, true},
	} {
		got, err := ParseFork(tc.message)
		if (err != nil) != tc.err || got != tc.want {
			t.Errorf("%s: ParseFork = %+v, %v; want %+v, error %v", tc.name, got, err, tc.want, tc.err)
		}
		if err != nil && !errors.Is(err, ErrForkTrailer) {
			t.Errorf("%s: the error %v is not ErrForkTrailer", tc.name, err)
		}
	}

	for _, tc := range []struct {
		name, subject, body string
		t                   ForkTrailers
		err                 bool
	}{
		{"creation", "Create pdf", "", ForkTrailers{ForkID: aForkID, Machine: aMachine}, false},
		{"merge with a body", "pdf: merge", "two lines\nof body", ForkTrailers{Base: commitID, Machine: aMachine}, false},
		{"a body ending in a trailer of its own", "s", "Agentx-Machine: " + aMachine, ForkTrailers{}, true},
		{"a value git would not read back", "s", "", ForkTrailers{Machine: "not hex"}, true},
	} {
		m, err := ForkMessage(tc.subject, tc.body, tc.t)
		if (err != nil) != tc.err {
			t.Errorf("%s: ForkMessage = %q, %v; want error %v", tc.name, m, err, tc.err)
			continue
		}
		if err != nil {
			continue
		}
		if got, err := ParseFork(m); err != nil || got != tc.t || !strings.HasPrefix(m, tc.subject+"\n") {
			t.Errorf("%s: %q reads back as %+v, %v", tc.name, m, got, err)
		}
	}
	if m, _ := ForkMessage("Create pdf", "", ForkTrailers{ForkID: aForkID, Machine: aMachine}); m != "Create pdf\n\nAgentx-Fork-ID: "+aForkID+"\nAgentx-Machine: "+aMachine+"\n" {
		t.Errorf("a creation message reads %q", m)
	}
}

func TestNewForkIDIsAVersionFourUUID(t *testing.T) {
	t.Parallel()
	a, b := NewForkID(), NewForkID()
	if !forkID.MatchString(a) || a[14] != '4' || !strings.ContainsRune("89ab", rune(a[19])) {
		t.Errorf("NewForkID = %q, not a lowercase version 4 UUID", a)
	}
	if a == b {
		t.Errorf("two fork ids are both %s", a)
	}
}

// TestResolveWalksFirstParents is the read rule over histories built by
// hand, one shape per case.
// oid is an object id made up for a commit named in a test.
func oid(name string) string { return fmt.Sprintf("%x", sha1.Sum([]byte(name))) }

func TestResolveWalksFirstParents(t *testing.T) {
	t.Parallel()
	imp := Import{Source: "https://github.com/example/skills", Path: "pdf", Commit: commitID, Hash: hashID}
	other := imp
	other.Commit = strings.Repeat("9", 40)
	importMsg, otherMsg := imp.Message(), other.Message()
	idA, idB := aForkID, "fedcba98-7654-4321-8fed-cba987654321"
	own := func(trailers ...string) string {
		return "own commit\n\n" + strings.Join(trailers, "\n") + "\n"
	}
	commit := func(id, message string, parents ...string) Commit {
		for i, p := range parents {
			parents[i] = oid(p)
		}
		return Commit{ID: oid(id), Tree: "tree-" + id, Parents: parents, Message: message}
	}
	history := func(cs ...Commit) map[string]Commit {
		m := map[string]Commit{}
		for _, c := range cs {
			m[c.ID] = c
		}
		return m
	}
	for _, tc := range []struct {
		name    string
		tip     string
		commits map[string]Commit
		want    ForkLineage
		problem string
	}{
		{"the tip is an import", oid("i1"), history(commit("i1", importMsg)),
			ForkLineage{Base: oid("i1"), BaseTree: "tree-i1", Import: imp}, ""},
		{"a creation over an import", oid("c"), history(commit("i1", importMsg), commit("c", own("Agentx-Fork-ID: "+idA), "i1")),
			ForkLineage{ID: idA, Base: oid("i1"), BaseTree: "tree-i1", Import: imp}, ""},
		{"a merge naming its base", oid("m"), history(
			commit("i1", importMsg), commit("i2", otherMsg),
			commit("c", own("Agentx-Fork-ID: "+idA), "i1"),
			commit("m", own("Agentx-Base: "+oid("i2")), "c", "i2")),
			ForkLineage{ID: idA, Base: oid("i2"), BaseTree: "tree-i2", Import: other}, ""},
		{"a fork of a fork keeps the nearer id", oid("c2"), history(
			commit("i1", importMsg), commit("c", own("Agentx-Fork-ID: "+idB), "i1"),
			commit("e", own("Agentx-Machine: "+aMachine), "c"),
			commit("c2", own("Agentx-Fork-ID: "+idA), "e")),
			ForkLineage{ID: idA, Base: oid("i1"), BaseTree: "tree-i1", Import: imp}, ""},
		{"greenfield", oid("e"), history(commit("g", own("Agentx-Fork-ID: "+idA)), commit("e", "an edit\n", "g")),
			ForkLineage{ID: idA, Greenfield: true}, ""},
		{"a base that is no import", oid("m"), history(
			commit("g", own("Agentx-Fork-ID: "+idA)), commit("x", "a plain commit\n"),
			commit("m", own("Agentx-Base: "+oid("x")), "g", "x")),
			ForkLineage{ID: idA}, "which is not an import commit"},
		{"a base outside the history", oid("m"), history(
			commit("i1", importMsg), commit("g", own("Agentx-Fork-ID: "+idA)),
			commit("m", own("Agentx-Base: "+oid("i1")), "g")),
			ForkLineage{ID: idA}, "which is not an import commit in its history"},
		{"an import message on a commit with a parent is no base", oid("q"), history(
			commit("g", own("Agentx-Fork-ID: "+idA)), commit("q", importMsg, "g")),
			ForkLineage{ID: idA, Greenfield: true}, ""},
		{"no fork id anywhere", oid("e"), history(commit("i1", importMsg), commit("e", "an edit\n", "i1")),
			ForkLineage{Base: oid("i1"), BaseTree: "tree-i1", Import: imp}, ""},
		{"a commit missing from the history", oid("e"), history(commit("e", "an edit\n", "gone")),
			ForkLineage{}, "does not hold the commit " + oid("gone")},
	} {
		got := Resolve(tc.tip, tc.commits)
		problem := got.Problem
		got.Problem = ""
		if got != tc.want || (tc.problem == "") != (problem == "") || !strings.Contains(problem, tc.problem) {
			t.Errorf("%s: Resolve = %+v, problem %q; want %+v, problem %q", tc.name, got, problem, tc.want, tc.problem)
		}
	}
}

// TestReadForksWalksOnceForEveryFork reads two forks sharing a history and
// one faked as its own import commit, with the lineage read back from what
// git wrote.
func TestReadForksWalksOnceForEveryFork(t *testing.T) {
	t.Parallel()
	requireGit(t)
	r, gitDir := newRepo(t)
	ctx := context.Background()
	commitOf := func(message string, parents ...string) string {
		args := []string{"commit-tree", "4b825dc642cb6eb9a060e54bf8d69288fbee4904"}
		for _, p := range parents {
			args = append(args, "-p", p)
		}
		out, err := r.IsolatedInput(ctx, gitDir, strings.NewReader(message), args...)
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(out)
	}
	imp := Import{Source: "https://github.com/example/skills", Path: "pdf", Commit: commitID, Hash: hashID}
	base := commitOf(imp.Message())
	first := commitOf("Fork pdf\n\nAgentx-Fork-ID: "+aForkID+"\n", base)
	second := commitOf("Fork pdf2\n\nAgentx-Fork-ID: fedcba98-7654-4321-8fed-cba987654321\n", first)
	recs := map[string]Record{
		"pdf":    {Name: "pdf", Kind: KindFork, Commit: first},
		"pdf2":   {Name: "pdf2", Kind: KindFork, Commit: second},
		"faked":  {Name: "faked", Kind: KindFork, Commit: base, Tree: "t", Import: imp, HasImport: true},
		"manage": {Name: "manage", Kind: KindManaged, Commit: base, Import: imp, HasImport: true},
	}
	cache := map[string]ForkLineage{}
	if err := ReadForks(ctx, r, gitDir, recs, cache); err != nil {
		t.Fatal(err)
	}
	if l := recs["pdf"].Fork; l == nil || l.ID != aForkID || l.Base != base || l.Import != imp {
		t.Errorf("pdf reads %+v", l)
	}
	if l := recs["pdf2"].Fork; l == nil || l.ID == aForkID || l.Base != base {
		t.Errorf("pdf2 reads %+v", l)
	}
	if l := recs["faked"].Fork; l == nil || l.ID != "" || l.Base != base || l.BaseTree != "t" {
		t.Errorf("a fork whose tip is an import reads %+v", l)
	}
	if recs["manage"].Fork != nil {
		t.Error("a managed record was given a fork lineage")
	}
	if len(cache) != 2 {
		t.Errorf("the cache holds %d tips, want the two walked", len(cache))
	}
}
