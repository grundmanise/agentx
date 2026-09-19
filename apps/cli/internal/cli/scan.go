package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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
		Short: "Inventory the agent configurations on this machine with their skills, MCP servers and plugins",
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
// the exclusive lock. An unfinished mutation journal found under the shared
// lock is recovered under the exclusive one, and the reads start over: no
// inventory is composed from half-applied state.
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
	for sc == nil {
		var journals []string
		err = home.ReadLocked(lockCtx, inv.dirs.Home, func() error {
			var err error
			if journals, err = home.Journals(inv.dirs.Home); err != nil || len(journals) > 0 {
				return err
			}
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
		if err == nil && len(journals) > 0 {
			inv.out.debugf("recovering %s", strings.Join(journals, ", "))
			err = home.Recover(lockCtx, inv.dirs.Home)
		}
		if err != nil {
			return scan.Snapshot{}, err
		}
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
	if inv.instance == "" {
		if inv.instance = inv.env["AGENTX_INSTANCE_ID"]; inv.instance == "" {
			var b [16]byte
			rand.Read(b[:])
			inv.instance = hex.EncodeToString(b[:])
		}
	}
	return inv.instance
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
	skills := map[string][][]string{} // name, kind, scope, placement
	for _, s := range snap.Skills {
		for _, o := range s.Occurrences {
			path := o.Path
			if o.Kind == "symlink" {
				path += " -> " + o.ResolvedPath
			}
			if o.Plugin != "" {
				path += "  (plugin " + o.Plugin + ")"
			}
			skills[o.Configuration] = append(skills[o.Configuration], []string{s.Name, o.Kind, o.Scope, path})
		}
	}
	servers := map[string][][]string{} // name, transport, command line or URL, what a handshake found, (disabled)
	for _, s := range snap.MCPServers {
		for _, o := range s.Occurrences {
			what := o.URL
			if o.Command != "" {
				what = strings.Join(append([]string{o.Command}, o.Args...), " ")
			}
			if o.Plugin != "" {
				what += "  (plugin " + o.Plugin + ")"
			}
			cells := []string{s.Name, o.Transport, what}
			if n := len(s.Tools); n > 0 {
				cells = append(cells, plural(n, "tool"))
			}
			if o.Enabled != nil && !*o.Enabled {
				cells = append(cells, "(disabled)")
			}
			servers[o.Configuration] = append(servers[o.Configuration], cells)
		}
	}
	plugins := map[string][][]string{} // name, version, (disabled)
	for _, p := range snap.Plugins {
		cells := []string{p.Name, p.Version}
		if p.Enabled != nil && !*p.Enabled {
			cells = append(cells, "(disabled)")
		}
		plugins[p.Configuration] = append(plugins[p.Configuration], cells)
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
		printRows(t, "  ", skills[c.ID])
		if len(servers[c.ID]) > 0 {
			fmt.Fprintln(t, "  servers:")
			printRows(t, "    ", servers[c.ID])
		}
		if len(plugins[c.ID]) > 0 {
			fmt.Fprintln(t, "  plugins:")
			printRows(t, "    ", plugins[c.ID])
		}
	}
	t.Flush()
	for _, w := range snap.Warnings {
		fmt.Fprintf(inv.out.stderr, "warning: %s\n", w)
	}
}

// printRows writes rows of cells as one indented table line each, sorted by
// the first cell and then the whole line.
func printRows(t io.Writer, indent string, rows [][]string) {
	lines := make([]string, len(rows))
	for i, cells := range rows {
		lines[i] = strings.Join(cells, "\t")
	}
	sort.Slice(lines, func(i, j int) bool {
		a, _, _ := strings.Cut(lines[i], "\t")
		b, _, _ := strings.Cut(lines[j], "\t")
		if a != b {
			return a < b
		}
		return lines[i] < lines[j]
	})
	for _, line := range lines {
		fmt.Fprintln(t, indent+line)
	}
}
