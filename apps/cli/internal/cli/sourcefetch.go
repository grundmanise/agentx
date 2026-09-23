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

// progressEvent is one finished step of a run that does several, so that a
// caller can follow a long command. Subject names what the step was about
// and Current counts the steps done, this one included.
type progressEvent struct {
	event
	Phase   string `json:"phase"`
	Subject string `json:"subject,omitempty"`
	Current int    `json:"current"`
	Total   int    `json:"total,omitempty"` // absent when the run does not know it in advance
}

// fetchTarget is one source a fetch run covers: what to fetch, and the
// settings entry it was resolved from, which carries the pin and the alias
// the event repeats.
type fetchTarget struct {
	src   source.Source
	entry home.Source
}

func newSourceFetchCommand(inv *invocation) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "fetch [<url|id>...]",
		Short: "Fetch added sources again and report what moved",
		Long: "Fetch added sources again and report what moved. Name one or more sources by URL\n" +
			"or id, or pass --all to fetch every source of this machine. A path in a URL is\n" +
			"ignored: a fetch covers the whole source, whatever part of it you name.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return inv.sourceFetch(cmd.Context(), args, all) },
	}
	cmd.Flags().BoolVar(&all, "all", false, "fetch every source of this machine")
	return cmd
}

// sourceFetch re-fetches the named sources, or every one of them, in
// parallel and outside the lock, then records what moved in one settings
// write. It is the manual refresh of a source no skill was installed from,
// which nothing else updates.
func (inv *invocation) sourceFetch(ctx context.Context, args []string, all bool) error {
	targets, err := inv.sourcesToFetch(args, all)
	if err != nil {
		return err
	}
	if len(targets) == 0 { // --all on a machine that has no source
		inv.out.print("No sources. Add one with ", inv.out.paint(label, "agentx source add <url>"), ".")
		return nil
	}
	gitDir, exists, err := gitx.CheckAccountRepo(ctx, inv.git, inv.dirs.Home)
	if err != nil {
		return accountRepoFailure(err)
	}
	if !exists { // settings name sources the account repo has nothing of
		return sourceFailure(fmt.Errorf("%w: %s", source.ErrNotFetched, targets[0].src.URL), targets[0].src)
	}
	// The lock is taken before the network and released again: an unfinished
	// mutation journal is recovered, and a run that could not write the
	// settings at the end refuses now rather than after fetching everything.
	// The fetches themselves never hold it, so that a slow network cannot
	// block a scan.
	//
	// The remotes are brought back in line with the settings here too. The
	// settings hold the pin and the remote's refspec is derived from it, so
	// a run interrupted between the two leaves a remote recording a ref the
	// settings do not name. The refspecs are read in one git process and
	// only one that disagrees is written, which changes no pin: the pin is
	// what the settings say, and only source add sets it.
	if err := home.MutateQuiet(inv.dirs.Home, func() error { return inv.alignRemotes(ctx, gitDir, targets) }); err != nil {
		return accountRepoFailure(err)
	}
	srcs := make([]source.Source, len(targets))
	for i, t := range targets {
		srcs[i] = t.src
	}
	results := source.FetchAll(ctx, inv.git, gitDir, srcs, func(s source.Source, finished int) {
		inv.out.emit(progressEvent{event: newEvent("progress"), Phase: "fetch", Subject: s.URL, Current: finished, Total: len(srcs)})
	})
	// A source whose remote the account repo no longer holds, which an
	// interrupted removal leaves behind, cannot be fetched at all. git
	// reports that as a repository it cannot find and names the remote,
	// which is agentx's internal name for the source rather than anything
	// the user wrote; the truth is that the source is not on this machine,
	// which is what source skills says of the same state. Only a source
	// that failed is checked, so a run that works spawns nothing for it.
	for i, res := range results {
		if res.Err != nil && !source.Configured(ctx, inv.git, gitDir, res.Source.ID()) {
			results[i].Err = fmt.Errorf("%w: %s", source.ErrNotFetched, res.Source.URL)
		}
	}
	return inv.reportFetched(ctx, gitDir, targets, results)
}

// alignRemotes rewrites the fetch refspec of every target whose remote does
// not record the pin the settings hold. It runs under the lock, since git
// config fails rather than waits for its own lock file, and reads every
// remote in one git process, so a run whose remotes are right costs one.
//
// A source with no remote at all, which an interrupted removal leaves, is
// left alone: writing a refspec for it would build half a remote out of an
// entry nothing can fetch. The fetch that follows fails, and the run says
// what it is: a source this machine does not hold.
func (inv *invocation) alignRemotes(ctx context.Context, gitDir string, targets []fetchTarget) error {
	remotes := source.Remotes(ctx, inv.git, gitDir)
	for _, t := range targets {
		remote, ok := remotes[t.src.ID()]
		if !ok || remote.URL == "" || remote.Refspec == source.Refspec(t.src) {
			continue
		}
		if err := source.SetRefspec(ctx, inv.git, gitDir, t.src); err != nil {
			return err
		}
	}
	return nil
}

// reportFetched records the sources that fetched in one settings write,
// then reports every result in the order the command named them, so that
// the parallel work leaves no trace in the output. A source that failed is
// one warning naming it; the run then refuses, naming them all.
func (inv *invocation) reportFetched(ctx context.Context, gitDir string, targets []fetchTarget, results []source.Result) error {
	now := time.Now().UTC().Format(time.RFC3339)
	fetched := make(map[string]bool, len(results))
	var failed []source.Result
	for _, res := range results {
		if res.Err == nil {
			fetched[res.Source.URL] = true
		} else {
			failed = append(failed, res)
		}
	}
	if len(fetched) > 0 {
		// One write for the whole run, under the lock, on the settings as
		// they are now: a source removed while this run fetched is not
		// written back, and drops out of the report with it, so that
		// nothing says a source is present and fresh once it is gone.
		if err := home.Mutate(inv.dirs.Home, func() error {
			s, err := inv.loadSettings()
			if err != nil {
				return err
			}
			var removed []string
			for url := range fetched {
				if i := s.FindSource(url); i >= 0 {
					s.Sources[i].LastFetched = now
				} else {
					delete(fetched, url)
					removed = append(removed, url)
				}
			}
			// A fetch that finished after the removal put the source ref
			// back, since publishing a whole fetch is the last thing it
			// does. Take it away again under this same lock, so that a
			// removal a fetch raced still leaves nothing of the source.
			for _, url := range removed {
				if err := source.Remove(ctx, inv.git, gitDir, source.ID(url)); err != nil {
					return err
				}
			}
			return home.SaveSettings(inv.dirs.Home, s)
		}); err != nil {
			return err
		}
	}
	for i, res := range results {
		if res.Err != nil {
			message, _ := fetchFailure(res)
			inv.out.warn(message)
			continue
		}
		if !fetched[res.Source.URL] {
			continue // removed while this run fetched
		}
		entry, listing := targets[i].entry, res.Listing
		n := len(listing.Skills)
		inv.out.emit(sourceEvent{event: newEvent("source"), ID: res.Source.ID(), URL: res.Source.URL, Alias: entry.Alias, Pin: entry.Pin,
			LastFetched: now, Commit: listing.Commit, Previous: movedFrom(listing), Skills: &n})
		inv.out.done(inv.addLine(false, res.Source, listing) + ": " + inv.out.paint(noteStyle, plural(n, "skill")))
	}
	if len(failed) == 0 {
		return nil
	}
	return fetchRefusal(failed, len(results))
}

// fetchRefusal is how a run ends when a source it named could not be
// fetched. One run over several sources has one exit code, and it is the
// code every failure agrees on: fetching one source answers exactly as
// adding or listing that source would, and a run whose sources all failed
// the same way answers for that cause rather than flattening a vanished
// pin or a broken account repo into "unreachable". Only a run whose causes
// disagree, where no one code is true of them all, is the source-level
// refusal. Every failed source is named, so that the result event says
// which.
func fetchRefusal(failed []source.Result, total int) error {
	urls := make([]string, len(failed))
	for i, res := range failed {
		urls[i] = res.Source.URL
	}
	st := commonStatus(failed)
	if len(failed) == 1 {
		_, hint := fetchFailure(failed[0])
		return fail(st, "could not fetch "+urls[0], hint)
	}
	return fail(st, fmt.Sprintf("could not fetch %d of %s: %s", len(failed), plural(total, "source"), strings.Join(urls, ", ")),
		"the warnings name each failure; "+refusalHint(st))
}

// commonStatus is the exit status every failure of a run carries, and the
// source-level one when they carry more than one between them.
func commonStatus(failed []source.Result) status {
	var common status
	for i, res := range failed {
		var f *failure
		if !errors.As(sourceFailure(res.Err, res.Source), &f) {
			return exitSource // a lock or a recovery, which is not one source's
		}
		if i > 0 && f.status != common {
			return exitSource
		}
		common = f.status
	}
	return common
}

// refusalHint is what a run of several sources that failed the same way
// says to do about it, where a run over one repeats that source's own hint.
func refusalHint(st status) string {
	switch st {
	case exitNotFound:
		return "run 'agentx source list' to see what each source is pinned to and what this machine holds of it"
	case exitAccountRepo:
		return "run 'agentx doctor' and check the account repo it names"
	}
	return "check that you can reach every source and that a private one has a git credential helper (git config credential.helper) or an SSH key"
}

// fetchFailure is what one failed source says: the message and hint the
// exit code table would give it on its own, so that a warning names the
// same cause an add would have failed with, credential-free.
func fetchFailure(res source.Result) (message, hint string) {
	var f *failure
	if !errors.As(sourceFailure(res.Err, res.Source), &f) {
		return res.Source.URL + ": " + res.Err.Error(), ""
	}
	message = f.message
	if !strings.Contains(message, res.Source.URL) {
		// A local git failure is mapped by the exit code table alone, which
		// knows no URL. In a run over several sources a warning that names
		// none says nothing about which source it is about, and the refusal
		// promises that the warnings name each failure.
		message = res.Source.URL + ": " + message
	}
	return message, f.hint
}

// sourcesToFetch resolves the command line to the sources to fetch: every
// argument against the settings, or every source of the machine with
// --all. Each source is resolved before anything is fetched, so an
// argument that names none refuses the run before it touches the network.
func (inv *invocation) sourcesToFetch(args []string, all bool) ([]fetchTarget, error) {
	switch {
	case all && len(args) > 0:
		return nil, fail(exitUsage, "source fetch takes sources or --all, not both",
			"name the sources to fetch as <url|id>, or run 'agentx source fetch --all' to fetch every source")
	case !all && len(args) == 0:
		return nil, fail(exitUsage, "no source to fetch",
			"name one or more sources as <url|id>, or run 'agentx source fetch --all' to fetch every source")
	}
	s, err := inv.loadSettings()
	if err != nil {
		return nil, err
	}
	if all {
		targets := make([]fetchTarget, 0, len(s.Sources))
		for _, entry := range s.Sources {
			targets = append(targets, target(entry))
		}
		return targets, nil
	}
	var targets []fetchTarget
	seen := map[string]bool{}
	for _, arg := range args {
		src, entry, err := inv.findSource(arg)
		if err != nil {
			return nil, err
		}
		if src.Ref != "" && src.Ref != entry.Pin {
			return nil, pinMismatch(src, entry)
		}
		if seen[entry.URL] { // named twice, by URL and by id, or by two forms of one URL
			continue
		}
		seen[entry.URL] = true
		targets = append(targets, target(entry))
	}
	return targets, nil
}

// target is the fetch of one settings entry: the whole source at the pin
// the settings hold. A subpath the argument carried is dropped here, since
// a fetch covers the source whatever part of it was named.
func target(entry home.Source) fetchTarget {
	return fetchTarget{src: source.Source{URL: entry.URL, Ref: entry.Pin}, entry: entry}
}
