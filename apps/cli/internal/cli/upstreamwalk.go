package cli

import (
	"fmt"
	"strings"
)

// history is the source's history as one walk prints it: every commit
// reachable from the tip, with its committer time, its parents and, for each
// selected subpath whose entry it changes against its first parent (the
// empty tree for a root commit), what that entry is after it.
//
// It is what finding the upstream commit of several subpaths in one git
// process takes. git's own answer for one subpath, "git log -1 --no-renames
// <tip> -- :(literal)<subpath>", simplifies the history for that subpath
// alone: a merge whose entry for it matches one of its parents follows only
// the first such parent. A walk over several subpaths at once simplifies for
// all of them together and would follow other parents, so the walk keeps
// every commit and every parent instead, and lastChange replays git's walk
// for each subpath on it.
type history struct {
	commits map[string]*walkedCommit
}

type walkedCommit struct {
	epoch   string   // the committer time as the walk printed it
	parents []string // every parent, in order
	changed map[string]string
}

// historyRead is the walk: every commit and every parent (--full-history
// --sparse), each commit's changes against its first parent alone, a merge
// included, as raw entries with tree entries (-t) and full ids, so that the
// line whose path is a subpath gives that subpath's entry. Trees alone are
// compared: renames are not followed, which would read blobs a blobless
// clone does not hold, and every subpath is matched literally rather than as
// a pattern.
func historyRead(tip string, subpaths []string) []string {
	walk := []string{"log", "--full-history", "--sparse", "--root", "--no-renames", "--diff-merges=first-parent", "--raw", "-t", "--no-abbrev",
		"--format=%x00%H %ct %P", "-z", tip, "--"}
	for _, p := range subpaths {
		walk = append(walk, ":(literal)"+p)
	}
	return walk
}

// parseHistory reads what historyRead printed. Each commit is a NUL, its id,
// committer time and parents, a NUL, and then its raw entries, each a
// status field and a path ending in a NUL, the first after a newline. No
// field of an entry is empty, so an empty field is where the next commit
// starts. Only the entries whose path is one of the subpaths are kept.
func parseHistory(out string, subpaths []string) (history, error) {
	wanted := make(map[string]bool, len(subpaths))
	for _, p := range subpaths {
		wanted[p] = true
	}
	h := history{commits: map[string]*walkedCommit{}}
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); i++ {
		if fields[i] != "" || i+1 >= len(fields) {
			continue
		}
		i++
		header := strings.Fields(fields[i])
		if len(header) < 2 {
			return history{}, fmt.Errorf("unexpected history record %q", fields[i])
		}
		c := &walkedCommit{epoch: header[1], parents: header[2:], changed: map[string]string{}}
		h.commits[header[0]] = c
		for i+2 < len(fields) && fields[i+1] != "" {
			status, name := strings.TrimPrefix(fields[i+1], "\n"), fields[i+2]
			i += 2
			if !wanted[name] {
				continue
			}
			// ":<old mode> <new mode> <old id> <new id> <status>"
			parts := strings.Fields(strings.TrimPrefix(status, ":"))
			if len(parts) != 5 {
				return history{}, fmt.Errorf("unexpected raw entry %q of %s", status, short(header[0]))
			}
			entry := ""
			if strings.Trim(parts[3], "0") != "" {
				entry = parts[1] + " " + parts[3]
			}
			// A type change prints the path twice, once gone and once added:
			// what is there after the commit is the one that is not gone.
			if prev, ok := c.changed[name]; !ok || prev == "" {
				c.changed[name] = entry
			}
		}
	}
	return h, nil
}

// entry is what subpath is at commit id: the last change on the first-parent
// line, "" where it is absent. Every commit looked up is remembered in memo.
func (h history) entry(id, subpath string, memo map[string]string) string {
	var line []string
	e := ""
	for {
		if v, ok := memo[id]; ok {
			e = v
			break
		}
		c, ok := h.commits[id]
		if !ok {
			break // outside the walk: nothing there
		}
		line = append(line, id)
		if v, ok := c.changed[subpath]; ok {
			e = v
			break
		}
		if len(c.parents) == 0 {
			break
		}
		id = c.parents[0]
	}
	for _, id := range line {
		memo[id] = e
	}
	return e
}

// lastChange is what "git log -1 --no-renames <tip> -- :(literal)<subpath>"
// prints, replayed on the walk. git's default history simplification makes
// that walk a single line from the tip: a commit whose entry matches its
// parent's, or a merge whose entry matches one of its parents', is not a
// change and the walk goes on to that parent, the first such one for a
// merge; any other commit is the change, a merge that matches no parent
// and a root commit that holds the subpath included.
func (h history) lastChange(tip, subpath string) (string, bool) {
	memo := map[string]string{}
	for id := tip; ; {
		c, ok := h.commits[id]
		if !ok {
			return "", false
		}
		e := h.entry(id, subpath, memo)
		next := ""
		for _, p := range c.parents {
			if h.entry(p, subpath, memo) == e {
				next = p
				break
			}
		}
		switch {
		case next != "":
			id = next
		case len(c.parents) > 0 || e != "":
			return id, true
		default:
			return "", false // a root commit without the subpath
		}
	}
}
