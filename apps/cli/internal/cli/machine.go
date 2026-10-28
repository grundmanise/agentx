package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

type machineEvent struct {
	event
	ID         string `json:"id"`
	Label      string `json:"label"`
	Derivation string `json:"derivation"`
	// Where agentx keeps its files on this machine, as AGENTX_HOME and
	// AGENTX_LIBRARY resolved them. The account repo and its size are
	// absent until the repo is created.
	Home             string `json:"home"`
	SettingsFile     string `json:"settings_file"`
	Library          string `json:"library"`
	AccountRepo      string `json:"account_repo,omitempty"`
	AccountRepoBytes int64  `json:"account_repo_bytes,omitempty"`
}

func newMachineCommand(inv *invocation) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "machine",
		Short: "Print the machine id, label, how the id was derived and where agentx keeps its files",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := inv.machine(cmd.Context())
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
			m, err := inv.mutateMachine(cmd.Context(), func() error {
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
			m, err := inv.mutateMachine(cmd.Context(), func() error {
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
func (inv *invocation) mutateMachine(ctx context.Context, fn func() error) (machineEvent, error) {
	if err := home.Mutate(inv.dirs.Home, inv.refs(ctx), fn); err != nil {
		return machineEvent{}, err
	}
	m, err := inv.machine(ctx)
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
	t.add(c("Home", label), c(m.Home, plain))
	t.add(c("Settings", label), c(m.SettingsFile, plain))
	t.add(c("Library", label), c(m.Library, plain))
	if m.AccountRepo == "" {
		t.add(c("Account repo", label), c("not created yet", muted))
	} else {
		t.add(c("Account repo", label), c(m.AccountRepo+" ("+formatBytes(m.AccountRepoBytes)+")", plain))
	}
	inv.out.render(t, "")
}

func (inv *invocation) machine(ctx context.Context) (machineEvent, error) {
	s, err := inv.loadSettings()
	if err != nil {
		return machineEvent{}, err
	}
	id, derivation, err := home.MachineID(inv.dirs.Home, inv.env, inv.refs(ctx))
	if err != nil {
		return machineEvent{}, err
	}
	m := machineEvent{event: newEvent("machine"), ID: id, Label: inv.label(s), Derivation: derivation,
		Home: inv.dirs.Home, SettingsFile: home.SettingsPath(inv.dirs.Home), Library: inv.dirs.Library}
	// The account repo is reported from files alone, without a git process:
	// a present directory is the repo whether or not git can read it, which
	// is doctor's question, and its size is the one the user asks about.
	repo := gitx.AccountRepoPath(inv.dirs.Home)
	size, err := dirSize(repo)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return machineEvent{}, err
	default:
		m.AccountRepo, m.AccountRepoBytes = repo, size
	}
	return m, nil
}

// dirSize is the size on disk of the tree under root: the sizes of the
// regular files it holds, added up. A symlink counts nothing and is never
// followed, so the account repo's size is that of account.git alone, and
// nothing a worktree outside it holds. A missing root is fs.ErrNotExist.
func dirSize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	return total, err
}

// formatBytes renders a size for a person: whole bytes below a kibibyte,
// then one decimal in the largest binary unit that keeps the number above 1.
func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value, suffix := float64(n)/unit, "KiB"
	for _, next := range []string{"MiB", "GiB", "TiB"} {
		if value < unit {
			break
		}
		value, suffix = value/unit, next
	}
	return fmt.Sprintf("%.1f %s", value, suffix)
}
