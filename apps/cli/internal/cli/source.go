package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// sourceEvent is one source: its settings entry plus what the account repo
// holds of it.
type sourceEvent struct {
	event
	ID          string `json:"id"`
	URL         string `json:"url"`
	Alias       string `json:"alias,omitempty"`
	Pin         string `json:"pin,omitempty"`
	Subpath     string `json:"subpath,omitempty"` // the scope of this listing, not stored
	LastFetched string `json:"last_fetched,omitempty"`
	Commit      string `json:"commit,omitempty"` // the fetched commit; absent when the account repo holds no ref
	Skills      *int   `json:"skills,omitempty"` // how many skills the listing found; only after a listing
}

// sourceSkillEvent is one installable skill of a source.
type sourceSkillEvent struct {
	event
	Source      string `json:"source"` // the source id
	Subpath     string `json:"subpath"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Tree        string `json:"tree"`
}

func newSourceCommand(inv *invocation) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "source",
		Short:       "Add, list and remove the sources skills are installed from",
		Annotations: map[string]string{annotationGroup: "true"},
		Args:        cobra.NoArgs,
		RunE:        needSubcommand(inv, "no source command given", "run 'agentx source --help' to list commands"),
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "add <url>",
		Short: "Fetch a git repository and add it as a source",
		Long: "Fetch a git repository and add it as a source. The URL is owner/repo or owner/repo/subpath\n" +
			"for GitHub, a GitHub or GitLab URL with an optional tree path, an SSH URL or a file:// URL,\n" +
			"any of them with #ref to pin a branch or tag. A user or token in the URL is dropped.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return inv.sourceAdd(cmd.Context(), args[0]) },
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List the sources of this machine",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return inv.sourceList(cmd.Context()) },
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "skills <url|id>",
		Short: "List the installable skills of a source",
		Args:  cobra.ExactArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return inv.sourceSkills(cmd.Context(), args[0]) },
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "remove <url|id>",
		Short: "Remove a source and what was fetched from it",
		Args:  cobra.ExactArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return inv.sourceRemove(cmd.Context(), args[0]) },
	})
	return cmd
}

// parseSource turns a command argument into a Source, reporting a dropped
// credential once.
func (inv *invocation) parseSource(arg string) (source.Source, error) {
	src, err := source.Parse(arg)
	if err != nil {
		return src, fail(exitUsage, err.Error(), "accepted forms: "+source.Forms)
	}
	if src.Stripped {
		inv.out.warn("the user or token in the URL was dropped: agentx never stores credentials; put them in a git credential helper (git config credential.helper) or an SSH key")
	}
	return src, nil
}

// sourceAdd fetches the source and records it in the settings.
func (inv *invocation) sourceAdd(ctx context.Context, arg string) error {
	src, err := inv.parseSource(arg)
	if err != nil {
		return err
	}
	before, err := inv.loadSettings()
	if err != nil {
		return err
	}
	existing := before.FindSource(src.URL)
	gitDir, _, err := gitx.OpenAccountRepo(ctx, inv.git, inv.dirs.Home)
	if err != nil {
		return accountRepoFailure(err)
	}
	// The network runs outside the lock, so that a slow fetch never blocks a
	// scan; git serialises the ref and config writes on its own.
	if err := source.Configure(ctx, inv.git, gitDir, src); err != nil {
		return err
	}
	listing, err := source.Fetch(ctx, inv.git, gitDir, src)
	if err != nil {
		if existing < 0 { // nothing of a source that was never added is kept
			_ = source.Remove(ctx, inv.git, gitDir, src.ID())
		} else { // the remote follows the pin the settings still hold
			_ = source.Configure(ctx, inv.git, gitDir, source.Source{URL: src.URL, Ref: before.Sources[existing].Pin})
		}
		return sourceFailure(err, src)
	}
	entry := home.Source{URL: src.URL, Pin: src.Ref, LastFetched: time.Now().UTC().Format(time.RFC3339)}
	added := true
	err = home.Mutate(inv.dirs.Home, func() error {
		s, err := inv.loadSettings()
		if err != nil {
			return err
		}
		if i := s.FindSource(src.URL); i >= 0 {
			added = false
			entry.Alias = s.Sources[i].Alias // unused so far; carried, never dropped
		}
		s.SetSource(entry)
		return home.SaveSettings(inv.dirs.Home, s)
	})
	if err != nil {
		return err
	}
	n := len(listing.Skills)
	inv.out.emit(sourceEvent{event: newEvent("source"), ID: src.ID(), URL: src.URL, Alias: entry.Alias, Pin: src.Ref, Subpath: src.Subpath,
		LastFetched: entry.LastFetched, Commit: listing.Commit, Skills: &n})
	verb := "added"
	if !added {
		verb = "fetched"
	}
	inv.out.done(verb + " " + inv.out.paint(heading, src.URL) + pinned(inv.out, src.Ref) + " at " + short(listing.Commit) + ": " + inv.out.paint(noteStyle, plural(n, "skill")) + under(inv.out, src.Subpath))
	return nil
}

// sourceList reports every source in the settings with its fetched commit.
func (inv *invocation) sourceList(ctx context.Context) error {
	s, err := inv.loadSettings()
	if err != nil {
		return err
	}
	commits := map[string]string{}
	if gitDir, exists, err := gitx.CheckAccountRepo(ctx, inv.git, inv.dirs.Home); err != nil {
		return accountRepoFailure(err)
	} else if exists {
		if commits, err = source.Commits(ctx, inv.git, gitDir); err != nil {
			return err
		}
	}
	out := inv.out
	if len(s.Sources) == 0 {
		out.print("No sources. Add one with ", out.paint(label, "agentx source add <url>"), ".")
		return nil
	}
	out.print(out.paint(heading, plural(len(s.Sources), "source")))
	t := &table{}
	for _, src := range s.Sources {
		id := source.ID(src.URL)
		out.emit(sourceEvent{event: newEvent("source"), ID: id, URL: src.URL, Alias: src.Alias, Pin: src.Pin, LastFetched: src.LastFetched, Commit: commits[id]})
		pin := c("(unpinned)", muted)
		if src.Pin != "" {
			pin = c(src.Pin, plain)
		}
		commit := c("not fetched", warnStyle)
		if commits[id] != "" {
			commit = c(short(commits[id]), muted)
		}
		t.add(c("  "+src.URL, heading), pin, commit, c(src.LastFetched, muted), c(id, label))
	}
	out.render(t, "")
	return nil
}

// sourceSkills lists the skills of a fetched source under the subpath of
// the argument, from the account repo alone.
func (inv *invocation) sourceSkills(ctx context.Context, arg string) error {
	src, entry, err := inv.findSource(arg)
	if err != nil {
		return err
	}
	if src.Ref != "" && src.Ref != entry.Pin {
		return fail(exitUsage, fmt.Sprintf("%s is pinned to %q, not %q", src.URL, entry.Pin, src.Ref), "run 'agentx source add "+src.URL+"#"+src.Ref+"' to change the pin")
	}
	gitDir, exists, err := gitx.CheckAccountRepo(ctx, inv.git, inv.dirs.Home)
	if err != nil {
		return accountRepoFailure(err)
	}
	if !exists {
		return sourceFailure(fmt.Errorf("%w: %s", source.ErrNotFetched, src.URL), src)
	}
	listing, err := source.List(ctx, inv.git, gitDir, src)
	if err != nil {
		return sourceFailure(err, src)
	}
	out := inv.out
	n := len(listing.Skills)
	id := src.ID()
	out.emit(sourceEvent{event: newEvent("source"), ID: id, URL: src.URL, Alias: entry.Alias, Pin: entry.Pin, Subpath: src.Subpath,
		LastFetched: entry.LastFetched, Commit: listing.Commit, Skills: &n})
	out.print(out.paint(heading, plural(n, "skill")), " in ", out.paint(heading, src.URL), under(out, src.Subpath), " at ", short(listing.Commit))
	t := &table{}
	for _, sk := range listing.Skills {
		out.emit(sourceSkillEvent{event: newEvent("source_skill"), Source: id, Subpath: sk.Subpath, Name: sk.Name, Description: sk.Description, Tree: sk.Tree})
		subpath := sk.Subpath
		if subpath == "" {
			subpath = "."
		}
		t.add(c("  "+sk.Name, label), c(subpath, muted), c(sk.Description, plain))
	}
	out.render(t, "")
	return nil
}

// sourceRemove deletes the settings entry, the remote and the ref.
func (inv *invocation) sourceRemove(ctx context.Context, arg string) error {
	src, _, err := inv.findSource(arg)
	if err != nil {
		return err
	}
	err = home.Mutate(inv.dirs.Home, func() error {
		s, err := inv.loadSettings()
		if err != nil {
			return err
		}
		s.RemoveSource(src.URL)
		if err := home.SaveSettings(inv.dirs.Home, s); err != nil {
			return err
		}
		gitDir, exists, err := gitx.CheckAccountRepo(ctx, inv.git, inv.dirs.Home)
		if err != nil {
			return accountRepoFailure(err)
		}
		if !exists {
			return nil
		}
		return source.Remove(ctx, inv.git, gitDir, src.ID())
	})
	if err != nil {
		return err
	}
	inv.out.emit(sourceEvent{event: newEvent("source"), ID: src.ID(), URL: src.URL})
	inv.out.done("removed " + inv.out.paint(heading, src.URL))
	return nil
}

// findSource resolves a source id or URL to its settings entry. An id must
// match an entry; a URL is parsed and may carry a subpath. Neither being
// known is exit 5.
func (inv *invocation) findSource(arg string) (source.Source, home.Source, error) {
	s, err := inv.loadSettings()
	if err != nil {
		return source.Source{}, home.Source{}, err
	}
	if source.IsID(arg) {
		for _, entry := range s.Sources {
			if source.ID(entry.URL) == arg {
				return source.Source{URL: entry.URL}, entry, nil
			}
		}
		return source.Source{}, home.Source{}, fail(exitNotFound, "no source with id "+arg, "run 'agentx source list' to see the sources")
	}
	src, err := inv.parseSource(arg)
	if err != nil {
		return src, home.Source{}, err
	}
	if i := s.FindSource(src.URL); i >= 0 {
		return src, s.Sources[i], nil
	}
	return src, home.Source{}, fail(exitNotFound, "no source "+src.URL, "run 'agentx source add "+src.URL+"' to add it")
}

// sourceFailure maps a source package error to the exit code table.
func sourceFailure(err error, src source.Source) error {
	msg := err.Error()
	switch {
	case errors.Is(err, source.ErrRefNotFound):
		return fail(exitNotFound, fmt.Sprintf("%s has no ref %q: %s", src.URL, src.Ref, trimGit(msg)), "pin a branch, tag or commit that exists in the source")
	case errors.Is(err, source.ErrNoSubpath):
		return fail(exitNotFound, fmt.Sprintf("%s: %s", src.URL, msg), "name a directory of the repository")
	case errors.Is(err, source.ErrNotFetched):
		return fail(exitNotFound, msg, "run 'agentx source add "+src.URL+"' to fetch it")
	case errors.Is(err, source.ErrUnreachable):
		hint := "check the URL and that you can reach it; a private repository needs a git credential helper (git config credential.helper) or an SSH key"
		if src.Stripped {
			hint = "the user or token in the URL was dropped and agentx never stores credentials: put them in a git credential helper (git config credential.helper) and add the source again"
		}
		return fail(exitSource, fmt.Sprintf("%s: %s", src.URL, trimGit(msg)), hint)
	}
	return err
}

// trimGit keeps the first line of a git error after the source package's
// prefix, which is what names the cause.
func trimGit(msg string) string {
	if _, rest, ok := strings.Cut(msg, ": git "); ok {
		msg = "git " + rest
	}
	line, _, _ := strings.Cut(msg, "\n")
	return line
}

// accountRepoFailure is exit 8 for an account repo git cannot read.
func accountRepoFailure(err error) error {
	if errors.Is(err, home.ErrLocked) || errors.Is(err, home.ErrRecovery) {
		return err
	}
	return fail(exitAccountRepo, err.Error(), "run 'agentx doctor' and check the account repo it names")
}

func short(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}

func pinned(out *writer, ref string) string {
	if ref == "" {
		return ""
	}
	return " pinned to " + out.paint(label, ref)
}

func under(out *writer, subpath string) string {
	if subpath == "" {
		return ""
	}
	return " under " + out.paint(muted, subpath)
}
