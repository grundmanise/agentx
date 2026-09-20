package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
		Short: "Inventory the agent configurations with their skills, MCP servers and plugins",
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
	return fail(exitNotFound, fmt.Sprintf("configuration %q is not detected", slug), hint)
}

// printSnapshot writes the human inventory: one summary line counting the
// machine, then one section per configuration with its skills, servers and
// plugins as labelled blocks. Warnings go to stderr.
func (inv *invocation) printSnapshot(snap scan.Snapshot) {
	out := inv.out
	// Counts per configuration and, keyed by configuration and plugin name,
	// per plugin: what each one carries.
	nSkills, nServers, nPlugins := map[string]int{}, map[string]int{}, map[string]int{}
	pluginSkills, pluginServers := map[string]int{}, map[string]int{}
	skills := map[string]*table{} // name, kind, scope, placement
	for _, s := range snap.Skills {
		for _, o := range s.Occurrences {
			nSkills[o.Configuration]++
			if o.Plugin != "" {
				pluginSkills[o.Configuration+"\x00"+o.Plugin]++
			}
			path := o.Path
			if o.Kind == "symlink" {
				path += out.paint(muted, " -> "+o.ResolvedPath)
			}
			if o.Plugin != "" {
				path += "  " + out.paint(tagStyle, "(plugin "+o.Plugin+")")
			}
			block(skills, o.Configuration).add(c(s.Name, heading), c(o.Kind, muted), c(o.Scope, muted), c(path, plain))
		}
	}
	servers := map[string]*table{} // name, transport, command line or URL, what a handshake found, (disabled)
	for _, s := range snap.MCPServers {
		for _, o := range s.Occurrences {
			nServers[o.Configuration]++
			if o.Plugin != "" {
				pluginServers[o.Configuration+"\x00"+o.Plugin]++
			}
			what := o.URL
			if o.Command != "" {
				what = strings.Join(append([]string{o.Command}, o.Args...), " ")
			}
			if o.Plugin != "" {
				what += "  " + out.paint(tagStyle, "(plugin "+o.Plugin+")")
			}
			cells := []cell{c(s.Name, heading), c(o.Transport, muted), c(what, plain)}
			if n := len(s.Tools); n > 0 {
				cells = append(cells, c(plural(n, "tool"), noteStyle))
			}
			if o.Enabled != nil && !*o.Enabled {
				cells = append(cells, c("(disabled)", warnStyle))
			}
			block(servers, o.Configuration).add(cells...)
		}
	}
	plugins := map[string]*table{} // name, version, what it provides, (disabled)
	for _, p := range snap.Plugins {
		nPlugins[p.Configuration]++
		cells := []cell{c(p.Name, heading), c(p.Version, plain)}
		if provides := counts(pluginSkills[p.Configuration+"\x00"+p.Name], "skill", pluginServers[p.Configuration+"\x00"+p.Name], "server"); provides != "" {
			cells = append(cells, c(provides, noteStyle))
		}
		if p.Enabled != nil && !*p.Enabled {
			cells = append(cells, c("(disabled)", warnStyle))
		}
		block(plugins, p.Configuration).add(cells...)
	}
	if len(snap.Configurations) == 0 {
		out.print(out.paint(warnStyle, "No agent configurations detected."))
		out.print(out.paint(muted, "A configuration is detected by its directory, such as ~/.claude or ~/.cursor; check HOME."))
		return
	}
	out.print(out.paint(infoStyle, plural(len(snap.Configurations), "configuration")+", "+
		plural(len(snap.Skills), "skill")+", "+plural(len(snap.MCPServers), "server")+", "+plural(len(snap.Plugins), "plugin")+" detected"))
	for _, conf := range snap.Configurations {
		out.print("")
		state := out.paint(okStyle, "enabled")
		if !conf.Enabled {
			state = out.paint(warnStyle, "disabled")
		}
		line := []string{out.paint(heading, conf.Name), " ", out.paint(muted, "("+conf.ID+")"), "  ", conf.Path, "  ", state}
		if has := counts(nSkills[conf.ID], "skill", nServers[conf.ID], "server", nPlugins[conf.ID], "plugin"); has != "" {
			line = append(line, "  ", out.paint(noteStyle, has))
		}
		out.print(line...)
		empty := true
		for _, b := range []struct {
			name string
			rows map[string]*table
		}{{"skills", skills}, {"servers", servers}, {"plugins", plugins}} {
			t, ok := b.rows[conf.ID]
			if !ok {
				continue
			}
			empty = false
			out.print("  ", out.paint(label, b.name+":"))
			t.sortRows()
			out.render(t, "    ")
		}
		if empty {
			out.print("  ", out.paint(muted, "no skills, servers or plugins"))
		}
	}
	for _, w := range snap.Warnings {
		out.warn(w)
	}
}

// counts joins the non-zero counts of pairs of count and noun, so that a
// heading says what is there and nothing about what is not: "2 skills,
// 1 server", or "" when every count is zero.
func counts(pairs ...any) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		if n := pairs[i].(int); n > 0 {
			parts = append(parts, plural(n, pairs[i+1].(string)))
		}
	}
	return strings.Join(parts, ", ")
}

// block returns the table for configuration id in m, creating it on first use.
func block(m map[string]*table, id string) *table {
	t, ok := m[id]
	if !ok {
		t = &table{}
		m[id] = t
	}
	return t
}
