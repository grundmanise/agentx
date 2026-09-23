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
	Commit      string `json:"commit,omitempty"`          // the fetched commit; absent when the account repo holds no ref
	Previous    string `json:"previous_commit,omitempty"` // what the ref held before this fetch, when the fetch moved it
	Skills      *int   `json:"skills,omitempty"`          // how many skills the listing found; only after a listing
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
		Short:       "Add, fetch, list and remove the sources skills are installed from",
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
	cmd.AddCommand(newSourceFetchCommand(inv))
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

// takeBackWait bounds how long an add waits for the lock to take back the
// remote it wrote. Waiting at all is not optional: the run is undoing a
// write of its own, and giving up would leave a remote no source names.
// Waiting forever is not an option either, since the command is handed
// context.Background() and a lost lock would become a hang. Five seconds is
// generous for what it waits on: every competing holder is another agentx
// mutation, each holding the lock for one git config write or one settings
// write, so the bound covers a long queue of them and still ends the run.
const takeBackWait = 5 * time.Second

// leftBehind is the refusal of a run that wrote the remote of a source,
// could not record the source and could not take the remote back either. A
// remote the settings do not name is not a source: `source fetch` and
// `source skills` both answer from the settings, so nothing would ever
// name it again. The run keeps the exit code of whatever stopped it — a
// lost lock is still exit 7 — and says what stayed behind and how to clear
// it: adding the source again rewrites the remote and records it, which is
// the state this run failed to reach, and removing it by id takes it away.
func leftBehind(cause error, src source.Source) error {
	f := &failure{status: exitInternal, cause: cause}
	var already *failure
	switch {
	case errors.As(cause, &already):
		f.status = already.status
	case errors.Is(cause, home.ErrLocked):
		f.status = exitLocked
	case errors.Is(cause, home.ErrRecovery):
		f.status = exitRefused
	}
	f.message = cause.Error() + "; the remote " + source.RemoteName(src.ID()) + " was left in the account repo and no source names it"
	f.hint = "run 'agentx source add " + src.URL + "' to add the source and take the remote with it, or 'agentx source remove " + src.ID() + "' to clear it"
	return f
}

// sourceAdd fetches the source its argument names and records it in the
// settings.
func (inv *invocation) sourceAdd(ctx context.Context, arg string) error {
	src, err := inv.parseSource(arg)
	if err != nil {
		return err
	}
	_, _, err = inv.addSource(ctx, src)
	return err
}

// addSource is source add once its argument is parsed: it writes the
// source's remote, fetches it, records it in the settings as a mutation of
// its own and confirms it, then returns the listing of the fetch and the
// entry it wrote. skill add runs it too, for a source this machine does not
// have yet and for --fetch, so that the source is fetched once and the
// install reads the listing that fetch built.
func (inv *invocation) addSource(ctx context.Context, src source.Source) (listing source.Listing, entry home.Source, err error) {
	before, err := inv.loadSettings()
	if err != nil {
		return listing, entry, err
	}
	existing := before.FindSource(src.URL)
	gitDir, _, err := gitx.OpenAccountRepo(ctx, inv.git, inv.dirs.Home)
	if err != nil {
		return listing, entry, accountRepoFailure(err)
	}
	remote := source.RemoteName(src.ID())
	// revert takes back the remote this add is about to write, for a run
	// that gets no further: a source that was not there before goes
	// altogether, ref and all, and one that was goes back to the pin the
	// settings still hold, which is what they will still say when the next
	// command reads them. It writes the git config of the account repo, so
	// every caller holds the lock while it runs.
	revert := func() error {
		if existing < 0 { // nothing of a source that was never added is kept
			return source.Remove(ctx, inv.git, gitDir, src.ID())
		} // else the remote goes back to the pin the settings still hold
		return source.Configure(ctx, inv.git, gitDir, source.Source{URL: src.URL, Ref: before.Sources[existing].Pin})
	}
	// left answers for a take-back: cause, the failure that stopped the
	// run, when the remote went back, and the refusal that names what stayed
	// behind when it did not.
	left := func(cause, err error) error {
		if err == nil {
			return cause
		}
		inv.out.debugf("the remote %s could not be taken back: %v", remote, err)
		return leftBehind(cause, src)
	}
	// takeBack reverts under a lock this run is not holding, for a failure
	// that leaves the remote written and nothing naming it. It waits for
	// the lock rather than giving up on it: the run is cleaning up after
	// itself, and what it has to win is the very lock whose loss can be
	// what made it fail. The wait is bounded because the caller hands this
	// command context.Background().
	takeBack := func(cause error) error {
		inv.out.debugf("taking back the remote %s of %s: %v", remote, src.URL, cause)
		waiting, cancel := context.WithTimeout(ctx, takeBackWait)
		defer cancel()
		return left(cause, home.MutateQuietWaiting(waiting, inv.dirs.Home, inv.refs(ctx), revert))
	}
	// The remote is written under the lock: git config does not wait for its
	// own lock file, it fails, so two adds at once would otherwise leave a
	// remote half written. The fetch that follows runs outside the lock, so
	// that the network never blocks a scan. This hold may give up: a run
	// that loses it has written nothing and has nothing to take back.
	if err := home.MutateQuiet(inv.dirs.Home, inv.refs(ctx), func() error {
		return source.Configure(ctx, inv.git, gitDir, src)
	}); err != nil {
		return listing, entry, accountRepoFailure(err)
	}
	listing, err = source.Fetch(ctx, inv.git, gitDir, src)
	if err != nil {
		return listing, entry, takeBack(sourceFailure(err, src))
	}
	entry = home.Source{URL: src.URL, Pin: src.Ref, LastFetched: time.Now().UTC().Format(time.RFC3339)}
	// The remote went in under an earlier hold of the lock. Left behind by
	// a run that gets no further, it is a remote for a source the machine
	// does not know about, and since `source fetch` and `source skills`
	// both answer from the settings, nothing would ever name it again or
	// clean it up. A refusal cleans up after itself, as a failed fetch
	// already does.
	//
	// settled says the body below ran, so what becomes of the remote is
	// decided already: either the entry is written and the remote belongs
	// to it, or the body took the remote back under the lock it was holding
	// anyway. Taking it back there rather than afterwards is what makes the
	// cleanup free, since the lock this run has is the lock it would
	// otherwise have to wait for.
	//
	// Only a run whose body never ran — the lock was never won, or an
	// earlier mutation's recovery refused first — has a remote left to take
	// back out here, and that one has to wait for the lock to do it.
	added, settled := true, false
	err = home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		settled = true
		s, err := inv.loadSettings()
		if err != nil {
			return left(err, revert())
		}
		if i := s.FindSource(src.URL); i >= 0 {
			added = false
			entry.Alias = s.Sources[i].Alias // unused so far; carried, never dropped
		}
		s.SetSource(entry)
		if err := home.SaveSettings(inv.dirs.Home, s); err != nil {
			return left(err, revert())
		}
		return nil
	})
	if err != nil {
		if !settled {
			return listing, entry, takeBack(err)
		}
		return listing, entry, err
	}
	n := len(listing.Skills)
	inv.out.emit(sourceEvent{event: newEvent("source"), ID: src.ID(), URL: src.URL, Alias: entry.Alias, Pin: src.Ref, Subpath: src.Subpath,
		LastFetched: entry.LastFetched, Commit: listing.Commit, Previous: movedFrom(listing), Skills: &n})
	inv.out.done(inv.addLine(added, src, listing) + ": " + inv.out.paint(noteStyle, plural(n, "skill")) + under(inv.out, src.Subpath))
	return listing, entry, nil
}

// movedFrom is the previous_commit an event carries: what the source ref
// held before this fetch, and nothing when the fetch left it where it was
// or when the ref held nothing before, so that a caller can tell a real
// change from a no-op by the field's presence alone.
func movedFrom(listing source.Listing) string {
	if listing.Previous == "" || listing.Previous == listing.Commit {
		return ""
	}
	return listing.Previous
}

// addLine is the confirmation of source add, and of every source of a
// source fetch, which is a repeat by definition. A first add says what was
// added; a repeat says what moved, since that is the only thing the command
// can tell the reader that they did not already know.
func (inv *invocation) addLine(added bool, src source.Source, listing source.Listing) string {
	name := inv.out.paint(heading, src.URL) + pinned(inv.out, src.Ref)
	if added {
		return "added " + name + " at " + short(listing.Commit)
	}
	if listing.Previous == listing.Commit {
		return "re-fetched " + name + ", already at " + short(listing.Commit)
	}
	if listing.Previous == "" {
		return "re-fetched " + name + ", now at " + short(listing.Commit)
	}
	return "re-fetched " + name + ", now at " + short(listing.Commit) + ", was " + short(listing.Previous)
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
			return accountRepoFailure(err) // a local git failure in a source command is exit code 8, hint and all
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
		return pinMismatch(src, entry)
	}
	gitDir, exists, err := gitx.CheckAccountRepo(ctx, inv.git, inv.dirs.Home)
	if err != nil {
		return accountRepoFailure(err)
	}
	// Refused as not fetched, the source is named with the pin the settings
	// hold, which a bare URL argument leaves out.
	pinned := src
	pinned.Ref = entry.Pin
	if !exists {
		return sourceFailure(fmt.Errorf("%w: %s", source.ErrNotFetched, src.URL), pinned)
	}
	listing, err := source.List(ctx, inv.git, gitDir, src)
	if err != nil {
		return sourceFailure(err, pinned)
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
		// The name, the description and the directory names the subpath is
		// built from all come from the source's own repository: sanitised,
		// so that a row stays a row and no escape sequence reaches the
		// terminal. The event above carries them as the source wrote them.
		t.add(c("  "+sanitised(sk.Name), label), c(sanitised(subpath), muted), c(sanitised(sk.Description), plain))
	}
	out.render(t, "")
	return nil
}

// sourceRemove deletes the remote, the ref and the settings entry. The git
// state goes first: if it failed after the entry was gone, nothing would
// name the source any more and its remote would be unreachable for good.
func (inv *invocation) sourceRemove(ctx context.Context, arg string) error {
	id, url, err := inv.sourceToRemove(ctx, arg)
	if err != nil {
		return err
	}
	err = home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		gitDir, exists, err := gitx.CheckAccountRepo(ctx, inv.git, inv.dirs.Home)
		if err != nil {
			return accountRepoFailure(err)
		}
		if exists {
			if err := source.Remove(ctx, inv.git, gitDir, id); err != nil {
				return accountRepoFailure(err)
			}
		}
		s, err := inv.loadSettings()
		if err != nil {
			return err
		}
		s.RemoveSource(url)
		return home.SaveSettings(inv.dirs.Home, s)
	})
	if err != nil {
		return err
	}
	inv.out.emit(sourceEvent{event: newEvent("source"), ID: id, URL: url})
	what := url
	if what == "" {
		what = id
	}
	inv.out.done("removed " + inv.out.paint(heading, what))
	return nil
}

// sourceToRemove resolves what remove was asked to delete: the settings
// entry when there is one, else whatever the account repo still holds under
// that id, since an add cut short before its settings write leaves a remote
// and a ref that only remove can clean up.
func (inv *invocation) sourceToRemove(ctx context.Context, arg string) (id, url string, err error) {
	src, entry, findErr := inv.findSource(arg)
	if findErr == nil {
		return source.ID(entry.URL), entry.URL, nil
	}
	if !errors.Is(findErr, errNotAdded) {
		return "", "", findErr
	}
	if source.IsID(arg) {
		id = arg
	} else {
		id, url = src.ID(), src.URL
	}
	gitDir, exists, err := gitx.CheckAccountRepo(ctx, inv.git, inv.dirs.Home)
	if err != nil {
		return "", "", accountRepoFailure(err)
	}
	if exists && source.Present(ctx, inv.git, gitDir, id) {
		return id, url, nil
	}
	return "", "", findErr
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
		return source.Source{}, home.Source{}, fail(exitNotFound, "no source with id "+arg, "run 'agentx source list' to see the sources").(*failure).wrap(errNotAdded)
	}
	src, err := inv.parseSource(arg)
	if err != nil {
		return src, home.Source{}, err
	}
	if i := s.FindSource(src.URL); i >= 0 {
		return src, s.Sources[i], nil
	}
	return src, home.Source{}, fail(exitNotFound, "no source "+src.URL, "run 'agentx source add "+src.URL+"' to add it").(*failure).wrap(errNotAdded)
}

// errNotAdded marks the refusal of a source that has no settings entry, so
// that remove can still reach what the account repo holds under its id.
var errNotAdded = errors.New("not added")

// pinMismatch refuses an argument whose #ref is not the pin the settings
// hold. Listing and fetching both answer for the stored pin, so naming
// another ref asks for something the command does not do; changing a pin
// is what source add is for.
func pinMismatch(src source.Source, entry home.Source) error {
	return fail(exitUsage, fmt.Sprintf("%s is pinned to %q, not %q", src.URL, entry.Pin, src.Ref),
		"run 'agentx source add "+src.URL+"#"+src.Ref+"' to change the pin")
}

// sourceFailure maps a source package error to the exit code table.
func sourceFailure(err error, src source.Source) error {
	msg := err.Error()
	switch {
	case errors.Is(err, source.ErrRefNotFound):
		what := fmt.Sprintf("%s has no ref %q", src.URL, src.Ref)
		if src.Ref == "" {
			what = src.URL + " has no branch to follow"
		}
		return fail(exitNotFound, what+": "+trimGit(msg), "pin a branch, tag or commit that exists in the source")
	case errors.Is(err, source.ErrIncomplete):
		// source fetch, not source add: the source is already on this
		// machine with its pin, and fetching it again is what fills in what
		// is missing. An add would also rewrite the pin to whatever this
		// argument happened to say.
		return fail(exitNotFound, fmt.Sprintf("%s: %s", src.URL, msg), "run 'agentx source fetch "+src.URL+"' to fetch it again")
	case errors.Is(err, source.ErrNoSubpath):
		return fail(exitNotFound, fmt.Sprintf("%s: %s", src.URL, msg), "name a directory of the repository")
	case errors.Is(err, source.ErrNotFetched):
		// The source is in the settings, which an import leaves without
		// anything fetched, and src.Ref is the pin they hold. source add
		// writes the pin its argument names, so the bare URL would unpin it.
		return fail(exitNotFound, msg, "run 'agentx source add "+sourceAddArg(src.URL, src.Ref)+"' to fetch it")
	case errors.Is(err, source.ErrUnreachable):
		hint := "check the URL and that you can reach it; a private repository needs a git credential helper (git config credential.helper) or an SSH key"
		if src.Stripped {
			hint = "the user or token in the URL was dropped and agentx never stores credentials: put them in a git credential helper (git config credential.helper) and add the source again"
		}
		return fail(exitSource, fmt.Sprintf("%s: %s", src.URL, trimGit(msg)), hint)
	}
	// What is left is the account repo's own git failing on a local read.
	return accountRepoFailure(err)
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
