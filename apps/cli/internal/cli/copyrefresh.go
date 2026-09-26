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
// beside is skipped with a warning, as a placement it cannot make is. The
// refreshed configurations go into done.copies, the kept and skipped paths
// into done.skipped.
//
// Every copy removed carries the fingerprint it held when it was judged,
// so a copy edited between this plan and the step that removes it stops
// the mutation rather than being discarded.
func (inv *invocation) refreshCopies(m *home.Mutation, name, target string, placed []string, staged string, recorded []string, done *placements) {
	for _, t := range inv.detectedTargets() {
		if t.readsLibrary || !slices.Contains(recorded, t.id) {
			continue
		}
		place := t.ownPlace(inv.dirs.Library, name)
		state, err := home.State(place)
		if err != nil {
			inv.skipPlacement(done, t, place, err)
			continue
		}
		if !home.IsDir(state) {
			continue
		}
		tree, err := treeid.Read(place)
		if err != nil {
			inv.skipPlacement(done, t, place, err)
			continue
		}
		clean := len(tree.Unrecordable) == 0
		switch {
		case clean && tree.ID == target:
		case clean && slices.Contains(placed, tree.ID):
			fresh, fingerprint, err := stageRefresh(m, place, staged, target)
			if err != nil {
				inv.skipPlacement(done, t, place, err)
				continue
			}
			m.Remove(place, state)
			m.Publish(place, fresh, fingerprint)
			done.copies = append(done.copies, t.id)
		default:
			inv.keepCopy(done, t, place, name)
		}
	}
}

// stageRefresh lays the new content of one copy out beside it, from the
// library directory staged for the same mutation, and reads it back as git
// would record it: a copy that is not the version being placed never gets
// published.
func stageRefresh(m *home.Mutation, place, staged, target string) (string, string, error) {
	fresh := m.Sibling(place, "staged")
	err := os.MkdirAll(fresh, 0o755)
	if err == nil {
		err = copyTreeTo(staged, fresh)
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
