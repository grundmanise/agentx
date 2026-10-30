package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
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

// docsURL is where the help sends a reader for more than a page can hold.
const docsURL = "https://docs.agentx.wtf"

// banner is the root page's title, as plain text rather than a raw string
// literal because the letters are drawn with backticks.
var banner = []string{
	"                                _",
	"  __ _    __ _    ___   _ __   | |_  __  __",
	" / _` |  / _` |  / _ \\ | '_ \\  | __| \\ \\/ /",
	"| (_| | | (_| | |  __/ | | | | | |_   >  <",
	" \\__,_|  \\__, |  \\___| |_| |_|  \\__| /_/\\_\\",
	"         |___/",
}

// Cobra sorts a command's children by name. The lists here print them in
// the order the AddCommand calls give, so a group reads as its workflow:
// add, new, fork, list, and so on.
func init() {
	cobra.EnableCommandSorting = false
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

// help renders a command's help: its description, then its usage with the
// examples. Cobra's default template is plain; this one paints section
// titles, command names and flag names on a terminal and prints the same
// text bare elsewhere.
func (inv *invocation) help(cmd *cobra.Command, _ []string) {
	out := cmd.OutOrStdout()
	k := inv.out.helpInk(out)
	switch {
	case !cmd.HasParent():
		for _, line := range banner {
			fmt.Fprintln(out, k.paint(label, line))
		}
		fmt.Fprintln(out)
		fmt.Fprintln(out, k.paint(heading, cmd.Short))
		fmt.Fprintln(out, k.paint(muted, docsURL))
	case cmd.Long != "":
		fmt.Fprintln(out, cmd.Long)
	case cmd.Short != "":
		fmt.Fprintln(out, cmd.Short)
	}
	fmt.Fprintln(out)
	inv.writeUsage(out, k, cmd, true)
}

// usage renders the usage block alone: what cobra prints after a flag error,
// or under an unknown help topic. It keeps the command list and leaves out
// the examples.
func (inv *invocation) usage(cmd *cobra.Command) error {
	out := cmd.OutOrStderr()
	inv.writeUsage(out, inv.out.helpInk(out), cmd, false)
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

// writeUsage prints the usage block: the usage lines, the aliases, the
// sectioned list of the commands below cmd, the examples when withExamples
// is set, the flags, and the footer that names the next page to read. The
// root lists the global flags in full; every other page names them on one
// line and points at the root.
func (inv *invocation) writeUsage(out io.Writer, k ink, cmd *cobra.Command, withExamples bool) {
	title := func(s string) { fmt.Fprintln(out, k.paint(heading, s)) }
	title("Usage:")
	if cmd.Runnable() && cmd.Annotations[annotationGroup] == "" {
		fmt.Fprintf(out, "  %s\n", cmd.UseLine())
	}
	if cmd.HasAvailableSubCommands() {
		fmt.Fprintf(out, "  %s\n", commandUsage(cmd))
	}
	if len(cmd.Aliases) > 0 {
		fmt.Fprintln(out)
		title("Aliases:")
		fmt.Fprintf(out, "  %s\n", cmd.NameAndAliases())
	}
	wrap := inv.out.columns(out)
	if cmd.HasAvailableSubCommands() {
		// The sections share one synopsis column, so the page reads as one list.
		var tables []*table
		synopsis := 0
		for _, s := range sections {
			rows := sectionRows(cmd, cmd, s.id)
			if len(rows.rows) == 0 {
				tables = append(tables, nil)
				continue
			}
			for _, row := range rows.rows {
				synopsis = max(synopsis, lipgloss.Width(row[0].text))
			}
			tables = append(tables, rows)
		}
		for i, rows := range tables {
			if rows == nil {
				continue
			}
			rows.minWidths = []int{synopsis}
			rows.wrap = wrap
			fmt.Fprintln(out)
			title(strings.ToUpper(sections[i].title))
			rows.render(out, k, "  ")
		}
	}
	if withExamples && cmd.HasExample() {
		fmt.Fprintln(out)
		title("Examples:")
		writeExamples(out, k, cmd.Example, wrap)
	}
	if !cmd.HasParent() {
		fmt.Fprintln(out)
		title("Global flags:")
		writeFlags(out, k, cmd.PersistentFlags(), wrap)
	} else {
		if hasShownFlags(cmd.LocalFlags()) {
			fmt.Fprintln(out)
			title("Flags:")
			writeFlags(out, k, cmd.LocalFlags(), wrap)
		}
		fmt.Fprintln(out)
		fmt.Fprintln(out, k.paint(muted, "Global flags: --json, --verbose, --color (see 'agentx --help')"))
	}
	if cmd.HasAvailableSubCommands() {
		fmt.Fprintln(out)
		next := k.paint(label, fmt.Sprintf("'%s <command> --help'", cmd.CommandPath()))
		fmt.Fprintf(out, "Run %s for the details, flags and examples of a command.\n", next)
	}
}

// commandUsage is the usage line of a command with subcommands. The root's
// names the global flags, which every command accepts.
func commandUsage(cmd *cobra.Command) string {
	if !cmd.HasParent() {
		return cmd.CommandPath() + " [--json] [--verbose] [--color on|off] <command> [arguments]"
	}
	return cmd.CommandPath() + " <command> [arguments]"
}

// sectionRows collects the rows of one section of page's command list: the
// runnable descendants of cmd that fall under it, in AddCommand order, each
// with its synopsis and its Short. A group is not a row, but its children
// are; a runnable command with children, machine, is a row and so are they.
func sectionRows(page, cmd *cobra.Command, id string) *table {
	t := &table{}
	for _, sub := range cmd.Commands() {
		if !sub.IsAvailableCommand() {
			continue
		}
		if sub.Runnable() && sub.Annotations[annotationGroup] == "" && helpSection(sub) == id {
			t.add(c(synopsis(page, sub), label), c(sub.Short, plain))
		}
		t.rows = append(t.rows, sectionRows(page, sub, id).rows...)
	}
	return t
}

// helpSection is the id of the section a command is listed under: its own
// annotation, else the nearest ancestor's, else the last section, so that
// a command nobody placed still has a row. TestHelpTreeIsComplete fails on
// a command that reaches the fallback.
func helpSection(cmd *cobra.Command) string {
	for c := cmd; c != nil; c = c.Parent() {
		if id := c.Annotations[annotationSection]; id != "" {
			return id
		}
	}
	return sections[len(sections)-1].id
}

// synopsis is the first column of a command's row on page: its path below
// the page's command, then its arguments, from annotationSynopsis when the
// Use line is too long for a row and from the Use line otherwise.
func synopsis(page, cmd *cobra.Command) string {
	name := strings.TrimPrefix(cmd.CommandPath(), page.CommandPath()+" ")
	args, ok := cmd.Annotations[annotationSynopsis]
	if !ok {
		args = useArgs(cmd.Use)
	}
	if args == "" {
		return name
	}
	return name + " " + args
}

// useArgs is the arguments of a Use line: what follows the command's name.
func useArgs(use string) string {
	_, args, _ := strings.Cut(strings.TrimSpace(use), " ")
	return strings.TrimSpace(args)
}

// writeExamples prints a command's Example text as two columns: the command
// line, painted as a name, and what it does, wrapped to wrap columns when
// that is not zero. A line without a tab prints as it is.
func writeExamples(out io.Writer, k ink, example string, wrap int) {
	t := &table{wrap: wrap}
	for _, line := range strings.Split(example, "\n") {
		command, description, ok := splitExample(line)
		if !ok {
			t.add(c(line, plain))
			continue
		}
		t.add(c(command, label), c(description, plain))
	}
	t.render(out, k, "  ")
}

// splitExample splits one line of an Example at its first tab, and reports
// false for a line without one.
func splitExample(line string) (command, description string, ok bool) {
	return strings.Cut(line, "\t")
}

// hasShownFlags reports whether writeFlags would print a row for flags:
// one that is not hidden and is not --help, which every page names in its
// footer or its own usage line.
func hasShownFlags(flags *pflag.FlagSet) bool {
	shown := false
	flags.VisitAll(func(f *pflag.Flag) {
		shown = shown || !f.Hidden && f.Name != "help"
	})
	return shown
}

// writeFlags lists flags the way pflag does, one per line, with the names
// painted: shorthand and name, the value's type where it takes one, the
// usage and the default when it is not the zero value, wrapped to wrap
// columns when that is not zero. The --help flag is left out: it is how the
// reader got here.
func writeFlags(out io.Writer, k ink, flags *pflag.FlagSet, wrap int) {
	t := &table{wrap: wrap}
	flags.VisitAll(func(f *pflag.Flag) {
		if f.Hidden || f.Name == "help" {
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
