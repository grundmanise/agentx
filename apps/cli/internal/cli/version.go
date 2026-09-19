package cli

import (
	"strconv"

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
			t := &table{}
			t.add(c("CLI version", label), c(cliVersion, heading))
			t.add(c("Schema version", label), c(strconv.Itoa(schemaVersion), plain))
			inv.out.render(t, "")
			return nil
		},
	}
}
