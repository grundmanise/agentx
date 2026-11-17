package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
)

// foundToReadLibrary are the clients agentx placed symlinks for before it
// knew they read the library too. Each such link is a second entry for a
// skill the client already sees in the library.
var foundToReadLibrary = []string{"cursor", "github-copilot", "opencode"}

// sweepLibraryLinks removes the symlinks agentx placed in the own skills
// directory of a client of foundToReadLibrary that reads the library: a link
// at <dir>/<name> naming the library entry <library>/<name>, the placement
// agentx makes, see sameTarget. Nothing else is touched: a directory, a
// copy, and a link of the user's to anywhere else stay. A directory that a
// client that does not read the library reads too is left alone, since a
// link there is that client's placement, and so is a skills directory that
// is the library itself.
//
// It runs under the exclusive lock, from the prune of every mutating command
// (see refs), in a journal of its own: the first command that changes
// anything after an upgrade removes what earlier versions placed, and every
// later one finds nothing to remove. A failure is a warning, since it is no
// reason to refuse the command that ran it.
func (inv *invocation) sweepLibraryLinks(u home.RefUpdater) {
	library := canonicalPath(inv.dirs.Library)
	clients := scan.Detect(inv.dirs)
	m := home.NewMutation(inv.dirs.Home)
	var removed []string
	for _, c := range clients {
		dir := scan.PlacementDir(c, inv.dirs)
		if !slices.Contains(foundToReadLibrary, c.Slug()) || !scan.ReadsLibrary(c, inv.dirs) || dir == "" ||
			canonicalPath(dir) == library || readByOther(clients, c, dir, inv) {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			if !os.IsNotExist(err) {
				inv.out.warn("cannot read " + dir + ": " + err.Error())
			}
			continue
		}
		for _, e := range entries {
			path := filepath.Join(dir, e.Name())
			if e.Type()&os.ModeSymlink == 0 || !sameTarget(path, filepath.Join(inv.dirs.Library, e.Name())) {
				continue
			}
			state, err := home.State(path)
			if err != nil || !home.IsLink(state) {
				continue
			}
			m.Remove(path, state)
			removed = append(removed, path)
		}
	}
	if err := m.Apply(u); err != nil {
		inv.out.warn("cannot remove the extra links agentx placed for clients that read the library: " + err.Error())
		return
	}
	if len(removed) > 0 {
		inv.out.warn("removed " + plural(len(removed), "link") + " agentx placed for clients that read the library, which listed those skills twice: " + strings.Join(removed, ", "))
	}
}

// readByOther reports whether a detected client other than c that does not
// read the library reads dir, by any spelling of it.
func readByOther(clients []scan.Client, c scan.Client, dir string, inv *invocation) bool {
	real := canonicalPath(dir)
	for _, o := range clients {
		if o.Slug() == c.Slug() || scan.ReadsLibrary(o, inv.dirs) {
			continue
		}
		if slices.ContainsFunc(o.SkillsDirs(inv.dirs), func(d string) bool { return canonicalPath(d) == real }) {
			return true
		}
	}
	return false
}
