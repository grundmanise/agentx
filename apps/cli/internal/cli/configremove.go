package cli

import (
	"bufio"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
)

// configRemovalEvent is one configuration config remove deleted.
type configRemovalEvent struct {
	event
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"` // the configuration directory
}

func newConfigRemoveCommand(inv *invocation) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:         "remove <configuration>...",
		Short:       "Delete the configuration directory of a client",
		Annotations: map[string]string{annotationSection: "settings", annotationSynopsis: "<id>..."},
		Long: "Delete the configuration directory of each client named, with everything in\n" +
			"it, after confirming, so that agentx no longer detects the client, and take\n" +
			"the client out of the settings.\n\n" +
			"A client whose configuration directory is the same one, or lies inside it, is\n" +
			"removed with it. The library stays as it is, and so does a skills directory\n" +
			"outside the configuration directory.\n\n" +
			"The command asks once before it changes anything. Pass --yes to remove without\n" +
			"asking, which is required whenever it cannot ask: with --json, or when either\n" +
			"stream is not a terminal.",
		Example: examples(
			"agentx config remove adal", "delete the configuration directory of a client, after confirming",
			"agentx config remove adal augment --yes", "delete two without asking, for a script",
		),
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return inv.configRemove(cmd, args, yes)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "delete the directories without asking")
	return cmd
}

// beneath reports whether path is dir or lies below it, as the two are
// spelled.
func beneath(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// neededIn names what agentx works from that deleting dir would take away,
// "" when nothing: the library, agentx home or the user's home directory,
// being dir or lying in it. Each is compared as it is spelled and as it
// really is, since a library that is a symlink into dir is spelled outside
// it. dir itself is resolved up to its last part only: a removal of a
// symlink unlinks it and deletes nothing it leads to.
func (inv *invocation) neededIn(dir string) string {
	real := dir
	if parent, err := filepath.EvalSymlinks(filepath.Dir(dir)); err == nil {
		real = filepath.Join(parent, filepath.Base(dir))
	}
	for _, p := range []struct{ path, what string }{
		{inv.dirs.Library, "the library"}, {inv.dirs.Home, "agentx home"}, {inv.dirs.User, "your home directory"},
	} {
		if beneath(filepath.Clean(p.path), dir) {
			return p.what
		}
		if resolved, err := filepath.EvalSymlinks(p.path); err == nil && beneath(resolved, real) {
			return p.what
		}
	}
	return ""
}

// configRemove deletes the configuration directory of each client named,
// after one confirmation, and takes every configuration that went with a
// directory out of the settings, as one journaled mutation. What a
// directory holds is the user's to judge: the command checks none of it.
func (inv *invocation) configRemove(cmd *cobra.Command, ids []string, yes bool) error {
	var named []string
	for _, id := range ids {
		if err := inv.detectedConfiguration(id); err != nil {
			return err
		}
		c, _ := scan.Lookup(id)
		named = append(named, filepath.Clean(c.ConfigDir(inv.dirs)))
	}
	// A directory named twice, or one inside another that goes, is one
	// removal: sorted, a directory comes before everything inside it.
	slices.Sort(named)
	var dirs []string
	for _, dir := range named {
		if !slices.ContainsFunc(dirs, func(outer string) bool { return beneath(dir, outer) }) {
			dirs = append(dirs, dir)
		}
	}
	for _, dir := range dirs {
		if what := inv.neededIn(dir); what != "" {
			return fail(exitRefused, quotedPath(dir)+" is or holds "+what+", so nothing was removed",
				"delete the directory yourself if it must go")
		}
	}
	// Every configuration kept in a directory that goes leaves with it.
	var events []configRemovalEvent
	for _, c := range scan.Detect(inv.dirs) {
		path := filepath.Clean(c.ConfigDir(inv.dirs))
		if slices.ContainsFunc(dirs, func(dir string) bool { return beneath(path, dir) }) {
			events = append(events, configRemovalEvent{event: newEvent("config_removal"), ID: c.Slug(), Name: c.Name(), Path: path})
		}
	}
	if err := inv.confirmRemoval(dirs, cmd.InOrStdin(), yes); err != nil {
		return err
	}
	ctx := cmd.Context()
	var settings home.Settings
	err := home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		edit, err := inv.beginSettings()
		if err != nil {
			return err
		}
		m := home.NewMutation(inv.dirs.Home)
		for _, dir := range dirs {
			state, err := home.State(dir)
			if err != nil {
				m.Discard()
				return err
			}
			m.Remove(dir, state)
		}
		for _, ev := range events {
			edit.dropConfiguration(ev.ID)
		}
		if err := edit.stage(m, inv.dirs.Home); err != nil {
			m.Discard()
			return err
		}
		if err := m.Apply(inv.refs(ctx)); err != nil {
			return err
		}
		settings = edit.settings()
		return nil
	})
	if err != nil {
		return mutationFailure(err)
	}
	paths := &table{}
	removed := make([]string, len(events))
	for i, ev := range events {
		inv.out.emit(ev)
		removed[i] = ev.ID
		paths.add(c(ev.ID, heading), c(quotedPath(ev.Path), muted))
	}
	inv.emitSettings(settings)
	inv.out.done("removed " + plural(len(events), "configuration"))
	inv.out.render(paths, "  ")
	inv.summary = "removed " + andList(removed)
	return nil
}

// confirmRemoval asks once before anything is deleted, as confirmImport
// asks before an import: without --yes the command asks, and where it
// cannot ask, in JSON mode or when either stream is no terminal, it
// refuses. Anything but yes is a refusal.
func (inv *invocation) confirmRemoval(dirs []string, in io.Reader, yes bool) error {
	if yes {
		return nil
	}
	quoted := make([]string, len(dirs))
	for i, dir := range dirs {
		quoted[i] = quotedPath(dir)
	}
	hint := "run it again with --yes to remove without asking"
	if inv.out.json || !isTerminal(in) || !isTerminal(inv.out.stdout) {
		return fail(exitRefused, "removing deletes "+andList(quoted)+", and there is no terminal to confirm at", hint)
	}
	return inv.askToRemove(quoted, in, hint)
}

// askToRemove puts the question, which names every directory that is
// deleted, and reads the answer as askToImport reads it.
func (inv *invocation) askToRemove(dirs []string, in io.Reader, hint string) error {
	them := "them"
	if len(dirs) == 1 {
		them = "it"
	}
	inv.out.prompt("Delete " + andList(dirs) + " with everything in " + them + "? [y/N] ")
	answer, _ := bufio.NewReader(in).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return nil
	}
	return fail(exitRefused, "the removal was declined: nothing was changed", hint)
}
