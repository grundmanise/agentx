package cli

import (
	"slices"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// sourceRefusal is why src is not a source entry agentx would write, and
// "" when it is one. Every field is held to the rule of the command that
// writes it: url and alias are both URLs of a source and are stored
// canonical; pin and default_branch are refs this machine hands to git,
// which source.ValidRef exists to check before that happens; last_fetched
// and access_checked are dates agentx wrote; layout and access are the
// words agentx writes, the default of each written as no key at all. The
// messages name the entry by its url, which is canonical by the time they
// are reached. Pure.
func sourceRefusal(src home.Source) string {
	urls := []string{src.URL}
	if src.Alias != "" {
		urls = append(urls, src.Alias)
	}
	for _, url := range urls {
		if why := badSourceURL(url); why != "" {
			return why
		}
	}
	switch {
	case src.Layout != "" && src.Layout != home.LayoutFork:
		return src.URL + " has the layout " + clipped(src.Layout) + ", and agentx writes fork, or no layout for a tree source"
	case src.Account && src.Layout != home.LayoutFork:
		return src.URL + " is marked as the account remote, which only a source of the fork layout can be"
	case src.Layout == home.LayoutFork && !src.Account:
		return src.URL + " is a fork source other than the account remote, and this agentx keeps no other"
	case src.Pin != "" && !source.ValidRef(src.Pin):
		return src.URL + " is pinned to " + clipped(src.Pin) + ", which is not a ref git accepts"
	case src.Access != "" && src.Access != home.AccessWritable && src.Access != home.AccessReadOnly:
		return src.URL + " has the access " + clipped(src.Access) + ", and agentx writes writable or read-only, or none while it is unknown"
	case src.AccessChecked != "" && !fetchTime(src.AccessChecked):
		return src.URL + " had its access checked at " + clipped(src.AccessChecked) + ", which is not RFC 3339 in UTC"
	case src.DefaultBranch != "" && !source.ValidRef(src.DefaultBranch):
		return src.URL + " has the default branch " + clipped(src.DefaultBranch) + ", which is not a ref git accepts"
	case src.LastFetched != "" && !fetchTime(src.LastFetched):
		return src.URL + " was last fetched at " + clipped(src.LastFetched) + ", which is not RFC 3339 in UTC"
	}
	return ""
}

// sourcesRefusal is why sources is not a list agentx would write, "" when
// it is one: an entry sourceRefusal refuses, a source listed twice, a list
// out of order, more than one account remote, or a tree source and a fork
// source naming one repository, which would be fetched both ways and
// pushed to by both. SetSource keeps the list sorted by url and holds one
// entry per source, so two entries for one source are a list agentx did
// not write and a pin or an alias silently lost: FindSource only ever sees
// the first. resolve reads the host an SSH alias names, as
// source.SameRepository takes it; nil reads every host as itself. Pure.
func sourcesRefusal(sources []home.Source, resolve func(host string) string) string {
	seen := map[string]bool{}
	account := ""
	for _, src := range sources {
		if why := sourceRefusal(src); why != "" {
			return why
		}
		switch {
		case seen[src.URL]:
			return src.URL + " is listed twice"
		case src.Account && account != "":
			return "both " + account + " and " + src.URL + " are marked as the account remote"
		case src.Account:
			account = src.URL
		}
		seen[src.URL] = true
	}
	if !slices.IsSortedFunc(sources, func(a, b home.Source) int { return strings.Compare(a.URL, b.URL) }) {
		return "the sources must be sorted by url"
	}
	for _, fork := range sources {
		if fork.Layout != home.LayoutFork {
			continue
		}
		for _, tree := range sources {
			if tree.Layout != home.LayoutFork && sameRepository(fork, tree, resolve) {
				return tree.URL + " and the fork source " + fork.URL + " name one repository"
			}
		}
	}
	return ""
}

// sameRepository reports whether the URLs of a and b name one repository.
func sameRepository(a, b home.Source, resolve func(host string) string) bool {
	x, err := source.ParseAddress(a.URL)
	if err != nil {
		return false
	}
	y, err := source.ParseAddress(b.URL)
	if err != nil {
		return false
	}
	return source.SameRepository(x, y, resolve)
}

// forkSourceRefusal is why src, a tree source an add is about to write,
// cannot be one, nil when it can: a fork source of s already names its
// repository, through resolve.
func forkSourceRefusal(s home.Settings, src home.Source, resolve func(host string) string) *failure {
	for _, fork := range s.Sources {
		if fork.Layout == home.LayoutFork && (fork.URL == src.URL || sameRepository(src, fork, resolve)) {
			return forkSourceIs(src.URL, fork)
		}
	}
	return nil
}

// forkSourceIs refuses to read url, which names the repository of the fork
// source fork, as a tree source, exit 6. A fork source holds one branch per
// fork, and fetching it as a tree as well would bring its objects in
// without their blobs, which a fork's history then lacks. Its forks are
// listed with skill list --remote and installed with skill add
// --from-account, so the hint names both.
func forkSourceIs(url string, fork home.Source) *failure {
	what := url + " is the fork source " + fork.URL
	if fork.URL == url {
		what = url + " is a fork source"
	}
	return refuse(exitRefused, what+": it holds one branch per fork rather than skills on one branch",
		"run 'agentx skill list --remote' to see its forks, then 'agentx skill add --from-account <name>' to install one")
}

// forkToTree refuses source add --layout tree of the fork source fork, exit
// 6: its forks' history would lose the blobs a tree fetch leaves out, and
// the layout of a source is changed by removing it and adding it again.
func forkToTree(fork home.Source) *failure {
	return refuse(exitRefused, fork.URL+" is a fork source, and a source keeps its layout",
		"remove it first with 'agentx source remove "+shellWord(fork.URL)+"', then add it again")
}
