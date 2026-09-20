package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// annotationGroup marks a command that only groups subcommands: its own
// RunE shows help, so its usage names no flags of its own.
const annotationGroup = "agentx.group"

// help renders a command's help: its description, then its usage. Cobra's
// default template is plain; this one paints section titles, command names
// and flag names on a terminal and prints the same text bare elsewhere.
func (inv *invocation) help(cmd *cobra.Command, _ []string) {
	out := cmd.OutOrStdout()
	k := inv.out.helpInk(out)
	if cmd.Long != "" {
		fmt.Fprintln(out, cmd.Long)
	} else if cmd.Short != "" {
		fmt.Fprintln(out, cmd.Short)
	}
	fmt.Fprintln(out)
	inv.writeUsage(out, k, cmd)
}

// usage renders the usage block alone: what cobra prints after a flag error.
func (inv *invocation) usage(cmd *cobra.Command) error {
	out := cmd.OutOrStderr()
	inv.writeUsage(out, inv.out.helpInk(out), cmd)
	return nil
}

// helpInk is the ink for help text, which cobra sends to stdout or, in JSON
// mode, to stderr.
func (w *writer) helpInk(out io.Writer) ink {
	if out == w.stderr {
		return w.err()
	}
	return w.out()
}

func (inv *invocation) writeUsage(out io.Writer, k ink, cmd *cobra.Command) {
	title := func(s string) { fmt.Fprintln(out, k.paint(heading, s)) }
	title("Usage:")
	if cmd.Runnable() && cmd.Annotations[annotationGroup] == "" {
		fmt.Fprintf(out, "  %s\n", cmd.UseLine())
	}
	if cmd.HasAvailableSubCommands() {
		fmt.Fprintf(out, "  %s [command]\n", cmd.CommandPath())
	}
	if len(cmd.Aliases) > 0 {
		fmt.Fprintln(out)
		title("Aliases:")
		fmt.Fprintf(out, "  %s\n", cmd.NameAndAliases())
	}
	if cmd.HasExample() {
		fmt.Fprintln(out)
		title("Examples:")
		fmt.Fprintln(out, cmd.Example)
	}
	if cmd.HasAvailableSubCommands() {
		fmt.Fprintln(out)
		title("Commands:")
		t := &table{}
		for _, sub := range cmd.Commands() {
			if sub.IsAvailableCommand() || sub.Name() == "help" {
				t.add(c(sub.Name(), label), c(sub.Short, plain))
			}
		}
		t.render(out, k, "  ")
	}
	if cmd.HasAvailableLocalFlags() {
		fmt.Fprintln(out)
		title("Flags:")
		writeFlags(out, k, cmd.LocalFlags())
	}
	if cmd.HasAvailableInheritedFlags() {
		fmt.Fprintln(out)
		title("Global flags:")
		writeFlags(out, k, cmd.InheritedFlags())
	}
	if cmd.HasAvailableSubCommands() {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "Use %s for more information about a command.\n", k.paint(label, fmt.Sprintf("%q", cmd.CommandPath()+" [command] --help")))
	}
}

// writeFlags lists flags the way pflag does, one per line, with the names
// painted: shorthand and name, the value's type where it takes one, the
// usage and the default when it is not the zero value.
func writeFlags(out io.Writer, k ink, flags *pflag.FlagSet) {
	t := &table{}
	flags.VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		names := "    --" + f.Name
		if f.Shorthand != "" {
			names = "-" + f.Shorthand + ", --" + f.Name
		}
		varname, usage := pflag.UnquoteUsage(f)
		if varname != "" {
			names += " " + varname
		}
		if f.DefValue != "" && f.DefValue != "false" && f.DefValue != "0" && f.DefValue != "[]" {
			usage += fmt.Sprintf(" (default %s)", f.DefValue)
		}
		t.add(c(names, label), c(strings.TrimSpace(usage), plain))
	})
	t.render(out, k, "  ")
}
