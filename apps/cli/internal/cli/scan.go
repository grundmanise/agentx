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
	var handshake bool
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
			snap, err := inv.scan(cmd.Context(), lockWait, project, handshake)
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
	cmd.Flags().BoolVar(&handshake, "handshake", false, "start or connect to every MCP server and record the tools, prompts and resources it exposes")
	return cmd
}

// handshakeTimeout is the budget per server: ten seconds, or
// AGENTX_HANDSHAKE_TIMEOUT, a duration such as 500ms, for tests and diagnosis.
const handshakeTimeout = 10 * time.Second

// lockWait is how long a one-shot scan waits for a mutation to finish; a
// longer wait is exit code 7. Serve passes 0 and waits until its context ends.
const lockWait = time.Second

// scan inventories the machine under the shared lock, waiting for a mutation
// in progress for wait (or until ctx is done when wait is 0), then, with
// handshake, connects to every declared server outside the lock and stores
// what each exposed. The machine id is read first: storing a random one takes
// the exclusive lock.
func (inv *invocation) scan(ctx context.Context, wait time.Duration, project string, handshake bool) (scan.Snapshot, error) {
	timeout := handshakeTimeout
	if v := inv.env["AGENTX_HANDSHAKE_TIMEOUT"]; handshake && v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return scan.Snapshot{}, fail(exitUsage, "AGENTX_HANDSHAKE_TIMEOUT "+v+" is not a duration", "set it like 10s or 500ms, or unset it")
		}
		timeout = d
	}
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
	lockCtx := ctx
	if wait > 0 {
		var cancel context.CancelFunc
		lockCtx, cancel = context.WithTimeout(ctx, wait)
		defer cancel()
	}
	var sc *scan.Scan
	err = home.ReadLocked(lockCtx, inv.dirs.Home, func() error {
		s, err := inv.loadSettings()
		if err != nil {
			return err
		}
		var copyMode map[string][]string
		if err := json.Unmarshal(s.CopyMode, &copyMode); err != nil {
			return fail(exitInternal, "parse "+home.SettingsPath(inv.dirs.Home)+": copy_mode must map skill names to configuration ids", "fix copy_mode in the settings file")
		}
		sc = scan.Read(scan.Options{
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
	if err != nil {
		return scan.Snapshot{}, err
	}
	if handshake {
		sc.Handshake(ctx, scan.HandshakeOptions{Env: inv.env, Timeout: timeout, Version: cliVersion})
	}
	snap, fresh := sc.Snapshot()
	if len(fresh) > 0 {
		if err := home.SaveHandshakes(inv.dirs.Home, fresh); err != nil {
			return scan.Snapshot{}, err
		}
	}
	return snap, nil
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

// printSnapshot writes the human inventory: one section per configuration
// with one line per skill occurrence, then its servers and its plugins.
// Warnings go to stderr.
func (inv *invocation) printSnapshot(snap scan.Snapshot) {
	type row struct{ name, kind, scope, path string }
	rows := map[string][]row{}
	for _, s := range snap.Skills {
		for _, o := range s.Occurrences {
			path := o.Path
			if o.Kind == "symlink" {
				path += " -> " + o.ResolvedPath
			}
			if o.Plugin != "" {
				path += "  (plugin " + o.Plugin + ")"
			}
			rows[o.Configuration] = append(rows[o.Configuration], row{s.Name, o.Kind, o.Scope, path})
		}
	}
	servers := map[string][]row{} // name, transport, command line or URL, what a handshake found
	for _, s := range snap.MCPServers {
		var exposed string
		switch n := len(s.Tools); {
		case n == 1:
			exposed = "1 tool"
		case n > 1:
			exposed = fmt.Sprintf("%d tools", n)
		}
		for _, o := range s.Occurrences {
			what := o.URL
			if o.Command != "" {
				what = strings.Join(append([]string{o.Command}, o.Args...), " ")
			}
			if o.Plugin != "" {
				what += "  (plugin " + o.Plugin + ")"
			}
			servers[o.Configuration] = append(servers[o.Configuration], row{name: s.Name, kind: o.Transport, path: what, scope: exposed})
		}
	}
	plugins := map[string][]row{} // name, version
	for _, p := range snap.Plugins {
		plugins[p.Configuration] = append(plugins[p.Configuration], row{name: p.Name, kind: p.Version})
	}
	byName := func(list []row) {
		sort.Slice(list, func(i, j int) bool {
			if list[i].name != list[j].name {
				return list[i].name < list[j].name
			}
			return list[i].path < list[j].path
		})
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
		byName(rows[c.ID])
		for _, r := range rows[c.ID] {
			fmt.Fprintf(t, "  %s\t%s\t%s\t%s\n", r.name, r.kind, r.scope, r.path)
		}
		if list := servers[c.ID]; len(list) > 0 {
			byName(list)
			fmt.Fprintln(t, "  servers:")
			for _, r := range list {
				fmt.Fprintf(t, "    %s\t%s\t%s\t%s\n", r.name, r.kind, r.path, r.scope)
			}
		}
		if list := plugins[c.ID]; len(list) > 0 {
			byName(list)
			fmt.Fprintln(t, "  plugins:")
			for _, r := range list {
				fmt.Fprintf(t, "    %s\t%s\n", r.name, r.kind)
			}
		}
	}
	t.Flush()
	for _, w := range snap.Warnings {
		fmt.Fprintf(inv.out.stderr, "warning: %s\n", w)
	}
}
