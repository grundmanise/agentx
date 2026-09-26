package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

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
		Short:       "Read and change the settings",
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
			err = home.Mutate(inv.dirs.Home, inv.refs(cmd.Context()), func() error {
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
// Enabling takes --place-all, which also places every skill the library
// holds into the configuration; without it, enabling only decides where
// future installs go.
func newEnableCommand(inv *invocation, use, short string) *cobra.Command {
	disable := use == "disable"
	var placeAll bool
	cmd := &cobra.Command{
		Use:   use + " <configuration>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := inv.detectedConfiguration(args[0]); err != nil {
				return err
			}
			if placeAll {
				return inv.enableAndPlaceAll(cmd.Context(), args[0])
			}
			var s home.Settings
			err := home.Mutate(inv.dirs.Home, inv.refs(cmd.Context()), func() error {
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
			inv.printEnabled(args[0], !disable)
			return nil
		},
	}
	if !disable {
		cmd.Flags().BoolVar(&placeAll, "place-all", false, "also place every skill the library holds into the configuration")
	}
	return cmd
}

// printEnabled writes the one confirmation line of config enable or disable.
func (inv *invocation) printEnabled(id string, enabled bool) {
	state := inv.out.paint(warnStyle, "disabled")
	if enabled {
		state = inv.out.paint(okStyle, "enabled")
	}
	inv.out.done(inv.out.paint(label, id) + " is now " + state)
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

// labelLimit bounds the machine label. A label is the name of a computer
// for a person to read: it is printed by config list, carried in every
// settings event and in every export, and shown beside the machine
// wherever the desktop app lists it. Nothing needs more than this, and
// without a bound a settings file can be made megabytes long: not through
// config set label, whose argument ARG_MAX bounds, but through an import,
// whose label comes out of a file.
const labelLimit = 256

// validLabel is the rule every route to the label is held to: config set
// label, machine rename and the label an import restores. One rule, one
// gate, whoever chose the bytes.
//
// No control character: the label is printed, put into the question
// another machine's import asks, and handed to the desktop app, and while
// the user choosing an escape sequence about their own machine is their
// business, an import moves that choice to whoever wrote the document,
// which is the reasoning the contract already applies to a source URL.
// Keeping the rule here rather than at the import keeps the settings file
// agentx wrote one an import restores byte for byte.
func validLabel(label string) error {
	if strings.TrimSpace(label) == "" || strings.IndexFunc(label, unicode.IsControl) >= 0 {
		return fail(exitUsage, "the label must be one non-empty line with no control character in it", "")
	}
	if len(label) > labelLimit {
		return fail(exitUsage, fmt.Sprintf("the label must be at most %d bytes, and this one is %d", labelLimit, len(label)), "")
	}
	return nil
}

// configurationIDPattern is the shape of a configuration id: the slug of a
// client the registry knows, lowercase letters and digits in
// hyphen-separated parts, such as claude-code. Every registered slug has
// it, which a test holds the registry to.
var configurationIDPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// configurationIDLimit bounds one, far above the longest slug there is.
const configurationIDLimit = 64

// configurationID reports whether id could name a configuration. What is
// checked is the shape and not membership of this build's registry: a
// settings file written where a newer agentx knows a client this one does
// not names a configuration that is real there, and it is not agentx's to
// drop. The shape is what keeps an escape sequence or a paragraph of text
// out of a list config list prints and skill place reads.
func configurationID(id string) bool {
	return len(id) <= configurationIDLimit && configurationIDPattern.MatchString(id)
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
		{"sources", compactValue(s.Sources)},
		{"copy_mode", compact(s.CopyMode)},
	}
}

// compactValue renders v as compact JSON.
func compactValue(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func compact(raw json.RawMessage) string {
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		return string(raw)
	}
	return b.String()
}
