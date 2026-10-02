package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/interrupt"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// The account remote is a source of the fork layout that its settings
// entry marks as the account remote: a Git repository the user owns,
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
// never without blobs: its forks' history is what pull, publish and revert
// read.
//
// An earlier agentx kept the account remote as the account repo's remote
// origin and nowhere else. A command that changes something moves such a
// remote into the settings first, see migrateAccount; until then a command
// that only reads finds it there, see legacyOrigin.

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

// accountSource is the account remote as this machine records it: its
// entry, and the name of its git remote in the account repo, which every
// command that reads, fetches or pushes the account remote's fork branches
// works with. ok is false when no account remote is set. The entry is read
// from the settings; with none there, the remote origin an earlier agentx
// left in the account repo stands in for it, see legacyOrigin, its URL as
// that remote records it.
func (inv *invocation) accountSource(ctx context.Context) (entry home.Source, remote string, ok bool, err error) {
	return inv.accountSourceIn(ctx, gitx.AccountRepoPath(inv.dirs.Home))
}

// accountSourceIn is accountSource for a command that has the account
// repo's path at hand. It runs no git unless the settings mark no account
// remote and the account repo's configuration names a remote origin.
func (inv *invocation) accountSourceIn(ctx context.Context, gitDir string) (entry home.Source, remote string, ok bool, err error) {
	s, err := inv.loadSettings()
	if err != nil {
		return home.Source{}, "", false, err
	}
	if entry, ok := accountEntry(s); ok {
		return entry, source.RemoteName(source.ID(entry.URL)), true, nil
	}
	url, present, ours, err := inv.legacyOrigin(ctx, gitDir)
	if err != nil || !present || !ours {
		return home.Source{}, "", false, err
	}
	return home.Source{URL: url, Layout: home.LayoutFork, Account: true}, gitx.OriginRemote, true, nil
}

// originSection is the header of the remote origin's section in a git
// configuration file: the section name is read without regard to case, the
// subsection name as written, as git reads both.
var originSection = regexp.MustCompile(`(?m)^[ \t]*\[(?i:remote)[ \t]+"origin"\]`)

// hasOrigin reports whether the configuration file of the account repo at
// gitDir has a section for the remote origin. It reads one file and runs no
// git, so that every command can afford to ask: almost every machine says
// no.
func hasOrigin(gitDir string) bool {
	b, err := os.ReadFile(filepath.Join(gitDir, "config"))
	return err == nil && originSection.Match(b)
}

// legacyOrigin reads the remote origin of the account repo at gitDir from
// its own configuration file alone, so that a remote.origin.* of the
// user's global configuration never reads as one: present is false when the
// file has no such section. ours says whether it is the account remote an
// earlier agentx wrote: one URL and one fetch refspec, the fork branches
// onto refs/remotes/origin/skills/*. Any other origin, one the user wrote
// into the account repo by hand, is no account remote, and nothing of
// agentx's reads or moves it.
func (inv *invocation) legacyOrigin(ctx context.Context, gitDir string) (url string, present, ours bool, err error) {
	if !hasOrigin(gitDir) {
		return "", false, false, nil
	}
	out, _, err := inv.git.IsolatedStatus(ctx, gitDir, 1, "config", "--file", filepath.Join(gitDir, "config"), "-z", "--get-regexp", `^remote\.origin\.(url|fetch)$`)
	if err != nil {
		return "", true, false, accountRepoFailure(err)
	}
	var urls, fetches []string
	for _, record := range strings.Split(out, "\x00") {
		key, value, _ := strings.Cut(record, "\n")
		switch strings.ToLower(key) {
		case "remote.origin.url":
			urls = append(urls, value)
		case "remote.origin.fetch":
			fetches = append(fetches, value)
		}
	}
	if len(urls) != 1 {
		return "", true, false, nil
	}
	return urls[0], true, len(fetches) == 1 && fetches[0] == gitx.ForkRefspec(gitx.OriginRemote), nil
}

// accountRemote is the account repo and the account remote, see
// accountSource, for a command that fetches the remote, refusing, exit code
// 6, when no remote is set. The account repo is created when this machine
// has none yet, as settings an import wrote leave it, and the remote is
// written into it when it is not the one the entry wants, see alignAccount.
func (inv *invocation) accountRemote(ctx context.Context) (gitDir string, entry home.Source, remote string, err error) {
	entry, remote, ok, err := inv.accountSource(ctx)
	if err != nil {
		return "", home.Source{}, "", err
	}
	if !ok {
		return "", home.Source{}, "", noRemoteRefusal()
	}
	if remote == gitx.OriginRemote { // read where an earlier agentx left it, as it is
		return gitx.AccountRepoPath(inv.dirs.Home), entry, remote, nil
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
// rewritten, as a run cut short or an older agentx leaves it. The remotes
// are read in one git process, and only a remote that disagrees is written,
// under the lock, since git config fails rather than waits for its own lock
// file.
func (inv *invocation) alignAccount(ctx context.Context, gitDir string, entry home.Source) error {
	if source.Remotes(ctx, inv.git, gitDir)[source.ID(entry.URL)] == source.RemoteOf(entry) {
		return nil
	}
	if err := home.MutateQuiet(inv.dirs.Home, inv.refs(ctx), func() error {
		return source.Configure(ctx, inv.git, gitDir, entry)
	}); err != nil {
		return accountRepoFailure(err)
	}
	return nil
}

// noRemoteRefusal refuses a command that works with the account remote on
// a machine that has none.
func noRemoteRefusal() *failure {
	return refuse(exitRefused, "no account remote is set", "run 'agentx remote set <url>' to attach a Git repository you own")
}

// fetchRemote fetches the fork branches of the account remote, the git
// remote called remote at url, see fetchForks, outside the lock, and
// reports it as one progress event. A remote git cannot reach is exit code
// 3.
func (inv *invocation) fetchRemote(ctx context.Context, gitDir, remote, url string) error {
	marker, err := inv.fetchForks(ctx, gitDir, remote)
	if err != nil {
		return unreachableRemote(url, err)
	}
	inv.dropMarker(ctx, gitDir, marker)
	inv.out.emit(progressEvent{event: newEvent("progress"), Phase: "fetch", Subject: shownURL(url), Current: 1, Total: 1})
	return nil
}

// fetchForks fetches the fork branches of the remote called remote, see
// gitx.FetchRemote. A fork source whose source ref is in the account repo
// was fetched as a tree source before, without blobs, by an add that took
// it for one or by an agentx that knows no layout, and git would leave out
// of this fetch every blob those commits seem to have: that fetch is made
// with --refetch, and marker names the source ref, which the caller deletes
// once what it fetched is recorded, so that the next fetch is an ordinary
// one again.
func (inv *invocation) fetchForks(ctx context.Context, gitDir, remote string) (marker string, err error) {
	refetch := false
	if id, ok := strings.CutPrefix(remote, "src-"); ok && source.IsID(id) {
		out, err := inv.git.Isolated(ctx, gitDir, "for-each-ref", "--format=%(objectname)", source.Ref(id))
		if err != nil {
			return "", err
		}
		if refetch = out != ""; refetch {
			marker = source.Ref(id)
		}
	}
	if err := inv.git.FetchRemote(ctx, gitDir, remote, refetch); err != nil {
		return "", err
	}
	return marker, nil
}

// dropMarker deletes the source ref a fetch of fork branches with
// --refetch named as its marker, see fetchForks. A ref that cannot be
// deleted costs one more such fetch next time, and nothing else.
func (inv *invocation) dropMarker(ctx context.Context, gitDir, marker string) {
	if marker == "" {
		return
	}
	if _, err := inv.git.Isolated(ctx, gitDir, "update-ref", "-d", marker); err != nil {
		inv.out.debugf("deleting %s: %v", marker, err)
	}
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
// ref or a folder, since a fork source is a whole repository.
func accountURL(arg string) (source.Source, *failure) {
	src, err := source.Parse(arg)
	switch {
	case err != nil:
		return src, refuse(exitUsage, err.Error(), "accepted forms: "+source.Forms)
	case src.Stripped:
		return src, refuse(exitUsage, "the URL carries a password or a token, and agentx never stores credentials",
			"put them in a git credential helper (git config credential.helper) or use an SSH key, and give the URL without them")
	case src.Ref != "" || src.Subpath != "":
		return src, refuse(exitUsage, src.URL+" names a folder or a ref, and a fork source is a whole repository", "give the URL of the repository alone")
	}
	return src, nil
}

// installedFrom counts, by the canonical URL of their source, the managed
// skills this machine installed, as their lineage records them; none on a
// machine with no account repo.
func (inv *invocation) installedFrom(ctx context.Context) (map[string]int, error) {
	gitDir, exists, err := gitx.CheckAccountRepo(ctx, inv.git, inv.dirs.Home)
	if err != nil || !exists {
		return map[string]int{}, accountRepoFailureOrNil(err)
	}
	records, err := lineage.List(ctx, inv.git, gitDir)
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	counts := map[string]int{}
	for _, rec := range records {
		if rec.Kind == lineage.KindManaged && rec.HasImport {
			counts[rec.Import.Source]++
		}
	}
	return counts, nil
}

// accountRefusal is why want, the account remote an add is about to write,
// cannot be it given the settings s, nil when it can; installed counts the
// managed skills of each source, see installedFrom, and resolve reads the
// host an SSH alias names, as source.Compare takes it. A tree source of the
// same URL becomes the account remote only while no managed skill was
// installed from it: its skills are fetched on one branch without blobs,
// which neither fork fetches nor the forks' history would ever read again.
// A tree source of another URL naming the same repository is refused too,
// since the repository would be fetched both ways. Both are exit code 6,
// the hint saying to remove the source first. Pure but for resolve.
func accountRefusal(s home.Settings, want home.Source, installed map[string]int, resolve func(host string) string) *failure {
	for _, tree := range s.Sources {
		switch {
		case tree.Layout == home.LayoutFork:
		case tree.URL == want.URL && installed[tree.URL] > 0:
			return refuse(exitRefused, fmt.Sprintf("%s is a source this machine installed %s from; the account remote must be a repository of your own that holds forks",
				tree.URL, plural(installed[tree.URL], "skill")),
				"remove the source first with 'agentx source remove "+shellWord(tree.URL)+"', or choose another repository")
		case tree.URL != want.URL && sameRepository(want, tree, resolve):
			return refuse(exitRefused, want.URL+" names the repository of the source "+tree.URL+", which cannot be both a source of skills and the account remote",
				"remove the source first with 'agentx source remove "+shellWord(tree.URL)+"', or choose another repository")
		}
	}
	return nil
}

// addAccount makes the repository arg names the account remote, with the
// push URL push names: remote set, source add --account and a source add of
// the account remote's URL again all run it. arg is held to the rules of
// accountURL and push to those of pushURLFor before anything is read.
//
// It is source add for a fork source, step by step. A source of the
// settings that cannot become the account remote refuses it, see
// accountRefusal, exit code 6. The repository is reached first, in the
// user's environment, so that a URL git cannot reach changes nothing, exit
// code 3, an account repo included. Then the account repo is opened,
// created when this is its first use, its remote is written under the
// lock, its fork branches are fetched outside it, see fetchForks, and what
// this machine may do there is checked, see checkSource. Last, in one hold
// of the lock, the settings entry is written, the account remote it
// replaces, if any, is taken out of the settings and its remote out of the
// account repo, its remote-tracking branches first, and a tree source of
// the same URL becomes this entry. Local forks stay as they are: those of
// a replaced account remote are published to the new one from then on. A
// run that gets no further than its fetch takes back the remote it wrote,
// as an add does.
func (inv *invocation) addAccount(ctx context.Context, arg string, push pushChoice) error {
	src, f := accountURL(arg)
	if f != nil {
		return f
	}
	resolve := inv.sshHosts(ctx)
	if push.set {
		record, warning, f := pushURLFor(push.url, src.URL, resolve)
		if f != nil {
			return f
		}
		push.url, push.warning = record, warning
	}
	before, err := inv.loadSettings()
	if err != nil {
		return err
	}
	existing := before.FindSource(src.URL)
	want := home.Source{URL: src.URL, Layout: home.LayoutFork, Account: true, PushURL: push.url}
	if !push.set && existing >= 0 {
		want.PushURL = before.Sources[existing].PushURL
	}
	installed, err := inv.installedFrom(ctx)
	if err != nil {
		return err
	}
	if f := accountRefusal(before, want, installed, resolve); f != nil {
		return f
	}
	reach := source.RemoteOf(want).URL
	if err := inv.git.ProbeRemote(ctx, reach); err != nil {
		return unreachableRemote(reach, err)
	}
	gitDir, _, err := gitx.OpenAccountRepo(ctx, inv.git, inv.dirs.Home)
	if err != nil {
		return accountRepoFailure(err)
	}
	id, remote := src.ID(), source.RemoteName(src.ID())
	// revert takes back the remote this add wrote, for a run that gets no
	// further: the remote of a source that was not there before goes with
	// what its fetch wrote, and that of one that was goes back to what its
	// entry wants, with the remote-tracking branches a fetch of a tree
	// source never writes taken out. Its context is the command's with the
	// stop signals taken off it, as an add's is: see addSource.
	undo := interrupt.Uninterruptible(ctx)
	revert := func() error {
		if existing < 0 {
			return inv.git.UnsetRemote(undo, gitDir, remote)
		}
		held := before.Sources[existing]
		if held.Layout != home.LayoutFork {
			if err := inv.git.DropRemoteRefs(undo, gitDir, remote); err != nil {
				return err
			}
		}
		return source.Configure(undo, inv.git, gitDir, held)
	}
	left := func(cause, err error) error {
		if err == nil {
			return cause
		}
		inv.out.debugf("the remote %s could not be taken back: %v", remote, err)
		return leftBehind(cause, id, "agentx remote set "+shellWord(sanitised(reach)))
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
	marker, err := inv.fetchForks(ctx, gitDir, remote)
	if err != nil {
		if ctx.Err() != nil {
			return takeBack(ctx.Err())
		}
		return takeBack(unreachableRemote(reach, err))
	}
	inv.out.emit(progressEvent{event: newEvent("progress"), Phase: "fetch", Subject: shownURL(reach), Current: 1, Total: 1})
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
		installed, err := inv.installedFrom(ctx)
		if err != nil {
			return left(err, revert())
		}
		if f := accountRefusal(s, want, installed, resolve); f != nil {
			return left(f, revert())
		}
		if i := s.FindSource(src.URL); i >= 0 {
			entry = entry.Merge(s.Sources[i]) // the alias and anything else this add did not find out is kept
		}
		if push.set && push.url == "" {
			entry.PushURL = "" // --push-url= clears what Merge kept
		}
		entry.DefaultBranch = "" // a fork source follows no branch; one a tree source of the URL knew means nothing now
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
		// that tracked it track this one, and the source ref a tree source
		// of this URL left, which the fetch above replaced.
		if replaced.URL != "" {
			old := source.RemoteName(source.ID(replaced.URL))
			if err := inv.retrack(ctx, gitDir, old, remote); err != nil {
				return accountRepoFailure(err)
			}
			if err := inv.git.UnsetRemote(ctx, gitDir, old); err != nil {
				return accountRepoFailure(err)
			}
			if err := source.Remove(ctx, inv.git, gitDir, source.ID(replaced.URL)); err != nil {
				return accountRepoFailure(err)
			}
		}
		inv.dropMarker(ctx, gitDir, marker)
		return nil
	})
	if err != nil {
		if !settled {
			return takeBack(err)
		}
		return err
	}
	if push.warning != "" {
		inv.out.warn(push.warning)
	}
	check.report(inv, entry.URL)
	n := len(tips)
	ev := entryEvent(entry)
	ev.Forks, ev.AccessReason = &n, check.reason()
	inv.out.emit(ev)
	holds := "; it holds " + plural(n, "fork")
	inv.summary = "the account remote is now " + entry.URL + holds
	inv.out.done("the account remote is now " + inv.out.paint(heading, entry.URL) + holds + pushesTo(inv.out, entry.PushURL) + accessPhrase(inv.out, entry.AccessName()))
	return nil
}

// removeAccount detaches the account remote entry names, whose git remote
// is called remote, in one hold of the lock: its remote is taken out of the
// account repo, see gitx.UnsetRemote, the remote-tracking branches first,
// then its source ref, which a tree fetch of it may have left, then its
// settings entry. remote unset and source remove of the account remote's
// URL or id both run it. Local fork branches, their worktrees and every
// commit stay as they are, and nothing on the remote changes. An account
// remote an earlier agentx left as the remote origin, which nothing could
// move into the settings, has no entry, and its remote alone goes.
func (inv *invocation) removeAccount(ctx context.Context, entry home.Source, remote string) error {
	id := source.ID(entry.URL)
	if err := home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		gitDir, exists, err := gitx.CheckAccountRepo(ctx, inv.git, inv.dirs.Home)
		if err != nil {
			return accountRepoFailure(err)
		}
		if exists {
			if err := inv.git.UnsetRemote(ctx, gitDir, remote); err != nil {
				return accountRepoFailure(err)
			}
			if remote != gitx.OriginRemote {
				if err := source.Remove(ctx, inv.git, gitDir, id); err != nil {
					return accountRepoFailure(err)
				}
			}
		}
		if remote == gitx.OriginRemote {
			return nil
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
	inv.summary = "the account remote " + u + " is no longer set; the forks of this machine are as they were"
	inv.out.done("the account remote " + inv.out.paint(heading, u) + " is no longer set; the forks of this machine are as they were")
	return nil
}

// forkCount is the number of fork branches the last fetch of the fork
// source entry, whose git remote is called remote, found, read in one
// for-each-ref, and whether it was fetched at all: an entry with no
// remote-tracking branch and no record of a fetch was not.
func (inv *invocation) forkCount(ctx context.Context, gitDir string, entry home.Source, remote string) (n int, fetched bool, err error) {
	tips, err := lineage.ListRemote(ctx, inv.git, gitDir, remote)
	if err != nil {
		return 0, false, err
	}
	return len(tips), len(tips) > 0 || entry.LastFetched != "", nil
}

// forksCell is the commit column of a fork source in source list and
// remote show: how many forks it held at its last fetch.
func forksCell(n int, fetched bool) cell {
	if !fetched {
		return c("not fetched", warnStyle)
	}
	return c(plural(n, "fork"), muted)
}
