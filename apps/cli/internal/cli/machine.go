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
	// AGENTX_LIBRARY resolved them. The store and its size are
	// absent until the repo is created.
	Home         string `json:"home"`
	SettingsFile string `json:"settings_file"`
	Library      string `json:"library"`
	Store        string `json:"store,omitempty"`
	StoreBytes   int64  `json:"store_bytes,omitempty"`
}

func newMachineCommand(inv *invocation) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "machine",
		Short:       "Print this machine's id, label and agentx's paths",
		Annotations: map[string]string{annotationSection: "machines"},
		Args:        cobra.NoArgs,
		Example: examples(
			"agentx machine", "this machine's id, label and paths",
			`agentx machine rename "work laptop"`, "change the label",
			"agentx machine reset-id", "give the machine a new id",
			"agentx machine report", "write this machine's inventory report to the hub",
			"agentx machine list", "this machine and the machines the hub holds",
			"agentx machine show work-laptop", "the skills of one of them, by agent configuration",
		),
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
		Use:     "rename <label>",
		Short:   "Change this machine's label",
		Args:    cobra.ExactArgs(1),
		Example: examples(`agentx machine rename "work laptop"`, "change the label"),
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
		Use:     "reset-id",
		Short:   "Replace this machine's id with a new random one",
		Args:    cobra.NoArgs,
		Example: examples("agentx machine reset-id", "replace the id with a new random one"),
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
	cmd.AddCommand(&cobra.Command{
		Use:   "report",
		Short: "Write this machine's inventory report to the hub",
		Long: "Scan this machine and write its inventory report to the hub, as the one file\n" +
			"inventory.json of the branch reports/<machine id>, so that your other machines\n" +
			"can show which skills this one has and which agent clients see them.\n\n" +
			"The report names the machine, its agent configurations, every skill an agent\n" +
			"client sees with its content hash, the library's skills with their sources, and\n" +
			"the plugins. It holds no path on the machine, no settings, no MCP server and no\n" +
			"content of a skill. Whoever can read the hub can read it.\n\n" +
			"A report the branch holds already is pushed nowhere. Otherwise it is one commit\n" +
			"on the branch, pushed without force. The report leaves the hub when this\n" +
			"machine does: 'agentx source remove' of the hub deletes it there.",
		Args:    cobra.NoArgs,
		Example: examples("agentx machine report", "write this machine's report to the hub"),
		RunE:    func(cmd *cobra.Command, args []string) error { return inv.machineReport(cmd.Context()) },
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List this machine and the machines the hub holds",
		Long: "List this machine and every machine the hub holds something of: each machine\n" +
			"that wrote an inventory report, and the cloud machine, from its snapshot. Each\n" +
			"row says when the machine's branch last changed, how many skills its agent\n" +
			"clients see and which agent clients it has. A row says what the machine held at\n" +
			"that change, never that the machine is on.\n\n" +
			"This machine is listed first, from a scan made now, whether it reports or not.\n" +
			"With --json, the one fleet event holds what the hub holds and nothing else.",
		Args: cobra.NoArgs,
		Example: examples(
			"agentx machine list", "this machine and the machines the hub holds",
			"agentx machine list --json", "what the hub holds of each machine, as one fleet event",
		),
		RunE: func(cmd *cobra.Command, args []string) error { return inv.machineList(cmd.Context()) },
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "show <machine>",
		Short: "Show the skills of one machine, as the hub holds them",
		Long: "Show one machine as the hub holds it: its label, id, operating system and\n" +
			"agentx version, when its branch last changed, and the skills each of its agent\n" +
			"configurations sees. Each skill names where it comes from: the library, with\n" +
			"the source it is published to, a plugin, or the agent client's own skills\n" +
			"directory. A library skill edited on that machine reads modified.\n\n" +
			"<machine> is cloud, a machine's label, its id or the start of its id, as\n" +
			"'agentx machine list' and 'agentx machine' print them.",
		Args: cobra.ExactArgs(1),
		Example: examples(
			"agentx machine show work-laptop", "a machine by its label",
			"agentx machine show 3f9a", "a machine by the start of its id",
			"agentx machine show cloud", "the cloud machine, from its snapshot",
		),
		RunE: func(cmd *cobra.Command, args []string) error { return inv.machineShow(cmd.Context(), args[0]) },
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
	if m.Store == "" {
		t.add(c("Store", label), c("not created yet", muted))
	} else {
		t.add(c("Store", label), c(m.Store+" ("+formatBytes(m.StoreBytes)+")", plain))
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
	// The store is reported from files alone, without a git process:
	// a present directory is the repo whether or not git can read it, which
	// is doctor's question, and its size is the one the user asks about.
	repo := gitx.StorePath(inv.dirs.Home)
	size, err := dirSize(repo)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return machineEvent{}, err
	default:
		m.Store, m.StoreBytes = repo, size
	}
	return m, nil
}

// dirSize is the size on disk of the tree under root: the sizes of the
// regular files it holds, added up. A symlink counts nothing and is never
// followed, so the store's size is that of store.git alone, and
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
