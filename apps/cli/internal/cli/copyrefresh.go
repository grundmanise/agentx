package cli

import (
	"context"
	"os"
	"slices"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

// refreshCopies plans, into a mutation that replaces a skill's library
// directory with the version target, what happens to the copies copy_mode
// records for the skill. Nothing records what a copy held when it was
// placed, so each one is judged by what it holds now, as drift judges a
// library directory: the fast path, then git with the copy as its work
// tree, so a file git ignores in a copy changes nothing.
//
//   - A copy that already holds target is left alone and says nothing.
//   - A copy that holds what agentx could have placed there, one of the
//     versions in placed (the library directory the mutation replaces, or
//     a version the skill was at), is refreshed: removed, its content
//     retained beside it until the mutation is verified, and replaced by
//     target, which lay lays out, with the files git ignores in the copy
//     carried over.
//   - A copy holding anything else was edited where it is and is kept byte
//     for byte, skipped and named with the warning a placement gives a copy
//     it keeps, see keepCopy.
//
// A path that holds no copy at all, nothing or a link, has no content of
// agentx's to refresh and is left as it is; the drift of the skill says
// what it is. A copy this machine cannot read or cannot stage a new one
// beside is skipped and left as it is, with a warning of its own, see
// skipRefresh. The refreshed configurations go into done.copies, the kept
// and skipped paths into done.skipped.
//
// A path two configurations share, as Zencoder and Zenflow share one
// skills directory, is judged and planned once: a second remove of it
// would find the first one's publish there, which is neither what it was
// to remove nor what it was to become, and would stop the mutation part
// way through, and a second warning would count one copy twice. Each
// configuration whose copy is refreshed is still named, as a placement
// names each configuration it placed into.
//
// Every copy removed carries the fingerprint it held when it was judged,
// so a copy edited between this plan and the step that removes it stops
// the mutation rather than being discarded.
func (inv *invocation) refreshCopies(ctx context.Context, m *home.Mutation, gitDir, name string, target version, placed []version, lay func(dest string) error, recorded []string, done *placements) {
	refreshed := map[string]bool{} // by path, every path judged, true when its copy is refreshed
	for _, t := range inv.detectedTargets() {
		if t.readsLibrary || !slices.Contains(recorded, t.id) {
			continue
		}
		place := t.ownPlace(inv.dirs.Library, name)
		if fresh, judged := refreshed[place]; judged {
			if fresh {
				done.copies = append(done.copies, t.id)
			}
			continue
		}
		refreshed[place] = inv.refreshCopy(ctx, m, gitDir, t, place, name, target, placed, lay, done)
	}
}

// refreshCopy judges and plans the one copy at place, as refreshCopies
// says, and reports whether it is refreshed.
func (inv *invocation) refreshCopy(ctx context.Context, m *home.Mutation, gitDir string, t placeTarget, place, name string, target version, placed []version, lay func(string) error, done *placements) bool {
	state, err := home.State(place)
	if err != nil {
		inv.skipRefresh(done, place, err)
		return false
	}
	if !home.IsDir(state) {
		return false
	}
	tree, err := treeid.Read(place)
	if err != nil {
		inv.skipRefresh(done, place, err)
		return false
	}
	j, err := inv.judgeDir(ctx, gitDir, place, tree, target, false)
	if err != nil {
		inv.skipRefresh(done, place, err)
		return false
	}
	if j.holds {
		return false
	}
	for _, v := range placed {
		if j, err = inv.judgeDir(ctx, gitDir, place, tree, v, true); err != nil {
			inv.skipRefresh(done, place, err)
			return false
		}
		if !j.holds {
			continue
		}
		fresh := m.Sibling(place, "staged")
		fingerprint, err := stageVersion(fresh, lay, target, place, j.ignored)
		if err != nil {
			os.RemoveAll(fresh)
			inv.skipRefresh(done, place, err)
			return false
		}
		m.Remove(place, state)
		m.Publish(place, fresh, fingerprint)
		done.copies = append(done.copies, t.id)
		return true
	}
	inv.keepCopy(done, t, place, name)
	return false
}

// skipRefresh leaves one copy as it is when this machine cannot read it or
// cannot stage its new content beside it, and says why, counting it where
// a skipped placement is counted: the run still succeeds and the result
// says how many were skipped. Unlike a placement that could not be made,
// the configuration keeps the copy it had.
func (inv *invocation) skipRefresh(done *placements, place string, err error) {
	done.skipped = append(done.skipped, place)
	inv.out.warn("cannot refresh " + place + ": " + err.Error() + "; the copy was left as it is")
}
