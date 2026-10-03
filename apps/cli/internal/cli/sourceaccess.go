package cli

import (
	"context"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// sourceCheck is what a look at a source just fetched found out besides its
// skills: whether this machine may push to it, when that was asked, and the
// branch the remote's HEAD names. ok is false for a look the run's stop cut
// short, which records nothing.
type sourceCheck struct {
	ok            bool
	access        gitx.Access
	checked       string // RFC 3339 UTC
	defaultBranch string // "" when it was not read or names no branch
}

// checkSource asks the source of entry what this machine may do there (see
// gitx.ProbeAccess) and, for an unpinned tree source, which branch its HEAD
// names, outside the lock, at its URL. The two run one after the other, so
// that a run that checks in its fetch workers keeps to their bound. Neither
// can fail the command: an answer that does not come is an unknown access
// and no default branch.
func (inv *invocation) checkSource(ctx context.Context, entry home.Source) sourceCheck {
	var c sourceCheck
	c.access = inv.git.ProbeAccess(ctx, entry.URL)
	if c.access.Access != home.AccessWritable {
		inv.out.debugf("access to %s: %s: %s", entry.URL, c.access.Access, c.access.Reason)
	}
	if entry.Pin == "" && entry.Layout != home.LayoutFork && ctx.Err() == nil {
		branch, err := inv.git.DefaultBranch(ctx, entry.URL)
		switch {
		case err != nil:
			inv.out.debugf("reading the default branch of %s: %v", entry.URL, err)
		case branch != "" && !source.ValidRef(branch):
			// The server names any branch it likes; one no pin may name
			// is not recorded, so that no export holds what import refuses.
			inv.out.debugf("the default branch of %s, %q, is not a ref agentx accepts", entry.URL, branch)
		default:
			c.defaultBranch = branch
		}
	}
	c.ok = ctx.Err() == nil
	c.checked = time.Now().UTC().Format(time.RFC3339)
	return c
}

// apply records the check on the settings entry: access and when it was
// checked, the access left out for an unknown one, and the default branch
// when one was read, so that an entry keeps the last one it knew.
func (c sourceCheck) apply(entry *home.Source) {
	if !c.ok {
		return
	}
	entry.Access = c.access.Access
	if entry.Access == home.AccessUnknown {
		entry.Access = ""
	}
	entry.AccessChecked = c.checked
	if c.defaultBranch != "" {
		entry.DefaultBranch = c.defaultBranch
	}
}

// reason is the access_reason of the event of the run that checked: why the
// access is not writable, "" when it is or when nothing was checked.
func (c sourceCheck) reason() string {
	if !c.ok || c.access.Access == home.AccessWritable {
		return ""
	}
	return c.access.Reason
}

// report warns about an answer the user can do something about: a host
// that wants their token or key authorised for an organisation before it
// says whether they may write.
func (c sourceCheck) report(inv *invocation, url string) {
	if c.ok && c.access.Authorise {
		inv.out.warnWith(url+": "+c.access.Reason, "authorise your token or key for the organisation, then run 'agentx source fetch "+shellWord(url)+"'")
	}
}

// entryEvent is the source event of a settings entry: everything the entry
// records, layout and access by their reported names.
func entryEvent(entry home.Source) sourceEvent {
	return sourceEvent{event: newEvent("source"), ID: source.ID(entry.URL), URL: entry.URL, Alias: entry.Alias, Layout: entry.LayoutName(),
		Account: entry.Account, Pin: entry.Pin, Access: entry.AccessName(), AccessChecked: entry.AccessChecked,
		DefaultBranch: entry.DefaultBranch, LastFetched: entry.LastFetched}
}

// accessPhrase ends the confirmation of a source that was just checked with
// what this machine may do there.
func accessPhrase(out *writer, access string) string {
	switch access {
	case home.AccessWritable:
		return "; " + out.paint(okStyle, "you can write to it")
	case home.AccessReadOnly:
		return "; " + out.paint(muted, "read-only for you")
	}
	return "; " + out.paint(muted, "write access unknown")
}

// accessWord is the access column of source list.
func accessWord(access string) cell {
	switch access {
	case home.AccessWritable:
		return c(access, okStyle)
	case home.AccessReadOnly:
		return c(access, plain)
	}
	return c(home.AccessUnknown, muted)
}
