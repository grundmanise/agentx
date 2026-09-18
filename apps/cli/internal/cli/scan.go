package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
)

type snapshotEvent struct {
	event
	scan.Snapshot
}

func newScanCommand(inv *invocation) *cobra.Command {
	var project, configuration string
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Inventory the agent configurations on this machine and the skills each one sees",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if configuration != "" {
				if err := inv.detectedConfiguration(configuration); err != nil {
					return err
				}
			}
			// A one-shot scan waits for a mutation to finish for at most a
			// second; a longer wait is exit code 7.
			ctx, cancel := context.WithTimeout(cmd.Context(), time.Second)
			defer cancel()
			snap, err := inv.scan(ctx, project)
			if err != nil {
				return err
			}
			inv.out.emit(snapshotEvent{event: newEvent("snapshot"), Snapshot: snap})
			if !inv.out.json {
				inv.printSnapshot(snap)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "also inventory the project-scope skills under this directory, read-only")
	cmd.Flags().StringVar(&configuration, "configuration", "", "the configuration that changed; the whole machine is scanned regardless")
	return cmd
}

// scan inventories the machine under the shared lock, waiting for a mutation
// in progress until ctx is done. The machine id is read first: storing a
// random one takes the exclusive lock.
func (inv *invocation) scan(ctx context.Context, project string) (scan.Snapshot, error) {
	if project != "" {
		abs, err := filepath.Abs(project)
		if err != nil {
			return scan.Snapshot{}, err
		}
		if info, err := os.Stat(abs); err != nil || !info.IsDir() {
			return scan.Snapshot{}, fail(exitNotFound, "project directory "+abs+" does not exist", "pass the root of a repository to --project")
		}
		project = abs
	}
	machineID, _, err := home.MachineID(inv.dirs.Home, inv.env)
	if err != nil {
		return scan.Snapshot{}, err
	}
	var snap scan.Snapshot
	err = home.ReadLocked(ctx, inv.dirs.Home, func() error {
		s, err := inv.loadSettings()
		if err != nil {
			return err
		}
		var copyMode map[string][]string
		if err := json.Unmarshal(s.CopyMode, &copyMode); err != nil {
			return fail(exitInternal, "parse "+home.SettingsPath(inv.dirs.Home)+": copy_mode must map skill names to configuration ids", "fix copy_mode in the settings file")
		}
		snap = scan.Run(scan.Options{
			Dirs:       inv.dirs,
			MachineID:  machineID,
			Label:      inv.label(s),
			InstanceID: inv.instanceID(),
			Project:    project,
			Disabled:   s.DisabledConfigurations,
			CopyMode:   copyMode,
		})
		return nil
	})
	return snap, err
}

// instanceID is fresh per process; AGENTX_INSTANCE_ID fixes it for tests.
func (inv *invocation) instanceID() string {
	if id := inv.env["AGENTX_INSTANCE_ID"]; id != "" {
		return id
	}
	var b [16]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// detectedConfiguration is nil when slug names a detected configuration, else
// a not-found failure listing the detected ones.
func (inv *invocation) detectedConfiguration(slug string) error {
	var slugs []string
	for _, c := range scan.Detect(inv.dirs) {
		if c.Slug() == slug {
			return nil
		}
		slugs = append(slugs, c.Slug())
	}
	hint := "no configuration is detected; install an agent client first"
	if len(slugs) > 0 {
		hint = "detected configurations: " + strings.Join(slugs, ", ")
	}
	return fail(exitNotFound, fmt.Sprintf("configuration %q is not detected on this machine", slug), hint)
}

// printSnapshot writes the human inventory: one section per configuration,
// one line per skill occurrence. Warnings go to stderr.
func (inv *invocation) printSnapshot(snap scan.Snapshot) {
	type row struct{ name, kind, scope, path string }
	rows := map[string][]row{}
	for _, s := range snap.Skills {
		for _, o := range s.Occurrences {
			path := o.Path
			if o.Kind == "symlink" {
				path += " -> " + o.ResolvedPath
			}
			rows[o.Configuration] = append(rows[o.Configuration], row{s.Name, o.Kind, o.Scope, path})
		}
	}
	if len(snap.Configurations) == 0 {
		fmt.Fprintln(inv.out.stdout, "No agent configurations detected.")
	}
	t := inv.out.table()
	for i, c := range snap.Configurations {
		if i > 0 {
			fmt.Fprintln(t)
		}
		state := "enabled"
		if !c.Enabled {
			state = "disabled"
		}
		fmt.Fprintf(t, "%s (%s)  %s  %s\n", c.Name, c.ID, c.Path, state)
		list := rows[c.ID]
		sort.Slice(list, func(i, j int) bool {
			if list[i].name != list[j].name {
				return list[i].name < list[j].name
			}
			return list[i].path < list[j].path
		})
		for _, r := range list {
			fmt.Fprintf(t, "  %s\t%s\t%s\t%s\n", r.name, r.kind, r.scope, r.path)
		}
	}
	t.Flush()
	for _, w := range snap.Warnings {
		fmt.Fprintf(inv.out.stderr, "warning: %s\n", w)
	}
}
