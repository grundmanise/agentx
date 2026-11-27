package cli

import (
	"context"
	"errors"
	"strings"

	"github.com/spf13/cobra"
)

func newSkillShowCommand(inv *invocation) *cobra.Command {
	var configuration string
	cmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Read a skill's files, edits, update and history in one run",
		Long: "Read in one run all that this machine holds of a skill: every file with its\n" +
			"content, as 'agentx skill files' reads them; then, for a skill of the library,\n" +
			"its unpublished edits, as 'agentx skill diff <name>' shows them, what its update\n" +
			"would apply, as 'agentx skill diff <name> --update' shows it, and its history,\n" +
			"as 'agentx skill history <name>' lists it. A part the skill does not have is\n" +
			"left out: an unmanaged skill has no edits to compare, a skill with no update\n" +
			"known has none to show, and only a hub skill has a history. Nothing is written\n" +
			"and nothing reaches the network.\n\n" +
			"<name> and --configuration are those of 'agentx skill files'. A skill a plugin\n" +
			"provides and a skill of a configuration's own skills directory have files alone.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := configurationNamed(cmd, configuration); err != nil {
				return err
			}
			return inv.skillShow(cmd.Context(), args[0], configuration)
		},
	}
	configurationFlag(cmd, &configuration)
	return cmd
}

// skillShow reads the skill arg names as skill files reads it and, for a
// skill of the library, runs the three reads that tell the rest of it, each
// as its own command runs it, over one read of the lineage: its unpublished
// edits, see diffOf, what its update would apply, see diffUpdateOf, and its
// history, see historyOf. Each emits its own events and prints its own
// lines, and the result joins their summaries.
//
// A part its command refuses, exit code 6, is one the skill does not have,
// and is left out: an unmanaged skill has no base version, a skill with no
// update known has none, and a skill that is no hub skill has no history.
// Any other failure is the run's. The files are read under the shared lock
// and the rest without one, as the commands read them.
func (inv *invocation) skillShow(ctx context.Context, arg, configuration string) error {
	read, err := inv.readSkillFiles(ctx, arg, configuration)
	if err != nil {
		return err
	}
	inv.reportSkillFiles(read)
	if !read.library {
		return nil
	}
	gitDir, rec, held, err := inv.hubRecord(ctx, arg)
	if err != nil {
		return err
	}
	summary := []string{inv.summary}
	for _, part := range []func() error{
		func() error { return inv.diffOf(ctx, gitDir, rec, held, arg, "") },
		func() error { return inv.diffUpdateOf(ctx, gitDir, rec, held, arg) },
		func() error { return inv.historyOf(ctx, gitDir, rec, held, arg) },
	} {
		err := part()
		var f *failure
		if errors.As(err, &f) && f.status == exitRefused {
			inv.out.debugf("left out: %s", f.message)
			continue
		}
		if err != nil {
			return err
		}
		summary = append(summary, inv.summary)
	}
	inv.summary = strings.Join(summary, "; ")
	return nil
}
