package cli

import (
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
		Short: "Print the machine id, label and how the id was derived",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := inv.machine()
			if err != nil {
				return err
			}
			inv.out.emit(m)
			inv.printMachine(m)
			return nil
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
			m, err := inv.mutateMachine(func() error {
				s, err := inv.loadSettings()
				if err != nil {
					return err
				}
				s.Label = args[0]
				return home.SaveSettings(inv.dirs.Home, s)
			})
			if err != nil {
				return err
			}
			inv.out.done("machine label is now " + inv.out.paint(heading, m.Label))
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "reset-id",
		Short: "Replace the machine id with a new random one",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := inv.mutateMachine(func() error {
				_, err := home.ResetMachineID(inv.dirs.Home)
				return err
			})
			if err != nil {
				return err
			}
			inv.out.done("machine id is now " + inv.out.paint(heading, m.ID))
			return nil
		},
	})
	return cmd
}

// mutateMachine runs fn as a mutation and then reports the machine as it now is.
func (inv *invocation) mutateMachine(fn func() error) (machineEvent, error) {
	if err := home.Mutate(inv.dirs.Home, fn); err != nil {
		return machineEvent{}, err
	}
	m, err := inv.machine()
	if err != nil {
		return machineEvent{}, err
	}
	inv.out.emit(m)
	return m, nil
}

// printMachine writes the machine as a key-value table.
func (inv *invocation) printMachine(m machineEvent) {
	t := &table{}
	t.add(c("Machine id", label), c(m.ID, heading))
	t.add(c("Label", label), c(m.Label, plain))
	t.add(c("Derivation", label), c(m.Derivation, muted))
	inv.out.render(t, "")
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
