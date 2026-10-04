package lineage

import "testing"

// TestAtCandidateIsAnUpdateOnlyWhenItCanBeRead holds AtCandidate to what an
// update is: a candidate whose lineage agentx can read, at a commit other
// than the one the import branch already holds. No candidate, a candidate
// whose trailers agentx cannot read and a candidate the branch already
// holds are no update; anything else is the same branch at the candidate's
// commit, tree and lineage, with nothing a check found left over.
func TestAtCandidateIsAnUpdateOnlyWhenItCanBeRead(t *testing.T) {
	t.Parallel()
	base := Import{Source: "https://github.com/example/skills", Path: "skills/pdf", Commit: commitID, Hash: hashID}
	next := Import{Source: base.Source, Path: base.Path, Commit: "fedcba9876543210fedcba9876543210fedcba98", Hash: hashID}
	rec := func(c *Candidate) Record {
		return Record{Name: "pdf", Kind: KindManaged, Ref: ManagedRef("pdf"), Commit: "c0ffee", Tree: "base-tree",
			Import: base, HasImport: true, Candidate: c, UpstreamRemoved: "marker"}
	}
	for _, c := range []struct {
		name      string
		candidate *Candidate
		update    bool
	}{
		{name: "no candidate"},
		{name: "a candidate whose lineage agentx cannot read", candidate: &Candidate{Commit: "cand", Tree: "next-tree"}},
		{name: "a candidate the branch already holds", candidate: &Candidate{Commit: "c0ffee", Tree: "base-tree", Import: base, HasImport: true}},
		{name: "a candidate", candidate: &Candidate{Commit: "cand", Tree: "next-tree", Import: next, HasImport: true}, update: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, ok := rec(c.candidate).AtCandidate()
			if ok != c.update {
				t.Fatalf("AtCandidate() is an update: %t, want %t", ok, c.update)
			}
			want := Record{}
			if c.update {
				want = Record{Name: "pdf", Kind: KindManaged, Ref: ManagedRef("pdf"), Commit: "cand", Tree: "next-tree", Import: next, HasImport: true}
			}
			if got != want {
				t.Errorf("AtCandidate() = %+v, want %+v", got, want)
			}
		})
	}
}

// TestForkCandidateIsJudgedAgainstTheBase holds ForkCandidate to what a
// fork's update is: a readable candidate other than the base version its
// history names, whatever its own tip holds. A fork whose lineage was not
// read, a greenfield one and one whose history does not say have no base,
// and so no update.
func TestForkCandidateIsJudgedAgainstTheBase(t *testing.T) {
	t.Parallel()
	base := Import{Source: "https://github.com/example/skills", Path: "skills/pdf", Commit: commitID, Hash: hashID}
	next := Import{Source: base.Source, Path: base.Path, Commit: "fedcba9876543210fedcba9876543210fedcba98", Hash: hashID}
	update := &Candidate{Commit: "cand", Tree: "next-tree", Import: next, HasImport: true}
	fork := func(l *ForkLineage, c *Candidate) Record {
		return Record{Name: "pdf", Kind: KindFork, Ref: ForkRef("pdf"), Commit: "tip", Tree: "tip-tree", Candidate: c, Fork: l}
	}
	read := &ForkLineage{ID: "id", Base: "import", BaseTree: "base-tree", Import: base}
	for _, c := range []struct {
		name   string
		rec    Record
		update bool
	}{
		{name: "a fork whose lineage was not read", rec: fork(nil, update)},
		{name: "a greenfield skill", rec: fork(&ForkLineage{ID: "id", Greenfield: true}, update)},
		{name: "a history that does not say", rec: fork(&ForkLineage{Problem: "missing"}, update)},
		{name: "a managed skill", rec: Record{Name: "pdf", Kind: KindManaged, Commit: "import", Import: base, HasImport: true, Candidate: update}},
		{name: "no candidate", rec: fork(read, nil)},
		{name: "the base itself", rec: fork(read, &Candidate{Commit: "import", Tree: "base-tree", Import: base, HasImport: true})},
		{name: "a newer version", rec: fork(read, update), update: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, ok := c.rec.ForkCandidate()
			if ok != c.update {
				t.Fatalf("ForkCandidate() is an update: %t, want %t", ok, c.update)
			}
			want := Record{}
			if c.update {
				want = Record{Name: "pdf", Kind: KindFork, Ref: ForkRef("pdf"), Commit: "cand", Tree: "next-tree", Import: next, HasImport: true}
			}
			if got != want {
				t.Errorf("ForkCandidate() = %+v, want %+v", got, want)
			}
			if b, ok := c.rec.ForkBase(); ok && (b.Commit != "import" || b.Tree != "base-tree" || b.Import != base) {
				t.Errorf("ForkBase() = %+v", b)
			}
		})
	}
}
