package lineage

import (
	"context"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
)

// The two namespaces of the account repo a skill's lineage lives in: one
// branch per managed skill, pointing at its import commit, and one per fork,
// pointing at the fork's own history. A skill in the library with a branch
// in neither is unmanaged.
const (
	ManagedPrefix = "refs/heads/managed/"
	ForkPrefix    = "refs/heads/skills/"
)

// CandidatePrefix is where an update candidate of a skill waits until it is
// accepted or dropped: the import commit of the newer upstream version the
// update check found, which the check writes and moves, an update deletes
// once the import branch holds it, and removal deletes with the skill, so
// that nothing of the skill is left under refs/agentx.
const CandidatePrefix = "refs/agentx/candidate/"

// CandidateRef is the update candidate of the skill called name.
func CandidateRef(name string) string { return CandidatePrefix + name }

// UpstreamRemovedPrefix is where the update check records that a managed
// skill's source no longer holds it: one ref per skill, pointing at the
// source commit the check fetched and found without the skill's directory,
// or without its SKILL.md. It is read with the lineage, in the same
// for-each-ref, so that a listing says so without reading the source refs,
// which may be deleted between two invocations; the check deletes it once
// the skill is back, and a removal of the skill deletes it with the branch.
// Only the check writes one.
const UpstreamRemovedPrefix = "refs/agentx/upstream-removed/"

// UpstreamRemovedRef is the upstream-removed marker of the skill called name.
func UpstreamRemovedRef(name string) string { return UpstreamRemovedPrefix + name }

// The kinds of lineage record a skill has. KindFork is one of the user's
// own skills, on a skills/ branch, which events list as managed (see
// eventKind in package cli).
const (
	KindManaged   = "managed"
	KindFork      = "fork"
	KindUnmanaged = "unmanaged"
)

// ManagedRef is the import branch of the managed skill called name.
func ManagedRef(name string) string { return ManagedPrefix + name }

// ForkRef is the branch of the fork called name.
func ForkRef(name string) string { return ForkPrefix + name }

// Record is what the account repo holds for one skill name: the branch, the
// commit it points at, that commit's tree and, when that commit carries
// them, the four lineage trailers. A managed skill's tip is its import
// commit. A fork's tip is a commit of its own history, whose lineage
// ReadForks reads into Fork; Import and HasImport then describe the tip
// alone and say nothing of the fork's base. What the last update check
// found for the skill comes with it: the candidate it pinned, and the
// source commit it found without the skill.
type Record struct {
	Name            string
	Kind            string // KindManaged or KindFork
	Ref             string
	Commit          string
	Tree            string // the root tree of that commit: for an import commit, the upstream directory as its one entry
	Import          Import
	HasImport       bool
	Parentless      bool         // the commit has no parent, as an import commit never has
	Candidate       *Candidate   // the update candidate, nil when the account repo holds none
	UpstreamRemoved string       // the source commit the upstream-removed marker names, "" when there is none
	Fork            *ForkLineage // a fork's lineage, read by ReadForks; nil for a managed skill and until then
}

// Candidate is an update candidate: the import commit of a newer upstream
// version, the tree it holds and, when its message is an import commit's,
// the lineage its trailers carry.
type Candidate struct {
	Commit    string
	Tree      string // the root tree, the upstream directory as its one entry, as a branch's is
	Import    Import
	HasImport bool
}

// AtCandidate is the record of the skill as an update leaves it: the same
// import branch at the candidate's import commit, with the tree and the
// lineage that commit carries, and nothing a check found left over. It is
// what the version an update lays out is read from, as ReadBase reads a
// branch's, and whether the skill has an update at all, for every command
// that shows or applies one.
//
// ok is false when there is no candidate whose lineage agentx can read, and
// when the candidate is the commit the import branch already holds. That is
// what an update leaves that moved the branch and stopped before it deleted
// the candidate, once its journal was moved aside to keep what the library
// directory held: the version is the skill's base already, so it is no
// update. The ref stays until the next check deletes it, reading
// CandidateCommit for the value it expects there.
func (rec Record) AtCandidate() (Record, bool) {
	c := rec.Candidate
	if c == nil || !c.HasImport || c.Commit == rec.Commit {
		return Record{}, false
	}
	return Record{Name: rec.Name, Kind: rec.Kind, Ref: rec.Ref, Commit: c.Commit, Tree: c.Tree, Import: c.Import, HasImport: true}, true
}

// ForkBase is the record of a fork's base version, the import commit its
// history names, as the import branch of a managed skill at that version
// would read: that commit, its tree and its trailers, with the fork's own
// name, kind and candidate. It is what a fork's update is judged against,
// by the same rules as a managed skill's, since a fork's update is a newer
// version of the upstream its base came from. ok is false for a fork
// whose lineage was not read, one with no base, as a skill made by skill
// new has none, and one whose history does not say which it is.
func (rec Record) ForkBase() (Record, bool) {
	l := rec.Fork
	if rec.Kind != KindFork || l == nil || l.Base == "" || l.Problem != "" {
		return Record{}, false
	}
	return Record{Name: rec.Name, Kind: rec.Kind, Ref: rec.Ref, Commit: l.Base, Tree: l.BaseTree, Import: l.Import, HasImport: true,
		Parentless: true, Candidate: rec.Candidate}, true
}

// ForkCandidate is the update of a fork, read as AtCandidate reads a
// managed skill's against the fork's base version: the candidate's import
// commit, when agentx can read its lineage and it is not the base the
// fork already holds. Whatever the fork's own commits hold, a candidate
// that is its base is no update.
func (rec Record) ForkCandidate() (Record, bool) {
	base, ok := rec.ForkBase()
	if !ok {
		return Record{}, false
	}
	return base.AtCandidate()
}

// CandidateCommit is the commit the skill's candidate ref holds, "" when it
// holds none.
func (rec Record) CandidateCommit() string {
	if rec.Candidate == nil {
		return ""
	}
	return rec.Candidate.Commit
}

// List reads every lineage branch of the account repo in one for-each-ref
// over both namespaces, trees and trailers and all, and returns them by
// skill name. The tree is what the fast path tells a managed skill's
// library directory from its base version by, with no git process of its
// own: see Record.HoldsID.
// A managed branch wins over a fork of the same name, which cannot happen
// while a rename into the fork namespace moves the branch rather than
// copying it.
//
// The same for-each-ref reads what the last update check left for each
// skill, its candidate with the trailers of the version it pins and its
// upstream-removed marker, so that a listing that shows them still costs
// one git process. A marker names a source's own commit, whose message is
// the source's and says nothing agentx reads, so its message is not
// printed at all. A candidate or a marker of a name no branch holds is no
// skill's and is left out.
//
// Nothing here reads the source refs: the lineage of a skill is what its own
// branch says, so deleting a source ref changes no lineage. Whether the
// source a managed skill names is still added is a question for the
// settings, which the caller reads; it is never recorded here.
func List(ctx context.Context, r *gitx.Runner, gitDir string) (map[string]Record, error) {
	const recordEnd = "\x01"
	markers := strings.TrimSuffix(UpstreamRemovedPrefix, "/")
	out, err := r.Isolated(ctx, gitDir,
		"for-each-ref", "--format=%(refname)%00%(objectname)%00%(tree)%00%(parent)%00"+
			"%(if:notequals="+markers+")%(refname:rstrip=1)%(then)%(contents)%(end)"+recordEnd,
		ManagedPrefix, ForkPrefix, CandidatePrefix, UpstreamRemovedPrefix)
	if err != nil {
		return nil, err
	}
	records := map[string]Record{}
	candidates := map[string]Candidate{}
	removed := map[string]string{}
	for _, entry := range strings.Split(out, recordEnd) {
		entry = strings.TrimPrefix(entry, "\n")
		if strings.TrimSpace(entry) == "" {
			continue
		}
		fields := strings.SplitN(entry, "\x00", 5)
		if len(fields) != 5 {
			continue
		}
		rec := Record{Ref: fields[0], Commit: fields[1], Tree: fields[2], Parentless: fields[3] == ""}
		switch {
		case strings.HasPrefix(rec.Ref, ManagedPrefix):
			rec.Name, rec.Kind = strings.TrimPrefix(rec.Ref, ManagedPrefix), KindManaged
		case strings.HasPrefix(rec.Ref, ForkPrefix):
			rec.Name, rec.Kind = strings.TrimPrefix(rec.Ref, ForkPrefix), KindFork
		case strings.HasPrefix(rec.Ref, CandidatePrefix):
			c := Candidate{Commit: rec.Commit, Tree: rec.Tree}
			if imported, err := Parse(fields[4]); err == nil {
				c.Import, c.HasImport = imported, true
			}
			candidates[strings.TrimPrefix(rec.Ref, CandidatePrefix)] = c
			continue
		case strings.HasPrefix(rec.Ref, UpstreamRemovedPrefix):
			removed[strings.TrimPrefix(rec.Ref, UpstreamRemovedPrefix)] = rec.Commit
			continue
		default:
			continue
		}
		if imported, err := Parse(fields[4]); err == nil {
			rec.Import, rec.HasImport = imported, true
		}
		if have, ok := records[rec.Name]; !ok || have.Kind == KindFork && rec.Kind == KindManaged {
			records[rec.Name] = rec
		}
	}
	for name, rec := range records {
		if c, ok := candidates[name]; ok {
			rec.Candidate = &c
		}
		rec.UpstreamRemoved = removed[name]
		records[name] = rec
	}
	return records, nil
}
