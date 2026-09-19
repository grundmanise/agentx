package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// cliVersion is set at build time:
// go build -ldflags "-X github.com/grundmanise/agentx/apps/cli/internal/cli.cliVersion=1.2.3"
var cliVersion = "dev"

type versionEvent struct {
	event
	CLIVersion string `json:"cli_version"`
}

func newVersionCommand(inv *invocation) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the CLI version and its output schema version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			inv.out.emit(versionEvent{event: newEvent("version"), CLIVersion: cliVersion})
			t := inv.out.table()
			fmt.Fprintf(t, "CLI version\t%s\n", cliVersion)
			fmt.Fprintf(t, "Schema version\t%d\n", schemaVersion)
			return t.Flush()
		},
	}
}
