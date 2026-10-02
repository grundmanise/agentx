package cli

import (
	"context"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
)

// The remote commands are other names of the source commands for the
// account remote, see account.go, which existed before the account remote
// was a source.

func newRemoteCommand(inv *invocation) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remote",
		Short: "Attach, show or detach the account remote forks are published to",
		Long: "Attach, show or detach the account remote, the source of the fork layout forks are\n" +
			"published to. Each command is another name of a source command: remote set is\n" +
			"source add --account, remote show lists the account remote as source list does, and\n" +
			"remote unset is source remove of the account remote.",
		Annotations: map[string]string{annotationGroup: "true"},
		Args:        cobra.NoArgs,
		RunE:        needSubcommand(inv, "no remote command given", "run 'agentx remote --help' to list commands"),
	}
	cmd.AddCommand(newRemoteSetCommand(inv))
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

func newRemoteSetCommand(inv *invocation) *cobra.Command {
	var pushURL string
	cmd := &cobra.Command{
		Use:   "set <url>",
		Short: "Attach a Git repository you own as the account remote, and fetch its forks",
		Long: "Attach a Git repository you own as the account remote, then fetch the forks it\n" +
			"holds. The URL is any URL git reaches: https://, ssh://, git@host:path, file:// or\n" +
			"an absolute path. Credentials never go in it: use a git credential helper or an SSH\n" +
			"key. --push-url names the URL forks are fetched from and pushed to, such as the\n" +
			"SSH address of a repository you name by its HTTPS URL; --push-url= clears it. Only\n" +
			"fork branches travel to and from the remote, never managed skills, and setting\n" +
			"another URL forgets what the old one held. It is source add <url> --account.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return inv.remoteSet(cmd.Context(), args[0], pushChoice{set: cmd.Flags().Changed("push-url"), url: pushURL})
		},
	}
	cmd.Flags().StringVar(&pushURL, "push-url", "", "the URL forks are fetched from and pushed to, such as its SSH address; empty clears it")
	return cmd
}

// scpRemote is git's SSH shorthand, [user@]host:path, which has no scheme
// and whose host part holds no slash.
var scpRemote = regexp.MustCompile(`^([^@/:\s]+@)?[^@/:\s]+:[^\s]+$`)

// remoteURLRefusal is why url cannot be the account remote, nil when it
// can: it has to be a URL git reaches, a URL with a scheme, the SSH
// shorthand or an absolute path, which remote set has always asked for and
// still does, though source add --account takes every form source add
// does. A password or a token in it is refused rather than dropped, since
// what is left could reach another account or none, and a #ref is refused,
// since the remote holds every fork branch. Pure.
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

// remoteSet is source add <url> --account, see addAccount, with the rules
// remote set has always held its URL to, see remoteURLRefusal, first.
func (inv *invocation) remoteSet(ctx context.Context, raw string, push pushChoice) error {
	if f := remoteURLRefusal(raw); f != nil {
		return f
	}
	return inv.addAccount(ctx, raw, push)
}

// remoteShow reports the account remote as source list reports a source,
// or that none is set, which is no failure and emits no event: the app
// reads the sources of the snapshot instead.
func (inv *invocation) remoteShow(ctx context.Context) error {
	entry, remote, ok, err := inv.accountSource(ctx)
	if err != nil {
		return err
	}
	if !ok {
		inv.summary = "no account remote is set"
		inv.out.print("No account remote is set. Attach one with ", inv.out.paint(label, "agentx remote set <url>"), ".")
		return nil
	}
	n, fetched := 0, false
	if gitDir, exists, err := gitx.CheckAccountRepo(ctx, inv.git, inv.dirs.Home); err != nil {
		return accountRepoFailure(err)
	} else if exists {
		if n, fetched, err = inv.forkCount(ctx, gitDir, entry, remote); err != nil {
			return accountRepoFailure(err)
		}
	}
	ev := entryEvent(entry)
	if fetched {
		ev.Forks = &n
	}
	inv.out.emit(ev)
	inv.summary = "the account remote is " + shownURL(entry.URL)
	t := &table{}
	t.add(c(shownURL(entry.URL), heading), c(entry.LayoutName(), muted), accessWord(entry.Access), forksCell(n, fetched))
	inv.out.render(t, "")
	return nil
}

// remoteUnset detaches the account remote, see removeAccount. With no
// remote set there is nothing to do, which is no failure.
func (inv *invocation) remoteUnset(ctx context.Context) error {
	entry, remote, ok, err := inv.accountSource(ctx)
	if err != nil {
		return err
	}
	if !ok {
		inv.summary = "no account remote is set"
		inv.out.print("No account remote is set; nothing to detach.")
		return nil
	}
	return inv.removeAccount(ctx, entry, remote)
}

// accountRepoFailureOrNil is accountRepoFailure for an error that may be nil.
func accountRepoFailureOrNil(err error) error {
	if err == nil {
		return nil
	}
	return accountRepoFailure(err)
}
