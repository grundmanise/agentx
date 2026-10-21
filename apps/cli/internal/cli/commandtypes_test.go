package cli

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// commandTypesFile is where the desktop app reads the commands the CLI
// takes from. It is generated from the command tree, so that a command line
// the app builds can be checked against what the CLI parses: the app's
// tests run against a mock of the CLI, which rejects a command line this
// file does not admit.
const commandTypesFile = "../../../desktop/src/cli/commands.gen.ts"

// probedArgs is how many arguments a command's argument check is tried
// with; a command that takes this many takes any number.
const probedArgs = 4

// cliCommand is one command the app can run: how many arguments it takes
// and its flags, each true when it takes a value.
type cliCommand struct {
	minArgs, maxArgs int // maxArgs is -1 for any number
	flags            map[string]bool
}

// flagValues gives each flag of set by name, true when it takes a value.
// A flag whose value is optional has no form the app's check knows.
func flagValues(set *pflag.FlagSet, into map[string]bool) error {
	var err error
	set.VisitAll(func(f *pflag.Flag) {
		isBool := f.Value.Type() == "bool"
		if f.NoOptDefVal != "" && !isBool {
			err = fmt.Errorf("--%s takes an optional value, which the app's check does not know", f.Name)
		}
		into[f.Name] = !isBool
	})
	return err
}

// argRange probes the argument check of cmd for the fewest and the most
// arguments it takes. A command without a check takes any number, as cobra
// lets a command with no subcommands do.
func argRange(cmd *cobra.Command) (low, high int, err error) {
	low, high = -1, -1
	for n := 0; n <= probedArgs; n++ {
		args := slices.Repeat([]string{"x"}, n)
		if cmd.Args != nil && cmd.Args(cmd, args) != nil {
			if low >= 0 && high < 0 {
				high = n - 1
			}
			continue
		}
		if low < 0 {
			low = n
		} else if high >= 0 {
			return 0, 0, fmt.Errorf("%s takes %d arguments but not %d", cmd.CommandPath(), n, high+1)
		}
	}
	if low < 0 {
		return 0, 0, fmt.Errorf("%s takes no number of arguments up to %d", cmd.CommandPath(), probedArgs)
	}
	return low, high, nil
}

// commandTree is every command of root that runs something, by its words
// after the root's name, and the flags every command takes. A command
// marked as a group only groups its subcommands; one that has subcommands
// and is no group, as agentx machine, runs something too.
func commandTree(root *cobra.Command) (map[string]cliCommand, map[string]bool, error) {
	global := map[string]bool{}
	if err := flagValues(root.PersistentFlags(), global); err != nil {
		return nil, nil, err
	}
	commands := map[string]cliCommand{}
	var walk func(cmd *cobra.Command) error
	walk = func(cmd *cobra.Command) error {
		for _, sub := range cmd.Commands() {
			if sub.HasSubCommands() {
				if err := walk(sub); err != nil {
					return err
				}
			}
			if sub.Annotations[annotationGroup] != "" {
				continue
			}
			low, high, err := argRange(sub)
			if err != nil {
				return err
			}
			flags := map[string]bool{}
			if err := flagValues(sub.LocalFlags(), flags); err != nil {
				return fmt.Errorf("%s: %w", sub.CommandPath(), err)
			}
			if err := flagValues(sub.InheritedFlags(), flags); err != nil {
				return fmt.Errorf("%s: %w", sub.CommandPath(), err)
			}
			for name := range global {
				delete(flags, name)
			}
			words := strings.TrimPrefix(sub.CommandPath(), root.Name()+" ")
			commands[words] = cliCommand{minArgs: low, maxArgs: high, flags: flags}
		}
		return nil
	}
	return commands, global, walk(root)
}

var identifier = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

// tsKey is a key of an object literal, quoted only when it has to be.
func tsKey(k string) string {
	if identifier.MatchString(k) {
		return k
	}
	return fmt.Sprintf("%q", k)
}

// tsFlags is the object literal of flags, indented by indent, one flag a
// line as the formatter keeps an object written that way.
func tsFlags(flags map[string]bool, indent string) string {
	if len(flags) == 0 {
		return "{}"
	}
	var b strings.Builder
	b.WriteString("{\n")
	for _, name := range slices.Sorted(maps.Keys(flags)) {
		fmt.Fprintf(&b, "%s  %s: %t,\n", indent, tsKey(name), flags[name])
	}
	b.WriteString(indent + "}")
	return b.String()
}

// renderCommandTypes is the content of commandTypesFile.
func renderCommandTypes() (string, error) {
	commands, global, err := commandTree(newRoot(&invocation{env: map[string]string{}, out: &writer{}}))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(`// The commands the agentx CLI takes, generated from its command tree: do
// not edit. To write it again, run in apps/cli:
//   go test ./internal/cli -run TestCommandTypes -update

// One command: how many arguments it takes and its flags.
export interface CliCommand {
  // The fewest and the most arguments; null when it takes any number.
  args: readonly [number, number | null];
  // Each flag by name, true when it takes a value.
  flags: Readonly<Record<string, boolean>>;
}

// The flags every command takes, true when one takes a value.
`)
	fmt.Fprintf(&b, "export const GLOBAL_FLAGS: Readonly<Record<string, boolean>> = %s;\n\n", tsFlags(global, ""))
	b.WriteString("// Every command that runs something, by its words, such as \"skill add\".\n")
	b.WriteString("export const CLI_COMMANDS: Readonly<Record<string, CliCommand>> = {\n")
	for _, words := range slices.Sorted(maps.Keys(commands)) {
		c := commands[words]
		high := "null"
		if c.maxArgs >= 0 {
			high = fmt.Sprint(c.maxArgs)
		}
		fmt.Fprintf(&b, "  %s: {\n    args: [%d, %s],\n    flags: %s,\n  },\n", tsKey(words), c.minArgs, high, tsFlags(c.flags, "    "))
	}
	b.WriteString("};\n")
	return b.String(), nil
}

// TestCommandTypes checks that the desktop app's command list is the one
// the command tree gives. Write it again with
// go test ./internal/cli -run TestCommandTypes -update.
func TestCommandTypes(t *testing.T) {
	t.Parallel()
	got, err := renderCommandTypes()
	if err != nil {
		t.Fatal(err)
	}
	if *update {
		if err := os.WriteFile(commandTypesFile, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(commandTypesFile)
	if err != nil {
		t.Fatalf("%v; write it with go test ./internal/cli -run TestCommandTypes -update", err)
	}
	if string(want) != got {
		t.Errorf("%s is not what the command tree gives; write it again with go test ./internal/cli -run TestCommandTypes -update", filepath.Base(commandTypesFile))
	}
}

func TestArgRange(t *testing.T) {
	t.Parallel()
	tests := []struct {
		args      cobra.PositionalArgs
		low, high int
	}{
		{nil, 0, -1},
		{cobra.NoArgs, 0, 0},
		{cobra.ExactArgs(2), 2, 2},
		{cobra.MaximumNArgs(1), 0, 1},
		{cobra.MinimumNArgs(1), 1, -1},
		{cobra.ArbitraryArgs, 0, -1},
	}
	for i, tt := range tests {
		low, high, err := argRange(&cobra.Command{Use: "c", Args: tt.args})
		if err != nil || low != tt.low || high != tt.high {
			t.Errorf("case %d: argRange = %d, %d, %v, want %d, %d", i, low, high, err, tt.low, tt.high)
		}
	}
}
