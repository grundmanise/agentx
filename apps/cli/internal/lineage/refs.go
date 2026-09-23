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
// accepted or dropped. A later spec writes these; removal deletes the one
// of the skill it takes away, so that nothing of the skill is left under
// refs/agentx.
const CandidatePrefix = "refs/agentx/candidate/"

// CandidateRef is the update candidate of the skill called name.
func CandidateRef(name string) string { return CandidatePrefix + name }

// The kinds a skill of the library is listed with.
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
// commit it points at and, when that commit carries them, the four lineage
// trailers. A fork's tip carries the trailers of the last upstream version
// merged into it, and may carry none at all.
type Record struct {
	Name      string
	Kind      string // managed or fork
	Ref       string
	Commit    string
	Import    Import
	HasImport bool
}

// List reads every lineage branch of the account repo in one for-each-ref
// over both namespaces, trailers and all, and returns them by skill name. A
// managed branch wins over a fork of the same name, which cannot happen
// while a rename into the fork namespace moves the branch rather than
// copying it.
//
// Nothing here reads the source refs: the lineage of a skill is what its own
// branch says, so deleting a source ref changes no lineage. Whether the
// source a managed skill names is still added is a question for the
// settings, which the caller reads; it is never recorded here.
func List(ctx context.Context, r *gitx.Runner, gitDir string) (map[string]Record, error) {
	const recordEnd = "\x01"
	out, err := r.Isolated(ctx, gitDir,
		"for-each-ref", "--format=%(refname)%00%(objectname)%00%(contents)"+recordEnd,
		ManagedPrefix, ForkPrefix)
	if err != nil {
		return nil, err
	}
	records := map[string]Record{}
	for _, entry := range strings.Split(out, recordEnd) {
		entry = strings.TrimPrefix(entry, "\n")
		if strings.TrimSpace(entry) == "" {
			continue
		}
		fields := strings.SplitN(entry, "\x00", 3)
		if len(fields) != 3 {
			continue
		}
		rec := Record{Ref: fields[0], Commit: fields[1]}
		switch {
		case strings.HasPrefix(rec.Ref, ManagedPrefix):
			rec.Name, rec.Kind = strings.TrimPrefix(rec.Ref, ManagedPrefix), KindManaged
		case strings.HasPrefix(rec.Ref, ForkPrefix):
			rec.Name, rec.Kind = strings.TrimPrefix(rec.Ref, ForkPrefix), KindFork
		default:
			continue
		}
		if imported, err := Parse(fields[2]); err == nil {
			rec.Import, rec.HasImport = imported, true
		}
		if have, ok := records[rec.Name]; !ok || have.Kind == KindFork && rec.Kind == KindManaged {
			records[rec.Name] = rec
		}
	}
	return records, nil
}
