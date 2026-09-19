package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

const settableKeys = "label, auto_push, accept_operations"

type settingsEvent struct {
	event
	Settings home.Settings `json:"settings"`
}

func newConfigCommand(inv *invocation) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "config",
		Short:       "Read and change this machine's settings",
		Annotations: map[string]string{annotationGroup: "true"},
		Args:        cobra.NoArgs,
		RunE:        needSubcommand(inv, "no config command given", "run 'agentx config --help' to list commands"),
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "Print every setting",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := inv.loadSettings()
			if err != nil {
				return err
			}
			inv.emitSettings(s)
			inv.printSettings(s)
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "get <key>",
		Short: "Print one setting",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := inv.loadSettings()
			if err != nil {
				return err
			}
			for _, row := range inv.settingRows(s) {
				if row.key == args[0] {
					inv.emitSettings(s)
					if !inv.out.json {
						fmt.Fprintln(inv.out.stdout, row.value)
					}
					return nil
				}
			}
			return fail(exitUsage, fmt.Sprintf("unknown setting %q", args[0]), "keys: schema_version, "+settableKeys+", disabled_configurations, sources, copy_mode")
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "set <key> <value>",
		Short: "Change label, auto_push or accept_operations",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			apply, err := settingSetter(args[0], args[1])
			if err != nil {
				return err
			}
			var s home.Settings
			err = home.Mutate(inv.dirs.Home, func() error {
				var err error
				if s, err = inv.loadSettings(); err != nil {
					return err
				}
				apply(&s)
				return home.SaveSettings(inv.dirs.Home, s)
			})
			if err != nil {
				return err
			}
			inv.emitSettings(s)
			inv.out.done(inv.out.paint(label, args[0]) + " is now " + inv.out.paint(heading, args[1]))
			return nil
		},
	})
	cmd.AddCommand(newEnableCommand(inv, "enable", "Select a configuration for placements by default"))
	cmd.AddCommand(newEnableCommand(inv, "disable", "Leave a configuration out of placements by default"))
	return cmd
}

// newEnableCommand builds config enable or config disable: both edit the
// disabled_configurations list, and the configuration must be detected.
func newEnableCommand(inv *invocation, use, short string) *cobra.Command {
	disable := use == "disable"
	return &cobra.Command{
		Use:   use + " <configuration>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := inv.detectedConfiguration(args[0]); err != nil {
				return err
			}
			var s home.Settings
			err := home.Mutate(inv.dirs.Home, func() error {
				var err error
				if s, err = inv.loadSettings(); err != nil {
					return err
				}
				s.DisabledConfigurations = slices.DeleteFunc(s.DisabledConfigurations, func(id string) bool { return id == args[0] })
				if disable {
					s.DisabledConfigurations = append(s.DisabledConfigurations, args[0])
					slices.Sort(s.DisabledConfigurations)
				}
				return home.SaveSettings(inv.dirs.Home, s)
			})
			if err != nil {
				return err
			}
			inv.emitSettings(s)
			if disable {
				inv.out.done(inv.out.paint(label, args[0]) + " is now " + inv.out.paint(warnStyle, "disabled"))
			} else {
				inv.out.done(inv.out.paint(label, args[0]) + " is now " + inv.out.paint(okStyle, "enabled"))
			}
			return nil
		},
	}
}

// settingSetter validates value for key before any lock is taken and returns
// the change to apply.
func settingSetter(key, value string) (func(*home.Settings), error) {
	switch key {
	case "label":
		if err := validLabel(value); err != nil {
			return nil, err
		}
		return func(s *home.Settings) { s.Label = value }, nil
	case "auto_push":
		b, err := parseBool(key, value)
		return func(s *home.Settings) { s.AutoPush = b }, err
	case "accept_operations":
		b, err := parseBool(key, value)
		return func(s *home.Settings) { s.AcceptOperations = b }, err
	case "schema_version", "disabled_configurations", "sources", "copy_mode":
		return nil, fail(exitUsage, key+" cannot be changed with config set", "settable keys: "+settableKeys)
	}
	return nil, fail(exitUsage, fmt.Sprintf("unknown setting %q", key), "settable keys: "+settableKeys)
}

func parseBool(key, value string) (bool, error) {
	b, err := strconv.ParseBool(value)
	if err != nil {
		return false, fail(exitUsage, fmt.Sprintf("invalid value %q for %s", value, key), "use true or false")
	}
	return b, nil
}

func validLabel(label string) error {
	if strings.TrimSpace(label) == "" || strings.ContainsAny(label, "\r\n") {
		return fail(exitUsage, "the label must be one non-empty line", "")
	}
	return nil
}

// loadSettings reads the settings file, turning an unreadable file into exit 10.
func (inv *invocation) loadSettings() (home.Settings, error) {
	s, err := home.LoadSettings(inv.dirs.Home)
	if err != nil {
		return s, fail(exitInternal, err.Error(), "fix "+home.SettingsPath(inv.dirs.Home)+" or delete it to start from defaults")
	}
	return s, nil
}

// label is the effective machine label: the setting, or the hostname until set.
func (inv *invocation) label(s home.Settings) string {
	if s.Label != "" {
		return s.Label
	}
	return home.Hostname(inv.env)
}

func (inv *invocation) emitSettings(s home.Settings) {
	s.Label = inv.label(s)
	inv.out.emit(settingsEvent{event: newEvent("settings"), Settings: s})
}

type settingRow struct{ key, value string }

// printSettings writes the key-value table of config list. Booleans are
// painted by value and an empty value is shown as (none), so that a blank
// cell is never mistaken for a missing row.
func (inv *invocation) printSettings(s home.Settings) {
	t := &table{}
	for _, row := range inv.settingRows(s) {
		t.add(c(row.key, label), valueCell(row.value))
	}
	inv.out.render(t, "")
}

func valueCell(value string) cell {
	switch value {
	case "":
		return c("(none)", muted)
	case "true":
		return c(value, okStyle)
	case "false":
		return c(value, muted)
	}
	return c(value, plain)
}

func (inv *invocation) settingRows(s home.Settings) []settingRow {
	return []settingRow{
		{"schema_version", strconv.Itoa(s.SchemaVersion)},
		{"label", inv.label(s)},
		{"auto_push", strconv.FormatBool(s.AutoPush)},
		{"accept_operations", strconv.FormatBool(s.AcceptOperations)},
		{"disabled_configurations", strings.Join(s.DisabledConfigurations, ", ")},
		{"sources", compact(s.Sources)},
		{"copy_mode", compact(s.CopyMode)},
	}
}

func compact(raw json.RawMessage) string {
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		return string(raw)
	}
	return b.String()
}
