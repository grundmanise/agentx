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

// annotationSection names the help section a runnable command is listed
// under, by the id of one of sections. A command without one is listed
// under its parent's section.
const annotationSection = "agentx.section"

// annotationSynopsis is the arguments a command list shows after the
// command's name, for a command whose Use line is too long for a row:
// "<source>" for skill add. The Use line stays on the command's own page.
const annotationSynopsis = "agentx.synopsis"

// section is one heading of the command list, on the root page and on the
// page of a group whose commands fall under it.
type section struct{ id, title string }

// sections is every heading, in the order the lists print them.
var sections = []section{
	{"skills", "Skills"},
	{"updates", "Updates and publishing"},
	{"sources", "Sources"},
	{"machines", "Machines"},
	{"settings", "Settings"},
	{"diagnostics", "Inventory and diagnostics"},
}

// examples builds a command's Example text from pairs of a command line
// and what it does: one example per line, the two parts separated by a
// tab, which the help renderer aligns as two columns.
func examples(pairs ...string) string {
	if len(pairs)%2 != 0 {
		panic("examples: a command line without a description")
	}
	var lines []string
	for i := 0; i < len(pairs); i += 2 {
		lines = append(lines, pairs[i]+"\t"+pairs[i+1])
	}
	return strings.Join(lines, "\n")
}

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
