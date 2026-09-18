package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

type machineEvent struct {
	event
	ID         string `json:"id"`
	Label      string `json:"label"`
	Derivation string `json:"derivation"`
}

func newMachineCommand(inv *invocation) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "machine",
		Short: "Print this machine's id, label and how the id was derived",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := inv.machine()
			if err != nil {
				return err
			}
			inv.out.emit(m)
			t := inv.out.table()
			fmt.Fprintf(t, "Machine id\t%s\n", m.ID)
			fmt.Fprintf(t, "Label\t%s\n", m.Label)
			fmt.Fprintf(t, "Derivation\t%s\n", m.Derivation)
			return t.Flush()
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "rename <label>",
		Short: "Change the machine label",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validLabel(args[0]); err != nil {
				return err
			}
			return inv.mutateMachine(func() error {
				s, err := inv.loadSettings()
				if err != nil {
					return err
				}
				s.Label = args[0]
				return home.SaveSettings(inv.dirs.Home, s)
			})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "reset-id",
		Short: "Replace the machine id with a new random one",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return inv.mutateMachine(func() error {
				_, err := home.ResetMachineID(inv.dirs.Home)
				return err
			})
		},
	})
	return cmd
}

// mutateMachine runs fn as a mutation and then reports the machine as it now is.
func (inv *invocation) mutateMachine(fn func() error) error {
	if err := inv.mutate(fn); err != nil {
		return err
	}
	m, err := inv.machine()
	if err != nil {
		return err
	}
	inv.out.emit(m)
	return nil
}

func (inv *invocation) machine() (machineEvent, error) {
	s, err := inv.loadSettings()
	if err != nil {
		return machineEvent{}, err
	}
	id, derivation, err := home.MachineID(inv.dirs.Home, inv.env)
	if err != nil {
		return machineEvent{}, err
	}
	return machineEvent{event: newEvent("machine"), ID: id, Label: inv.label(s), Derivation: derivation}, nil
}
