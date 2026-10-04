package lineage

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
)

// Commit is one commit of a fork's history as the walk reads it.
type Commit struct {
	ID      string
	Tree    string
	Parents []string
	Message string
}

// ForkLineage is what a fork's history says of it, read along first
// parents from its tip: its permanent id, and the import commit that is its
// base version, if it has one. The walk is the lineage record: nothing
// else is kept anywhere.
type ForkLineage struct {
	ID      string // the Agentx-Fork-ID of the nearest commit carrying one; "" when none does
	Created string // that commit, the fork's creation commit, which skill new or skill fork wrote; "" when none does
	// Renamed are the names the fork's history records it was renamed
	// from, nearest first: the subjects skill rename writes, see
	// RenamedFrom, read along first parents from the tip down to the
	// creation commit. A rename keeps the fork id, so they sit above it.
	Renamed    []string
	Base       string // the base import commit; "" for a skill made by skill new, and when Problem says why it cannot be read
	BaseTree   string // the base's root tree, the upstream directory as its one entry
	Import     Import // the base's coordinates, when Base is not ""
	Greenfield bool   // the walk reached a root that is neither an import commit nor names one, as for a skill made by skill new
	Problem    string // why the base cannot be read; "" when it can
}

// Resolve reads a fork's lineage out of commits, the history of its tip,
// by the read rule, with no git. Along first parents from the tip, the
// first import commit is the base itself; the first commit with an
// Agentx-Base trailer names it, and the commit it names must be an import
// commit in the tip's history; reaching a root that is neither makes it a
// skill with no base, as skill new makes one. The fork id, and the renames
// above it, are read by a walk of its own, which goes on past the base: a
// fork's creation commit sits above the import it was forked from and
// carries the id, not the base. A fork of a fork carries two ids, and the
// nearer one is its own.
// Nothing is ever inferred: a history that does not say is reported, not
// guessed at.
func Resolve(tip string, commits map[string]Commit) ForkLineage {
	var l ForkLineage
	for id := tip; ; {
		c, ok := commits[id]
		if !ok {
			l.Problem = "its history does not hold the commit " + id
			break
		}
		if IsImport(c) {
			l.Base = id
			break
		}
		t, err := ParseFork(c.Message)
		if err != nil {
			l.Problem = fmt.Sprintf("the commit %s: %v", abbrev(id), err)
			break
		}
		if t.Base != "" {
			if b, ok := commits[t.Base]; !ok || !IsImport(b) || !ancestor(t.Base, tip, commits) {
				l.Problem = fmt.Sprintf("the commit %s names %s as its base, which is not an import commit in its history", abbrev(id), abbrev(t.Base))
			} else {
				l.Base = t.Base
			}
			break
		}
		if len(c.Parents) == 0 {
			l.Greenfield = true
			break
		}
		id = c.Parents[0]
	}
	if l.Base != "" {
		b := commits[l.Base]
		l.BaseTree = b.Tree
		l.Import, _ = Parse(b.Message)
	}
	for id := tip; ; {
		c, ok := commits[id]
		if !ok {
			break
		}
		if old := RenamedFrom(c.Message); old != "" {
			l.Renamed = append(l.Renamed, old)
		}
		if t, err := ParseFork(c.Message); err == nil && t.ForkID != "" {
			l.ID, l.Created = t.ForkID, id
			break
		}
		if len(c.Parents) == 0 {
			break
		}
		id = c.Parents[0]
	}
	return l
}

// ancestor reports whether a is tip or reachable from it in commits.
func ancestor(a, tip string, commits map[string]Commit) bool {
	seen := map[string]bool{}
	queue := []string{tip}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if id == a {
			return true
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		queue = append(queue, commits[id].Parents...)
	}
	return false
}

// Walk reads the lineage of every tip in one git process of the isolated
// environment: one git log over the whole history of all of them, every
// commit with its tree, its parents and its message, which Resolve then
// follows along first parents. The whole history is read rather than the
// first-parent chain because a base an Agentx-Base trailer names is a merge's
// second parent, whose trailers the first-parent chain does not hold. A
// fork's branch holds its own commits and the parentless import commits
// merged into it, so the history is small; tips that share it, as a fork of
// a fork does, read it once.
func Walk(ctx context.Context, r *gitx.Runner, gitDir string, tips []string) (map[string]ForkLineage, error) {
	if len(tips) == 0 {
		return map[string]ForkLineage{}, nil
	}
	const recordEnd = "\x01"
	out, err := r.IsolatedInput(ctx, gitDir, strings.NewReader(strings.Join(tips, "\n")+"\n"),
		"log", "--stdin", "--format=%H%x00%T%x00%P%x00%B"+recordEnd)
	if err != nil {
		return nil, err
	}
	commits := map[string]Commit{}
	for _, record := range strings.Split(out, recordEnd) {
		record = strings.TrimPrefix(record, "\n")
		fields := strings.SplitN(record, "\x00", 4)
		if len(fields) != 4 {
			continue
		}
		commits[fields[0]] = Commit{ID: fields[0], Tree: fields[1], Parents: strings.Fields(fields[2]), Message: fields[3]}
	}
	lineages := make(map[string]ForkLineage, len(tips))
	for _, tip := range tips {
		lineages[tip] = Resolve(tip, commits)
	}
	return lineages, nil
}

// ReadForks fills the Fork of every fork record of recs. A fork whose tip
// is itself an import commit, parentless with the trailers List read
// already, is its own base and costs no git; the others are read in one
// Walk, which also takes a tip that carries an import's trailers and has a
// parent, since such a commit is never an import. cache, when not nil,
// holds lineages by tip from an earlier read and is filled with this
// one's, so that a serve rescan walks only the tips that moved.
func ReadForks(ctx context.Context, r *gitx.Runner, gitDir string, recs map[string]Record, cache *WalkCache) error {
	var tips []string
	known := map[string]ForkLineage{}
	for _, rec := range recs {
		switch {
		case rec.Kind != KindFork:
		case rec.HasImport && rec.Parentless:
			known[rec.Commit] = ForkLineage{Base: rec.Commit, BaseTree: rec.Tree, Import: rec.Import}
		default:
			if l, ok := cache.get(rec.Commit); ok {
				known[rec.Commit] = l
			} else if _, listed := known[rec.Commit]; !listed {
				known[rec.Commit] = ForkLineage{}
				tips = append(tips, rec.Commit)
			}
		}
	}
	walked, err := Walk(ctx, r, gitDir, tips)
	if err != nil {
		return err
	}
	for tip, l := range walked {
		known[tip] = l
	}
	cache.put(walked)
	for name, rec := range recs {
		if rec.Kind == KindFork {
			l := known[rec.Commit]
			rec.Fork = &l
			recs[name] = rec
		}
	}
	return nil
}

// WalkCache keeps fork lineages by branch tip from one ReadForks to the
// next, for a process that reads them again and again, as serve does,
// whose scans and update checks read them on goroutines of their own: its
// methods may be called from several at once. A nil
// WalkCache keeps nothing.
type WalkCache struct {
	mu       sync.Mutex
	lineages map[string]ForkLineage
}

// NewWalkCache is an empty WalkCache.
func NewWalkCache() *WalkCache {
	return &WalkCache{lineages: map[string]ForkLineage{}}
}

// get is the lineage kept for tip.
func (c *WalkCache) get(tip string) (ForkLineage, bool) {
	if c == nil {
		return ForkLineage{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	l, ok := c.lineages[tip]
	return l, ok
}

// put keeps the lineages walked, by tip.
func (c *WalkCache) put(walked map[string]ForkLineage) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for tip, l := range walked {
		c.lineages[tip] = l
	}
}
