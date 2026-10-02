package cli

import (
	"context"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// The account remote is a Git repository the user owns, attached to the
// account repo as its one remote of fork branches, origin, see
// accountSource, with one fetch refspec: every fork branch onto a
// remote-tracking branch of the same name. Only fork branches travel, both
// ways: import branches, update candidates, upstream-removed markers and
// source refs stay on the machine that wrote them, and tags are never
// fetched. Every git that reaches the remote runs in the user's own
// environment, where their credential helpers, SSH keys and URL rewrites
// apply, but with no hook of theirs, see gitx.FetchRemote.

// remoteEvent is the account remote as the account repo records it.
type remoteEvent struct {
	event
	Name  string `json:"name"`
	URL   string `json:"url,omitempty"`   // absent when no remote is set
	Fetch string `json:"fetch,omitempty"` // the fetch refspec; absent when no remote is set
}

func newRemoteCommand(inv *invocation) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "remote",
		Short:       "Attach, show or detach the account remote forks are published to",
		Annotations: map[string]string{annotationGroup: "true"},
		Args:        cobra.NoArgs,
		RunE:        needSubcommand(inv, "no remote command given", "run 'agentx remote --help' to list commands"),
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "set <url>",
		Short: "Attach a Git repository you own as the account remote, and fetch its forks",
		Long: "Attach a Git repository you own as the account remote, then fetch the forks it\n" +
			"holds. The URL is any URL git reaches: https://, ssh://, git@host:path, file:// or\n" +
			"an absolute path, written as you give it. Credentials never go in it: use a git\n" +
			"credential helper or an SSH key. Only fork branches travel to and from the remote,\n" +
			"never managed skills, and setting another URL forgets what the old one held.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return inv.remoteSet(cmd.Context(), args[0]) },
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show the account remote",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return inv.remoteShow(cmd.Context()) },
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "unset",
		Short: "Detach the account remote; local forks stay as they are",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return inv.remoteUnset(cmd.Context()) },
	})
	return cmd
}

// scpRemote is git's SSH shorthand, [user@]host:path, which has no scheme
// and whose host part holds no slash.
var scpRemote = regexp.MustCompile(`^([^@/:\s]+@)?[^@/:\s]+:[^\s]+$`)

// remoteURLRefusal is why url cannot be the account remote, nil when it
// can: it has to be a URL git reaches as it is given, a URL with a scheme,
// the SSH shorthand or an absolute path, since agentx stores it as given
// and never rewrites it. A password or a token in it is refused rather than
// dropped, since what is left could reach another account or none, and a
// #ref is refused, since the remote holds every fork branch. Pure.
func remoteURLRefusal(raw string) *failure {
	const forms = "give a URL such as https://github.com/you/skills.git, git@github.com:you/skills.git or an absolute path"
	if raw == "" || strings.TrimSpace(raw) != raw || strings.HasPrefix(raw, "-") || strings.IndexFunc(raw, unicode.IsControl) >= 0 {
		return refuse(exitUsage, quotedArg(raw)+" is not a URL git can reach as the account remote", forms)
	}
	credential := refuse(exitUsage, "the URL carries a password or a token, and agentx never stores credentials",
		"put them in a git credential helper (git config credential.helper) or use an SSH key, and give the URL without them")
	if strings.Contains(raw, "#") {
		// The URL is not repeated: no credential check has read it yet.
		return refuse(exitUsage, "the URL names a ref with #, and the account remote holds every fork branch", "give the URL of the repository alone")
	}
	switch {
	case strings.Contains(raw, "://"):
		u, err := url.Parse(raw)
		if err != nil || u.Scheme == "" {
			return refuse(exitUsage, "the URL is not one git can read", forms)
		}
		if u.User != nil {
			_, password := u.User.Password()
			// git@ is the address of an SSH server, not a credential; a
			// password is, and so is a user over HTTP, where it is a token.
			if password || !strings.EqualFold(u.Scheme, "ssh") && !strings.EqualFold(u.Scheme, "git+ssh") {
				return credential
			}
		}
	case strings.HasPrefix(raw, "/"):
	case scpRemote.MatchString(raw):
		if user, _, ok := strings.Cut(raw, "@"); ok && strings.Contains(user, ":") {
			return credential
		}
	default:
		return refuse(exitUsage, quotedArg(raw)+" is not a URL git can reach as the account remote", forms)
	}
	return nil
}

// quotedArg is s in double quotes, the way a message names an argument it
// could not read as anything else.
func quotedArg(s string) string { return `"` + sanitised(s) + `"` }

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

// remoteSet attaches url as the account remote: it is reached first, in
// the user's environment, so that a URL git cannot reach changes nothing,
// exit code 3; then the account repo is opened, created when this is its
// first use, and the remote is recorded under the lock, see gitx.SetRemote;
// a URL other than the one set before forgets every remote-tracking branch
// the old one left; then its fork branches are fetched.
func (inv *invocation) remoteSet(ctx context.Context, raw string) error {
	if f := remoteURLRefusal(raw); f != nil {
		return f
	}
	if err := inv.git.ProbeRemote(ctx, raw); err != nil {
		return unreachableRemote(raw, err)
	}
	gitDir, _, err := gitx.OpenAccountRepo(ctx, inv.git, inv.dirs.Home)
	if err != nil {
		return accountRepoFailure(err)
	}
	remote := gitx.OriginRemote
	err = home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		previous, err := inv.git.RemoteURL(ctx, gitDir, remote)
		if err == nil {
			err = inv.git.SetRemote(ctx, gitDir, remote, raw)
		}
		if err == nil && previous != "" && previous != raw {
			err = inv.git.DropRemoteRefs(ctx, gitDir, remote)
		}
		if err != nil {
			return accountRepoFailure(err)
		}
		return nil
	})
	if err != nil {
		return mutationFailure(err)
	}
	if err := inv.fetchRemote(ctx, gitDir, remote, raw); err != nil {
		return err
	}
	tips, err := lineage.ListRemote(ctx, inv.git, gitDir, remote)
	if err != nil {
		return accountRepoFailure(err)
	}
	inv.out.emit(remoteEvent{event: newEvent("remote"), Name: remote, URL: raw, Fetch: gitx.ForkRefspec(remote)})
	holds := "; it holds " + plural(len(tips), "fork")
	inv.summary = "the account remote is now " + shownURL(raw) + holds
	inv.out.done("the account remote is now " + inv.out.paint(heading, shownURL(raw)) + holds)
	return nil
}

// remoteShow reports the account remote, or that none is set, which is no
// failure.
func (inv *invocation) remoteShow(ctx context.Context) error {
	entry, remote, ok, err := inv.accountSource(ctx)
	if err != nil {
		return err
	}
	if !ok {
		inv.out.emit(remoteEvent{event: newEvent("remote"), Name: gitx.OriginRemote})
		inv.summary = "no account remote is set"
		inv.out.print("No account remote is set. Attach one with ", inv.out.paint(label, "agentx remote set <url>"), ".")
		return nil
	}
	inv.out.emit(remoteEvent{event: newEvent("remote"), Name: remote, URL: entry.URL, Fetch: gitx.ForkRefspec(remote)})
	inv.summary = "the account remote is " + shownURL(entry.URL)
	inv.out.print(inv.out.paint(heading, remote), "  ", shownURL(entry.URL))
	return nil
}

// remoteUnset detaches the account remote under the lock, see
// gitx.UnsetRemote: local fork branches, their worktrees and every commit
// stay as they are, and nothing on the remote changes. With no remote set
// there is nothing to do, which is no failure.
func (inv *invocation) remoteUnset(ctx context.Context) error {
	entry, remote, ok, err := inv.accountSource(ctx)
	if err != nil {
		return err
	}
	if !ok {
		inv.out.emit(remoteEvent{event: newEvent("remote"), Name: gitx.OriginRemote})
		inv.summary = "no account remote is set"
		inv.out.print("No account remote is set; nothing to detach.")
		return nil
	}
	gitDir := gitx.AccountRepoPath(inv.dirs.Home)
	if err := home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		if err := inv.git.UnsetRemote(ctx, gitDir, remote); err != nil {
			return accountRepoFailure(err)
		}
		return nil
	}); err != nil {
		return mutationFailure(err)
	}
	inv.out.emit(remoteEvent{event: newEvent("remote"), Name: remote})
	u := shownURL(entry.URL)
	inv.summary = "the account remote " + u + " is no longer set; the forks of this machine are as they were"
	inv.out.done("the account remote " + inv.out.paint(heading, u) + " is no longer set; the forks of this machine are as they were")
	return nil
}

// accountSource is the account remote as this machine records it: its
// entry, and the name of its git remote in the account repo, which every
// command that reads, fetches or pushes the account remote's fork branches
// works with. ok is false when no account remote is set, or when this
// machine has no account repo at all.
func (inv *invocation) accountSource(ctx context.Context) (entry home.Source, remote string, ok bool, err error) {
	gitDir, exists, err := gitx.CheckAccountRepo(ctx, inv.git, inv.dirs.Home)
	if err != nil || !exists {
		return home.Source{}, "", false, accountRepoFailureOrNil(err)
	}
	return inv.accountSourceIn(ctx, gitDir)
}

// accountSourceIn is accountSource for a command that has read the
// account repo at gitDir already. The account remote is the account repo's
// remote origin, as agentx remote set records it, and its entry is read
// from there: the URL as the remote records it, as given, a fork layout and
// the account flag.
func (inv *invocation) accountSourceIn(ctx context.Context, gitDir string) (entry home.Source, remote string, ok bool, err error) {
	u, err := inv.git.RemoteURL(ctx, gitDir, gitx.OriginRemote)
	if err != nil {
		return home.Source{}, "", false, accountRepoFailure(err)
	}
	if u == "" {
		return home.Source{}, "", false, nil
	}
	return home.Source{URL: u, Layout: home.LayoutFork, Account: true}, gitx.OriginRemote, true, nil
}

// accountRepoFailureOrNil is accountRepoFailure for an error that may be nil.
func accountRepoFailureOrNil(err error) error {
	if err == nil {
		return nil
	}
	return accountRepoFailure(err)
}

// accountRemote is the account repo and the account remote, see
// accountSource, for a command that works with the remote, refusing, exit
// code 6, when no remote is set.
func (inv *invocation) accountRemote(ctx context.Context) (gitDir string, entry home.Source, remote string, err error) {
	entry, remote, ok, err := inv.accountSource(ctx)
	if err != nil {
		return "", home.Source{}, "", err
	}
	if !ok {
		return "", home.Source{}, "", noRemoteRefusal()
	}
	return gitx.AccountRepoPath(inv.dirs.Home), entry, remote, nil
}

// noRemoteRefusal refuses a command that works with the account remote on
// a machine that has none.
func noRemoteRefusal() *failure {
	return refuse(exitRefused, "no account remote is set", "run 'agentx remote set <url>' to attach a Git repository you own")
}

// fetchRemote fetches the fork branches of the account remote, the git
// remote called remote at url, see gitx.FetchRemote, outside the lock, and
// reports it as one progress event. A remote git cannot reach is exit code
// 3.
func (inv *invocation) fetchRemote(ctx context.Context, gitDir, remote, url string) error {
	if err := inv.git.FetchRemote(ctx, gitDir, remote); err != nil {
		return unreachableRemote(url, err)
	}
	inv.out.emit(progressEvent{event: newEvent("progress"), Phase: "fetch", Subject: shownURL(url), Current: 1, Total: 1})
	return nil
}

// unreachableRemote is a git that could not reach the account remote at
// url: exit code 3, as for a source.
func unreachableRemote(url string, err error) *failure {
	return refuse(exitSource, "cannot reach the account remote "+shownURL(url)+": "+trimGit(err.Error()),
		"check the URL and that you can reach it with git; a private repository needs a git credential helper (git config credential.helper) or an SSH key")
}
