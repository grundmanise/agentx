package cli

import (
	"errors"
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
// words agentx writes, the default of each written as no key at all; and
// push_url is the URL a push goes to, so it is held to the rules that keep
// a push where the user meant it: a URL git pushes over, no credential,
// the repository url names and not another, and a transport, user or host
// of its own, since a push URL that only respells url says nothing url
// does not. The messages name the entry by its url, which is canonical by
// the time they are reached, and never repeat the push URL, which may be a
// credential this refuses. Pure.
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
	return pushURLRefusal(src)
}

// pushURLRefusal is why the push_url of src is not one agentx would
// record, "" when it is one or when there is none.
func pushURLRefusal(src home.Source) string {
	if src.PushURL == "" {
		return ""
	}
	of := "the push_url of " + src.URL
	push, err := source.ParseAddress(src.PushURL)
	switch {
	case errors.Is(err, source.ErrCredential):
		return of + " carries a password or a token, and agentx never stores credentials"
	case err != nil:
		return of + " is not a URL git can push to: " + strings.TrimPrefix(err.Error(), source.ErrForm.Error()+": ")
	case push.Scheme == "git":
		return of + " is a git:// URL, over which nothing can be pushed"
	}
	canonical, err := source.ParseAddress(src.URL)
	switch {
	case err != nil:
		return src.URL + " is not a URL git can push to"
	case source.Compare(canonical, push, nil) == source.OtherRepository:
		return of + " names another repository"
	case source.SameTransport(canonical, push):
		return of + " only respells it: agentx records a push URL whose transport, user or host differs"
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
// the first. resolve reads the host an SSH alias names, as source.Compare
// takes it; nil reads every host as itself. Pure.
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

// sameRepository reports whether any URL of a, its url or its push URL,
// names the repository any URL of b names.
func sameRepository(a, b home.Source, resolve func(host string) string) bool {
	for _, x := range addresses(a) {
		for _, y := range addresses(b) {
			if source.Compare(x, y, resolve) == source.SameRepository {
				return true
			}
		}
	}
	return false
}

// addresses are the addresses of the URLs src reaches its repository by.
func addresses(src home.Source) []source.Address {
	var out []source.Address
	for _, raw := range []string{src.URL, src.PushURL} {
		if a, err := source.ParseAddress(raw); raw != "" && err == nil {
			out = append(out, a)
		}
	}
	return out
}
