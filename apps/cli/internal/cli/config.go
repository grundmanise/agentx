package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
)

const settableKeys = "label, accept_operations, ignore_system_files"

type settingsEvent struct {
	event
	Settings home.Settings `json:"settings"`
}

func newConfigCommand(inv *invocation) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "config",
		Short:       "Read and change the settings",
		Annotations: map[string]string{annotationGroup: "true", annotationSection: "settings"},
		Args:        cobra.NoArgs,
		Example: examples(
			"agentx config list", "every setting",
			`agentx config set label "work laptop"`, "name this machine",
			"agentx config disable cursor", "keep new skills out of one client",
			"agentx config enable cursor --place-all", "enable a client and place every skill in it",
			"agentx config placement cursor copy", "place copies in one client instead of symlinks",
			"agentx config remove adal", "delete the configuration directory of a client, after confirming",
		),
		RunE: needSubcommand(inv, "no config command given", "run 'agentx config --help' to list commands"),
	}
	cmd.AddCommand(&cobra.Command{
		Use:         "list",
		Short:       "Print every setting",
		Annotations: map[string]string{annotationSection: "settings", annotationRead: "true"},
		Args:        cobra.NoArgs,
		Example:     examples("agentx config list", "every setting and its value"),
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
		Use:         "get <key>",
		Short:       "Print one setting",
		Annotations: map[string]string{annotationSection: "settings", annotationRead: "true"},
		Args:        cobra.ExactArgs(1),
		Example:     examples("agentx config get label", "one setting"),
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
			return fail(exitUsage, fmt.Sprintf("unknown setting %q", args[0]), "keys: schema_version, "+settableKeys+", disabled_configurations, copy_configurations, sources, copy_mode")
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:         "set <key> <value>",
		Short:       "Change label, accept_operations or ignore_system_files",
		Annotations: map[string]string{annotationSection: "settings"},
		Args:        cobra.ExactArgs(2),
		Example: examples(
			`agentx config set label "work laptop"`, "name this machine",
			"agentx config set ignore_system_files true", "keep .DS_Store and swap files out of skill commits",
		),
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
				if err := home.SaveSettings(inv.dirs.Home, s); err != nil {
					return err
				}
				// The list follows the setting at once in an account repo
				// that exists; one created later gets it with the first
				// git run over a skill directory.
				gitDir := gitx.AccountRepoPath(inv.dirs.Home)
				if _, err := os.Stat(gitDir); err == nil && args[0] == "ignore_system_files" {
					return home.SyncExclude(gitDir, s.IgnoreSystemFiles)
				}
				return nil
			})
			if err != nil {
				return err
			}
			inv.emitSettings(s)
			inv.out.done(inv.out.paint(label, args[0]) + " is now " + inv.out.paint(heading, args[1]))
			return nil
		},
	})
	cmd.AddCommand(newEnableCommand(inv, "enable", "Select a client for placements by default"))
	cmd.AddCommand(newEnableCommand(inv, "disable", "Leave a client out of placements by default"))
	cmd.AddCommand(newPlacementCommand(inv))
	cmd.AddCommand(newConfigRemoveCommand(inv))
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
	example := examples(
		"agentx config enable cursor", "new installs also go to Cursor",
		"agentx config enable cursor --place-all", "and place every skill the library holds in it now",
	)
	if disable {
		example = examples("agentx config disable cursor", "new installs skip Cursor")
	}
	cmd := &cobra.Command{
		Use:         use + " <configuration>",
		Short:       short,
		Annotations: map[string]string{annotationSection: "settings"},
		Args:        cobra.ExactArgs(1),
		Example:     example,
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

// newPlacementCommand builds config placement, which chooses what agentx
// places into one configuration from now on: a symlink to the library, or
// a copy. On its own it edits the copy_configurations list and nothing on
// disk: a placement already made stays what it is until skill place makes
// it again. With --convert the same mutation also turns the placements
// already there into the mode, see setPlacementAndConvert. A configuration
// whose client reads the library has no choice to make, since the library
// entry is its placement.
func newPlacementCommand(inv *invocation) *cobra.Command {
	var convert bool
	cmd := &cobra.Command{
		Use:         "placement <configuration> <symlink|copy>",
		Short:       "Choose whether a client gets symlinks or copies",
		Annotations: map[string]string{annotationSection: "settings", annotationSynopsis: "<id> <mode>"},
		Long: "Choose what agentx places into one agent configuration from now on: symlink,\n" +
			"a symlink to the library directory, which is the default, or copy, a copy of\n" +
			"it, as --copy makes. Placements already on disk stay as they are unless\n" +
			"--convert is given: then every symlink to the library there becomes a copy,\n" +
			"or every copy agentx placed there that holds the library's version becomes\n" +
			"a symlink. A copy that is different from the library is kept as it is. No\n" +
			"skill is placed that was not placed there already. A client that reads the\n" +
			"library has no choice to make: the library entry is its placement.",
		Args: cobra.ExactArgs(2),
		Example: examples(
			"agentx config placement cursor copy", "skills placed in Cursor from now on are copies",
			"agentx config placement cursor symlink", "and symlinks to the library again",
			"agentx config placement cursor copy --convert", "and turn the symlinks already in Cursor into copies",
		),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, mode := args[0], args[1]
			if mode != scan.PlacementSymlink && mode != scan.PlacementCopy {
				return fail(exitUsage, fmt.Sprintf("unknown placement %q", mode), "use symlink or copy")
			}
			if err := inv.detectedConfiguration(id); err != nil {
				return err
			}
			if inv.configurationReadsLibrary(id) {
				return fail(exitUsage, id+" reads the library, so its placement is the library entry",
					"it sees every library skill there; choose symlink or copy for a configuration that does not read the library")
			}
			if convert {
				return inv.setPlacementAndConvert(cmd.Context(), id, mode == scan.PlacementCopy)
			}
			var s home.Settings
			err := home.Mutate(inv.dirs.Home, inv.refs(cmd.Context()), func() error {
				var err error
				if s, err = inv.loadSettings(); err != nil {
					return err
				}
				setCopyConfiguration(&s, id, mode == scan.PlacementCopy)
				return home.SaveSettings(inv.dirs.Home, s)
			})
			if err != nil {
				return err
			}
			inv.emitSettings(s)
			inv.out.done(inv.out.paint(label, id) + " now places " + inv.out.paint(heading, placedWord(mode == scan.PlacementCopy)))
			return nil
		},
	}
	cmd.Flags().BoolVar(&convert, "convert", false,
		"also turn the placements already in the configuration into the mode: symlinks to the library into copies, or unchanged copies into symlinks")
	return cmd
}

// setCopyConfiguration puts id in copy_configurations when it places
// copies and takes it out when it places symlinks.
func setCopyConfiguration(s *home.Settings, id string, copies bool) {
	s.CopyConfigurations = slices.DeleteFunc(s.CopyConfigurations, func(x string) bool { return x == id })
	if copies {
		s.CopyConfigurations = append(s.CopyConfigurations, id)
		slices.Sort(s.CopyConfigurations)
	}
}

// placedWord is what a configuration that places copies, or symlinks,
// gets, in the words config placement confirms it with.
func placedWord(copies bool) string {
	if copies {
		return "copies"
	}
	return "symlinks"
}

// configurationReadsLibrary reports whether the detected configuration id
// is one whose client reads the library.
func (inv *invocation) configurationReadsLibrary(id string) bool {
	for _, c := range scan.Detect(inv.dirs) {
		if c.Slug() == id {
			return scan.ReadsLibrary(c, inv.dirs)
		}
	}
	return false
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
	case "accept_operations":
		b, err := parseBool(key, value)
		return func(s *home.Settings) { s.AcceptOperations = b }, err
	case "ignore_system_files":
		b, err := parseBool(key, value)
		return func(s *home.Settings) { s.IgnoreSystemFiles = b }, err
	case "schema_version", "disabled_configurations", "copy_configurations", "sources", "copy_mode":
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

// loadSettings reads the settings file, turning an unreadable file, and
// one a later agentx wrote, into exit 10.
func (inv *invocation) loadSettings() (home.Settings, error) {
	s, err := home.LoadSettings(inv.dirs.Home)
	var newer *home.NewerSettingsError
	switch {
	case errors.As(err, &newer):
		// Deleting the file would lose what the later version keeps in it,
		// so the hint does not offer it.
		return s, refuse(exitInternal, err.Error(), "upgrade agentx to the version that wrote it").wrap(err)
	case err != nil:
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
		{"accept_operations", strconv.FormatBool(s.AcceptOperations)},
		{"ignore_system_files", strconv.FormatBool(s.IgnoreSystemFiles)},
		{"disabled_configurations", strings.Join(s.DisabledConfigurations, ", ")},
		{"copy_configurations", strings.Join(s.CopyConfigurations, ", ")},
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
