package cli

import (
	"context"
	"strings"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// readOnly are the commands that change nothing of agentx home, by their
// path. They read an account remote an earlier agentx left as the remote
// origin where it is, see accountSource, and every other command moves it
// into the settings before it reads anything, see migrateAccount.
var readOnly = map[string]bool{
	"agentx config get":    true,
	"agentx config list":   true,
	"agentx export":        true,
	"agentx remote show":   true,
	"agentx scan":          true,
	"agentx skill diff":    true,
	"agentx skill history": true,
	"agentx skill list":    true,
	"agentx source list":   true,
	"agentx source skills": true,
}

// migrates reports whether cmd moves an account remote an earlier agentx
// left as the remote origin before it runs: every command that can change
// something does, but serve, and a command that only groups others runs
// nothing. serve runs for as long as the desktop app does and must not
// fail to start over a lock another command holds: its reconciliation
// reads the remote where it is.
func migrates(cmd *cobra.Command) bool {
	return !cmd.HasSubCommands() && !readOnly[cmd.CommandPath()] && cmd.CommandPath() != "agentx serve"
}

// migrateAccount moves the account remote an earlier agentx kept as the
// account repo's remote origin into the settings, where every source is,
// see migrateOrigin, in a hold of the lock of its own: a command that
// changes something runs it before its first read, so that nothing it
// reads moves under it. The account repo's configuration file is read
// first, which costs no git: a machine whose account repo has no remote
// origin, almost every one, takes no lock for it.
func (inv *invocation) migrateAccount(ctx context.Context) error {
	gitDir := gitx.AccountRepoPath(inv.dirs.Home)
	if !hasOrigin(gitDir) {
		return nil
	}
	if err := home.MutateQuiet(inv.dirs.Home, inv.refs(ctx), func() error { return inv.migrateOrigin(ctx, gitDir) }); err != nil {
		return mutationFailure(err)
	}
	return nil
}

// migrateOrigin moves the remote origin of the account repo at gitDir, the
// account remote as an earlier agentx kept it, to the source entry and the
// remote every source has, under the lock its caller holds. Each step can
// be repeated, and the remote origin is the last thing to go, so that a
// run stopped anywhere leaves what the next one finishes:
//
//  1. The remote origin is read from the account repo's own configuration
//     file. One agentx did not write, see legacyOrigin, or whose URL is not
//     one a source can have, is left alone: doctor names it.
//  2. The remote of the source is written as a fork source's, see
//     source.RemoteOf. A source of the settings that cannot become the
//     account remote, see accountRefusal, stops the move with nothing
//     changed: doctor names it, and every command reads the remote origin
//     until the source is removed.
//  3. Every remote-tracking branch of origin is copied to the same name
//     under the source's remote, in one transaction; one the source's
//     remote holds already is left as it is.
//  4. The settings entry is written: the account remote, of the fork
//     layout, with a push URL when the old URL differs from the canonical
//     one in more than its spelling. Once it is there, the account remote
//     is the source, and every command reads it so.
//  5. The tracking configuration of every fork branch naming origin names
//     the source's remote instead.
//  6. The remote origin goes, its remote-tracking branches first, see
//     gitx.UnsetRemote.
//
// A settings entry of the same URL that is already the account remote is
// a move a stop cut short after step 4, which goes on from step 5; one of
// another URL means the remote origin is not the account remote any more,
// and it is left alone.
func (inv *invocation) migrateOrigin(ctx context.Context, gitDir string) error {
	url, present, ours, err := inv.legacyOrigin(ctx, gitDir)
	if err != nil || !present || !ours {
		return err
	}
	src, err := source.Parse(url)
	if err != nil || src.Stripped || src.Ref != "" || src.Subpath != "" {
		inv.out.debugf("the account repo's remote origin %s is not a URL a source can have, so it stays where it is", shownURL(url))
		return nil
	}
	s, err := inv.loadSettings()
	if err != nil {
		return err
	}
	remote := source.RemoteName(src.ID())
	entry, has := accountEntry(s)
	switch {
	case has && entry.URL != src.URL:
		inv.out.debugf("the account remote is %s, so the account repo's remote origin %s is not moved", entry.URL, shownURL(url))
		return nil
	case !has:
		resolve := inv.sshHosts(ctx)
		push, _, f := pushURLFor(url, src.URL, resolve)
		if f != nil {
			inv.out.debugf("the account repo's remote origin %s: %s", shownURL(url), f.message)
			return nil
		}
		entry = home.Source{URL: src.URL, Layout: home.LayoutFork, Account: true, PushURL: push}
		installed, err := inv.installedFrom(ctx)
		if err != nil {
			return err
		}
		if f := accountRefusal(s, entry, installed, resolve); f != nil {
			inv.out.debugf("the account repo's remote origin %s stays where it is: %s", shownURL(url), f.message)
			return nil
		}
		if err := source.Configure(ctx, inv.git, gitDir, entry); err != nil {
			return accountRepoFailure(err)
		}
	}
	if err := inv.copyForkRefs(ctx, gitDir, gitx.OriginRemote, remote); err != nil {
		return accountRepoFailure(err)
	}
	if !has {
		if i := s.FindSource(entry.URL); i >= 0 {
			entry = entry.Merge(s.Sources[i])
			entry.DefaultBranch = ""
		}
		s.SetSource(entry)
		if err := home.SaveSettings(inv.dirs.Home, s); err != nil {
			return err
		}
	}
	if err := inv.retrack(ctx, gitDir, gitx.OriginRemote, remote); err != nil {
		return accountRepoFailure(err)
	}
	if err := inv.git.UnsetRemote(ctx, gitDir, gitx.OriginRemote); err != nil {
		return accountRepoFailure(err)
	}
	inv.out.debugf("moved the account remote %s from the remote origin into the settings", entry.URL)
	return nil
}

// copyForkRefs creates, in one transaction, every remote-tracking fork
// branch of the remote called from under the same name for the remote
// called to, at the commit it holds; a branch to holds already is left as
// it is.
func (inv *invocation) copyForkRefs(ctx context.Context, gitDir, from, to string) error {
	out, err := inv.git.Isolated(ctx, gitDir, "for-each-ref", "--format=%(refname) %(objectname)", gitx.TrackingPrefix(from)+"skills/", gitx.TrackingPrefix(to)+"skills/")
	if err != nil {
		return err
	}
	held := map[string]bool{}
	var copies [][2]string
	for _, line := range strings.Split(out, "\n") {
		ref, oid, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		if name, ok := strings.CutPrefix(ref, gitx.TrackingPrefix(from)); ok {
			copies = append(copies, [2]string{gitx.TrackingPrefix(to) + name, oid})
		} else {
			held[ref] = true
		}
	}
	var b strings.Builder
	for _, cp := range copies {
		if !held[cp[0]] {
			b.WriteString("create " + cp[0] + " " + cp[1] + "\n")
		}
	}
	if b.Len() == 0 {
		return nil
	}
	_, err = inv.git.IsolatedInput(ctx, gitDir, strings.NewReader(b.String()), "update-ref", "--stdin")
	return err
}

// retrack makes every fork branch whose tracking configuration names the
// remote called from name the remote called to instead, see
// gitx.SetTracking, so that git status in its worktree goes on comparing it
// with its branch on the account remote.
func (inv *invocation) retrack(ctx context.Context, gitDir, from, to string) error {
	out, _, err := inv.git.IsolatedStatus(ctx, gitDir, 1, "config", "-z", "--get-regexp", `^branch\.skills/.*\.remote$`)
	if err != nil {
		return err
	}
	for _, record := range strings.Split(out, "\x00") {
		key, value, ok := strings.Cut(record, "\n")
		if !ok || value != from {
			continue
		}
		if _, err := inv.git.Isolated(ctx, gitDir, "config", key, to); err != nil {
			return err
		}
	}
	return nil
}
