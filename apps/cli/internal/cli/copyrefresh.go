package cli

import (
	"fmt"
	"os"
	"slices"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

// refreshCopies plans, into a mutation that replaces a skill's library
// directory with the version whose tree is target, what happens to the
// copies copy_mode records for the skill. Nothing records what a copy held
// when it was placed, so each one is judged by what it holds now, as git
// would record it:
//
//   - A copy that already holds target is left alone and says nothing.
//   - A copy that holds what agentx could have placed there, one of the
//     trees in placed (the library directory the mutation replaces, or a
//     version the skill was at), is refreshed: removed, its content
//     retained beside it until the mutation is verified, and replaced by a
//     copy of staged, the new library directory laid out for publishing.
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
// way through, and a second warning would count one copy twice. So is a
// path two configurations spell differently, one of their skills
// directories a symlink to the other's: paths are told apart by
// canonicalPath. Each configuration whose copy is refreshed is still
// named, as a placement names each configuration it placed into.
//
// Every copy removed carries the fingerprint it held when it was judged,
// so a copy edited between this plan and the step that removes it stops
// the mutation rather than being discarded.
func (inv *invocation) refreshCopies(m *home.Mutation, name, target string, placed []string, staged string, recorded []string, done *placements) {
	refreshed := map[string]bool{} // by canonicalPath, every path judged, true when its copy is refreshed
	for _, t := range inv.detectedTargets() {
		if t.readsLibrary || !slices.Contains(recorded, t.id) {
			continue
		}
		place := t.ownPlace(inv.dirs.Library, name)
		key := canonicalPath(place)
		if fresh, judged := refreshed[key]; judged {
			if fresh {
				done.copies = append(done.copies, t.id)
			}
			continue
		}
		refreshed[key] = inv.refreshCopy(m, t, place, name, target, placed, staged, done)
	}
}

// refreshCopy judges and plans the one copy at place, as refreshCopies
// says, and reports whether it is refreshed.
func (inv *invocation) refreshCopy(m *home.Mutation, t placeTarget, place, name, target string, placed []string, staged string, done *placements) bool {
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
	clean := len(tree.Unrecordable) == 0
	switch {
	case clean && tree.ID == target:
		return false
	case clean && slices.Contains(placed, tree.ID):
		fresh, fingerprint, err := stageRefresh(m, place, staged, target)
		if err != nil {
			inv.skipRefresh(done, place, err)
			return false
		}
		m.Remove(place, state)
		m.Publish(place, fresh, fingerprint)
		done.copies = append(done.copies, t.id)
		return true
	default:
		inv.keepCopy(done, t, place, name)
		return false
	}
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

// stageRefresh lays the new content of the directory at place out beside
// it, copied from the directory at from, and reads it back as git would
// record it: content whose tree is not target never gets published. The
// new content of a copy is copied from the library directory staged for
// the same mutation; the new library directory of a repair that keeps a
// displaced directory's content is copied from that directory.
func stageRefresh(m *home.Mutation, place, from, target string) (string, string, error) {
	fresh := m.Sibling(place, "staged")
	err := os.MkdirAll(fresh, 0o755)
	if err == nil {
		err = copyTreeTo(from, fresh)
	}
	if err == nil {
		err = home.SyncTree(fresh)
	}
	var tree treeid.Tree
	if err == nil {
		tree, err = treeid.Read(fresh)
	}
	if err == nil && tree.ID != target {
		err = fmt.Errorf("the staged copy at %s holds tree %s, not %s", fresh, tree.ID, target)
	}
	var fingerprint string
	if err == nil {
		fingerprint, err = home.Fingerprint(fresh)
	}
	if err != nil {
		os.RemoveAll(fresh)
		return "", "", err
	}
	return fresh, fingerprint, nil
}
