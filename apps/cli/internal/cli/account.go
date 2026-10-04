package cli

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/interrupt"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// The account remote is the source its settings entry marks with the
// account flag: a Git repository the user owns,
// holding one branch per fork, which forks are published to and another
// machine of the user's installs them from. Its remote in the account repo
// is named after its id, src-<id>, as every source's is, with one fetch
// refspec: every fork branch onto a remote-tracking branch of the same
// name, see source.RemoteOf. Only fork branches travel, both ways: import
// branches, update candidates, upstream-removed markers and source refs
// stay on the machine that wrote them, and tags are never fetched. Every
// git that reaches the remote runs in the user's own environment, where
// their credential helpers, SSH keys and URL rewrites apply, but with no
// hook of theirs, see gitx.FetchRemote. A fork repository is fetched whole,
// never without blobs: its forks' history is what the account step,
// publish and history read.
//
// The account remote is set only by source add <url> --account, see
// addAccount, and detached only by source remove, see removeAccount.

// accountEntry is the settings entry of the account remote, ok false when
// the settings mark none.
func accountEntry(s home.Settings) (entry home.Source, ok bool) {
	for _, src := range s.Sources {
		if src.Account {
			return src, true
		}
	}
	return home.Source{}, false
}

// accountSource is the account remote as the settings record it: its
// entry, and the name of its git remote in the account repo, which every
// command that reads, fetches or pushes the account remote's fork branches
// works with. ok is false when no account remote is set. It runs no git.
func (inv *invocation) accountSource() (entry home.Source, remote string, ok bool, err error) {
	s, err := inv.loadSettings()
	if err != nil {
		return home.Source{}, "", false, err
	}
	if entry, ok := accountEntry(s); ok {
		return entry, source.RemoteName(source.ID(entry.URL)), true, nil
	}
	return home.Source{}, "", false, nil
}

// accountRemote is the account repo and the account remote, see
// accountSource, for a command that fetches the remote, refusing, exit code
// 6, when no remote is set. The account repo is created when this machine
// has none yet, as settings an import wrote leave it, and the remote is
// written into it when it is not the one the entry wants, see alignAccount.
func (inv *invocation) accountRemote(ctx context.Context) (gitDir string, entry home.Source, remote string, err error) {
	entry, remote, ok, err := inv.accountSource()
	if err != nil {
		return "", home.Source{}, "", err
	}
	if !ok {
		return "", home.Source{}, "", noRemoteRefusal()
	}
	gitDir, _, err = gitx.OpenAccountRepo(ctx, inv.git, inv.dirs.Home)
	if err != nil {
		return "", home.Source{}, "", accountRepoFailure(err)
	}
	if err := inv.alignAccount(ctx, gitDir, entry); err != nil {
		return "", home.Source{}, "", err
	}
	return gitDir, entry, remote, nil
}

// alignAccount writes the remote of the account remote's entry into the
// account repo when the remote there is not the one the entry wants, see
// source.RemoteOf: missing, as settings an import wrote leave it, or
// rewritten, as a run cut short leaves it. The remotes
// are read in one git process, and only a remote that disagrees is written,
// under the lock, since git config fails rather than waits for its own lock
// file. One written over a remote never fetched whole is fetched with
// --refetch the first time, see awaitWholeFetch.
func (inv *invocation) alignAccount(ctx context.Context, gitDir string, entry home.Source) error {
	remotes := source.Remotes(ctx, inv.git, gitDir)
	if remotes[source.ID(entry.URL)] == source.RemoteOf(entry) {
		return nil
	}
	if err := home.MutateQuiet(inv.dirs.Home, inv.refs(ctx), func() error {
		return source.Configure(ctx, inv.git, gitDir, entry)
	}); err != nil {
		return accountRepoFailure(err)
	}
	inv.awaitWholeFetch(remotes, entry)
	return nil
}

// fetchedWhole reports whether remotes, the account repo's as
// source.Remotes reads them, hold the remote of id as only a fetch of the
// account remote leaves it: with the fork refspec. A remote that is
// missing, or that a shared source of the same repository left, as an
// import of settings naming that repository as the account remote does,
// was never fetched whole on this machine.
func fetchedWhole(remotes map[string]source.Remote, id string) bool {
	r, ok := remotes[id]
	return ok && r.Refspec == gitx.ForkRefspec(source.RemoteName(id))
}

// awaitWholeFetch marks the remote of the account remote entry, which an
// alignment just wrote, for a first fetch with --refetch when remotes, as
// they were before it, show it was never fetched whole, see fetchedWhole
// and fetchForks.
func (inv *invocation) awaitWholeFetch(remotes map[string]source.Remote, entry home.Source) {
	if id := source.ID(entry.URL); !fetchedWhole(remotes, id) {
		inv.unfetched.Store(source.RemoteName(id), entry.URL)
	}
}

// noRemoteRefusal refuses a command that works with the account remote on
// a machine that has none.
func noRemoteRefusal() *failure {
	return refuse(exitRefused, "no account remote is set", "run 'agentx source add <url> --account' to attach a Git repository you own")
}

// fetchRemote fetches the fork branches of the account remote, the git
// remote called remote at url, see fetchForks, outside the lock, and
// reports it as one progress event. A remote git cannot reach is exit code
// 3.
func (inv *invocation) fetchRemote(ctx context.Context, gitDir, remote, url string) error {
	if err := inv.fetchForks(ctx, gitDir, remote, false); err != nil {
		return unreachableRemote(url, err)
	}
	inv.out.emit(progressEvent{event: newEvent("progress"), Phase: "fetch", Subject: shownURL(url), Current: 1, Total: 1})
	return nil
}

// fetchForks fetches the fork branches of the remote called remote, see
// gitx.FetchRemote, with --refetch when refetch is set: the first add of
// the account remote on this machine asks for it, see addAccount. So does
// the first fetch after an alignment wrote the remote over one never
// fetched whole, see awaitWholeFetch, once a probe of the repository finds
// a fork branch there; a fetch so made that fails takes the remote out of
// the account repo again, under the lock, so that the next run writes it
// and refetches in turn rather than read a remote with the fork refspec as
// fetched whole.
func (inv *invocation) fetchForks(ctx context.Context, gitDir, remote string, refetch bool) error {
	url, first := inv.unfetched.LoadAndDelete(remote)
	if !first {
		return inv.git.FetchRemote(ctx, gitDir, remote, refetch)
	}
	seen, err := inv.git.ProbeRemote(ctx, url.(string))
	if err == nil {
		err = inv.git.FetchRemote(ctx, gitDir, remote, seen)
	}
	if err != nil {
		undo := interrupt.Uninterruptible(ctx)
		waiting, cancel := context.WithTimeout(undo, takeBackBound(ctx))
		defer cancel()
		if uerr := home.MutateQuietWaiting(waiting, inv.dirs.Home, inv.refs(ctx), func() error {
			return inv.git.UnsetRemote(undo, gitDir, remote)
		}); uerr != nil {
			inv.out.debugf("the remote %s could not be taken back: %v", remote, uerr)
		}
	}
	return err
}

// unreachableRemote is a git that could not reach the account remote at
// url: exit code 3, as for a source.
func unreachableRemote(url string, err error) *failure {
	return refuse(exitSource, "cannot reach the account remote "+shownURL(url)+": "+trimGit(err.Error()),
		"check the URL and that you can reach it with git; a private repository needs a git credential helper (git config credential.helper) or an SSH key")
}

// accountURL reads arg as the URL of the account remote, refusing, exit
// code 1, what the account remote cannot be: a URL source.Parse does not
// read, one with a user or a token in it, which an account remote is
// fetched and pushed with and agentx never stores, and one that names a
// ref or a folder, since the account remote is a whole repository.
func accountURL(arg string) (source.Source, *failure) {
	src, err := source.Parse(arg)
	switch {
	case err != nil:
		return src, refuse(exitUsage, err.Error(), "accepted forms: "+source.Forms)
	case src.Stripped:
		return src, refuse(exitUsage, "the URL carries a password or a token, and agentx never stores credentials",
			"put them in a git credential helper (git config credential.helper) or use an SSH key, and give the URL without them")
	case src.Ref != "" || src.Subpath != "":
		return src, refuse(exitUsage, src.URL+" names a folder or a ref, and the account remote is a whole repository", "give the URL of the repository alone")
	}
	return src, nil
}

// accountRefusal is why want, the account remote an add is about to write,
// cannot be it given the settings s, nil when it can; resolve reads the
// host an SSH alias names, as source.SameRepository takes it. A shared
// source, any entry but the account remote, of the same URL or of another
// URL naming the same repository is refused, exit code 6, the hint saying
// to remove it first: its skills are fetched on one branch without blobs,
// and the repository would be fetched both ways. Pure but for resolve.
func accountRefusal(s home.Settings, want home.Source, resolve func(host string) string) *failure {
	for _, shared := range s.Sources {
		if shared.Account || (shared.URL != want.URL && !sameRepository(want, shared, resolve)) {
			continue
		}
		what := want.URL + " is a shared source"
		if shared.URL != want.URL {
			what = want.URL + " names the repository of the shared source " + shared.URL
		}
		return refuse(exitRefused, what+", and a repository cannot be both a shared source and the account remote",
			"remove the source first with 'agentx source remove "+shellWord(shared.URL)+"', or choose another repository")
	}
	return nil
}

// addAccount makes the repository arg names the account remote: source add
// --account and a source add of the account remote's URL again both run it.
// arg is held to the rules of accountURL before anything is read.
//
// It is source add for the account remote, step by step. A shared source of
// the settings naming the same repository refuses it, see accountRefusal,
// exit code 6. The repository is reached first, in the user's environment,
// so that a URL git cannot reach changes nothing, exit code 3, an account
// repo included. Then the account repo is opened, created when this is its
// first use, its remote is written under the lock, its fork branches are
// fetched outside it, see fetchForks, and what this machine may do there is
// checked, see checkSource. The fetch of a remote the account repo did not
// hold as fetched whole, see fetchedWhole, from a repository the probe
// found holding fork branches, is made with --refetch: a shared source of
// the same repository, removed before or replaced by an import, may have
// left objects fetched without their blobs in the account repo, and git
// would otherwise leave those blobs out of this fetch. That is decided by
// the account repo, not the settings, which an import can name the
// account remote in before this machine fetched it. A repository with no
// fork branch has nothing to refetch, and git would wait on it for good,
// see gitx.FetchRemote. Last, in one hold of the lock, the settings
// entry is written, and the account remote it replaces, if any, is taken
// out of the settings and its remote out of the account repo, its
// remote-tracking branches first, then its source ref and staging refs.
// Local forks stay as they are: those of a replaced account remote are
// published to the new one by name, see remoteSelection. A run that gets
// no further than its fetch takes back the remote it wrote, as an add
// does.
func (inv *invocation) addAccount(ctx context.Context, arg string) error {
	src, f := accountURL(arg)
	if f != nil {
		return f
	}
	resolve := inv.sshHosts(ctx)
	before, err := inv.loadSettings()
	if err != nil {
		return err
	}
	existing := before.FindSource(src.URL)
	want := home.Source{URL: src.URL, Account: true}
	if f := accountRefusal(before, want, resolve); f != nil {
		return f
	}
	seen, err := inv.git.ProbeRemote(ctx, src.URL)
	if err != nil {
		return unreachableRemote(src.URL, err)
	}
	gitDir, _, err := gitx.OpenAccountRepo(ctx, inv.git, inv.dirs.Home)
	if err != nil {
		return accountRepoFailure(err)
	}
	id, remote := src.ID(), source.RemoteName(src.ID())
	whole := fetchedWhole(source.Remotes(ctx, inv.git, gitDir), id)
	// revert takes back the remote this add wrote, for a run that gets no
	// further: a remote not fetched whole before, the remote of a source
	// that was not there or one a shared source of the same repository
	// left, goes with what its fetch wrote, so that the next add refetches
	// in turn, and that of the account remote added again goes back to what
	// its entry wants. Its context is the command's with the stop signals
	// taken off it, as an add's is: see addSource.
	undo := interrupt.Uninterruptible(ctx)
	revert := func() error {
		if existing < 0 || !whole {
			return inv.git.UnsetRemote(undo, gitDir, remote)
		}
		return source.Configure(undo, inv.git, gitDir, before.Sources[existing])
	}
	left := func(cause, err error) error {
		if err == nil {
			return cause
		}
		inv.out.debugf("the remote %s could not be taken back: %v", remote, err)
		return leftBehind(cause, id, "agentx source add "+shellWord(sanitised(src.URL))+" --account")
	}
	takeBack := func(cause error) error {
		waiting, cancel := context.WithTimeout(undo, takeBackBound(ctx))
		defer cancel()
		return left(cause, home.MutateQuietWaiting(waiting, inv.dirs.Home, inv.refs(ctx), revert))
	}
	if err := home.MutateQuiet(inv.dirs.Home, inv.refs(ctx), func() error {
		return source.Configure(ctx, inv.git, gitDir, want)
	}); err != nil {
		return accountRepoFailure(err)
	}
	if err := inv.fetchForks(ctx, gitDir, remote, seen && !whole); err != nil {
		if ctx.Err() != nil {
			return takeBack(ctx.Err())
		}
		return takeBack(unreachableRemote(src.URL, err))
	}
	inv.out.emit(progressEvent{event: newEvent("progress"), Phase: "fetch", Subject: shownURL(src.URL), Current: 1, Total: 1})
	check := inv.checkSource(ctx, want)
	if err := ctx.Err(); err != nil {
		return takeBack(err)
	}
	tips, err := lineage.ListRemote(ctx, inv.git, gitDir, remote)
	if err != nil {
		return takeBack(accountRepoFailure(err))
	}
	entry := want
	entry.LastFetched = time.Now().UTC().Format(time.RFC3339)
	check.apply(&entry)
	var replaced home.Source
	settled := false
	err = home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		settled = true
		s, err := inv.loadSettings()
		if err != nil {
			return left(err, revert())
		}
		if f := accountRefusal(s, want, resolve); f != nil {
			return left(f, revert())
		}
		if i := s.FindSource(src.URL); i >= 0 {
			entry = entry.Merge(s.Sources[i]) // the alias and anything else this add did not find out is kept
		}
		for _, other := range s.Sources {
			if other.Account && other.URL != src.URL {
				replaced = other
			}
		}
		if replaced.URL != "" {
			s.RemoveSource(replaced.URL)
		}
		s.SetSource(entry)
		if err := home.SaveSettings(inv.dirs.Home, s); err != nil {
			return left(err, revert())
		}
		// What the settings no longer name goes now, under the same hold:
		// the remote of the account remote replaced, once the fork branches
		// that tracked it track this one.
		if replaced.URL != "" {
			old := source.RemoteName(source.ID(replaced.URL))
			if err := inv.retrack(ctx, gitDir, old, remote); err != nil {
				return accountRepoFailure(err)
			}
			if err := inv.git.UnsetRemote(ctx, gitDir, old); err != nil {
				return accountRepoFailure(err)
			}
			// Then its source ref and the staging refs a killed fetch of
			// its URL can have left, as a shared source add of that URL
			// cut short does, see removeAccount.
			if err := source.Remove(ctx, inv.git, gitDir, source.ID(replaced.URL)); err != nil {
				return accountRepoFailure(err)
			}
		}
		return nil
	})
	if err != nil {
		if !settled {
			return takeBack(err)
		}
		return err
	}
	check.report(inv, entry.URL)
	n := len(tips)
	ev := entryEvent(entry)
	ev.Skills, ev.AccessReason = &n, check.reason()
	inv.out.emit(ev)
	holds := "; it holds " + plural(n, "skill")
	inv.summary = "the account remote is now " + entry.URL + holds
	inv.out.done("the account remote is now " + inv.out.paint(heading, entry.URL) + holds + accessPhrase(inv.out, entry.AccessName()))
	return nil
}

// removeAccount detaches the account remote entry names in one hold of the
// lock: its remote is taken out of the account repo, see gitx.UnsetRemote,
// the remote-tracking branches first, then its source ref and every
// staging ref a killed fetch of its URL can have left, as a shared source
// add of that URL cut short does, see source.Remove, then its settings
// entry. source remove of the account remote's URL or id runs it. Local
// fork branches, their worktrees and every commit stay as they are, and
// nothing on the remote changes.
func (inv *invocation) removeAccount(ctx context.Context, entry home.Source) error {
	id := source.ID(entry.URL)
	remote := source.RemoteName(id)
	if err := home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		gitDir, exists, err := gitx.CheckAccountRepo(ctx, inv.git, inv.dirs.Home)
		if err != nil {
			return accountRepoFailure(err)
		}
		if exists {
			if err := inv.git.UnsetRemote(ctx, gitDir, remote); err != nil {
				return accountRepoFailure(err)
			}
			if err := source.Remove(ctx, inv.git, gitDir, id); err != nil {
				return accountRepoFailure(err)
			}
		}
		s, err := inv.loadSettings()
		if err != nil {
			return err
		}
		s.RemoveSource(entry.URL)
		return home.SaveSettings(inv.dirs.Home, s)
	}); err != nil {
		return mutationFailure(err)
	}
	inv.out.emit(sourceEvent{event: newEvent("source"), ID: id, URL: entry.URL})
	u := shownURL(entry.URL)
	inv.summary = "the account remote " + u + " is no longer set; your skills on this machine are as they were"
	inv.out.done("the account remote " + inv.out.paint(heading, u) + " is no longer set; your skills on this machine are as they were")
	return nil
}

// forkCount is the number of fork branches the last fetch of the account
// remote entry, whose git remote is called remote, found, read in one
// for-each-ref, and whether it was fetched at all: an entry with no
// remote-tracking branch and no record of a fetch was not.
func (inv *invocation) forkCount(ctx context.Context, gitDir string, entry home.Source, remote string) (n int, fetched bool, err error) {
	tips, err := lineage.ListRemote(ctx, inv.git, gitDir, remote)
	if err != nil {
		return 0, false, err
	}
	return len(tips), len(tips) > 0 || entry.LastFetched != "", nil
}

// forksCell is the commit column of the account remote in source list: how many
// forks it held at its last fetch.
func forksCell(n int, fetched bool) cell {
	if !fetched {
		return c("not fetched", warnStyle)
	}
	return c(plural(n, "skill"), muted)
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

// shownURL is a remote URL as agentx prints it: a password someone wrote
// into the account repo's configuration by hand is never shown.
func shownURL(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.User != nil {
		if _, ok := u.User.Password(); ok {
			u.User = url.UserPassword(u.User.Username(), "***")
			return sanitised(u.String())
		}
	}
	return sanitised(raw)
}
