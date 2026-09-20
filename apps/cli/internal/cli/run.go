// Package cli is the agentx command line: Run is the seam every test drives.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// invocation is what every command shares for one run.
type invocation struct {
	env      map[string]string
	out      *writer
	dirs     home.Dirs
	git      *gitx.Runner
	instance string // the instance_id snapshots carry, fixed once per run
	parsed   bool   // set once cobra has parsed the command line; errors after that are agentx's own
}

// Run executes one agentx invocation and returns its exit code. It reads
// nothing from the process: the environment comes from env and every stream
// is a parameter.
func Run(ctx context.Context, args []string, env map[string]string, stdin io.Reader, stdout, stderr io.Writer) int {
	// --json is decided by this scan, not by flag parsing, so that cobra's own
	// text (help, usage) and every error, including a flag cobra rejects, stay
	// off JSON stdout. The flag is still declared on the root so cobra accepts it.
	// --color is scanned the same way, so that an error cobra reports before
	// it parsed the flags, such as an unknown command, is painted as asked.
	out := &writer{stdout: stdout, stderr: stderr, env: env, json: slices.Contains(args, "--json"), color: colorFromArgs(args)}
	inv := &invocation{env: env, out: out}

	root := newRoot(inv)
	if args == nil {
		args = []string{} // cobra reads the process arguments when given nil
	}
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetErr(stderr)
	if out.json {
		root.SetOut(stderr)
	} else {
		root.SetOut(stdout)
	}
	return finish(inv, root.ExecuteContext(ctx))
}

// colorFromArgs returns the value of --color on the command line, as
// --color=value or --color value, or unset; cobra validates it after parsing.
func colorFromArgs(args []string) string {
	for i, arg := range args {
		if v, ok := strings.CutPrefix(arg, "--color="); ok {
			return v
		}
		if arg == "--color" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return string(colorUnset)
}

// newRoot builds the whole command tree; a new command is one AddCommand line here.
func newRoot(inv *invocation) *cobra.Command {
	var colorFlag string // parsed by cobra; the pre-scanned value stands until then
	root := &cobra.Command{
		Use:           "agentx",
		Short:         "Inventory the agent clients, skills, MCP servers and plugins on this machine",
		Annotations:   map[string]string{annotationGroup: "true"},
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			switch colorMode(colorFlag) {
			case colorUnset, colorOn, colorOff:
				inv.out.color = colorFlag
			default:
				return fail(exitUsage, fmt.Sprintf("invalid value %q for --color", colorFlag), "use on or off")
			}
			inv.parsed = true
			dirs, err := home.Resolve(inv.env)
			if err != nil {
				return fail(exitUsage, err.Error(), "set HOME to your home directory")
			}
			inv.dirs = dirs
			inv.out.debugf("agentx home %s, library %s, config home %s", dirs.Home, dirs.Library, dirs.Config)
			inv.git = gitx.New(inv.env, cmd.Name() == "serve", inv.out.debugf) // serve must fail rather than prompt
			if needsGit(cmd.Name()) {
				if _, err := inv.gitVersion(cmd.Context()); err != nil {
					return err
				}
			}
			return nil
		},
		RunE: needSubcommand(inv, "no command given", "run 'agentx help' to list commands"),
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.PersistentFlags().Bool("json", false, "write newline-delimited JSON events to stdout")
	root.PersistentFlags().BoolVar(&inv.out.verbose, "verbose", false, "log at debug level on stderr")
	root.PersistentFlags().StringVar(&colorFlag, "color", string(colorUnset), "colour the text output: on or off; left out, a terminal gets colour and a pipe does not")
	root.SetHelpFunc(inv.help)
	root.SetUsageFunc(inv.usage)

	root.AddCommand(newVersionCommand(inv))
	root.AddCommand(newConfigCommand(inv))
	root.AddCommand(newMachineCommand(inv))
	root.AddCommand(newScanCommand(inv))
	root.AddCommand(newDoctorCommand(inv))
	root.AddCommand(newServeCommand(inv))
	return root
}

// needSubcommand is the RunE of a command that only groups subcommands: help
// for a human, a usage error for a script.
func needSubcommand(inv *invocation, message, hint string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if inv.out.json {
			return fail(exitUsage, message, hint)
		}
		return cmd.Help()
	}
}

// finish reports err, emits the terminating result and maps err to an exit code.
func finish(inv *invocation, err error) int {
	out := inv.out
	if err == nil {
		out.result(true, "")
		return exitOK.exit
	}
	var f *failure
	switch {
	case errors.As(err, &f):
	case errors.Is(err, home.ErrLocked):
		f = &failure{status: exitLocked, message: err.Error(), hint: "wait for the command holding " + home.LockPath(inv.dirs.Home) + " to finish, then retry"}
	case errors.Is(err, home.ErrRecovery):
		f = &failure{status: exitRefused, message: err.Error(), hint: "restore the file to let the change finish, or move the journal aside to keep the file as it is"}
	case inv.parsed:
		f = &failure{status: exitInternal, message: err.Error()}
	default:
		f = &failure{status: exitUsage, message: err.Error(), hint: "run 'agentx help' for usage"}
	}
	out.fail(f)
	out.result(false, f.message)
	return f.status.exit
}
