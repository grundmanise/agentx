// Package cli is the agentx command line: Run is the seam every test drives.
package cli

import (
	"context"
	"errors"
	"io"
	"slices"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// invocation is what every command shares for one run.
type invocation struct {
	env    map[string]string
	dirs   home.Dirs
	out    *writer
	parsed bool // set once cobra has parsed the command line; errors after that are agentx's own
}

// Run executes one agentx invocation and returns its exit code. It reads
// nothing from the process: the environment comes from env and every stream
// is a parameter.
func Run(ctx context.Context, args []string, env map[string]string, stdin io.Reader, stdout, stderr io.Writer) int {
	// --json is decided by this scan, not by flag parsing, so that cobra's own
	// text (help, usage) and every error, including a flag cobra rejects, stay
	// off JSON stdout. The flag is still declared on the root so cobra accepts it.
	out := &writer{stdout: stdout, stderr: stderr, json: slices.Contains(args, "--json")}
	inv := &invocation{env: env, out: out}

	root := newRoot(inv)
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

// newRoot builds the whole command tree; a new command is one AddCommand line here.
func newRoot(inv *invocation) *cobra.Command {
	root := &cobra.Command{
		Use:           "agentx",
		Short:         "Inventory the agent clients, skills, MCP servers and plugins on this machine",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			inv.parsed = true
			dirs, err := home.Resolve(inv.env)
			if err != nil {
				return fail(exitUsage, err.Error(), "set HOME to your home directory")
			}
			inv.dirs = dirs
			inv.out.debugf("agentx home %s, library %s, config home %s", dirs.Home, dirs.Library, dirs.Config)
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return fail(exitUsage, "no command given", "run 'agentx help' to list commands")
		},
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.PersistentFlags().Bool("json", false, "write newline-delimited JSON events to stdout")
	root.PersistentFlags().BoolVar(&inv.out.verbose, "verbose", false, "log at debug level on stderr")

	root.AddCommand(newVersionCommand(inv))
	return root
}

// finish reports err, emits the terminating result and maps err to an exit code.
func finish(inv *invocation, err error) int {
	out := inv.out
	if err == nil {
		out.result(true, "")
		return exitOK.exit
	}
	var f *failure
	if !errors.As(err, &f) {
		if inv.parsed {
			f = &failure{status: exitInternal, message: err.Error()}
		} else {
			f = &failure{status: exitUsage, message: err.Error(), hint: "run 'agentx help' for usage"}
		}
	}
	out.fail(f)
	out.result(false, f.message)
	return f.status.exit
}
