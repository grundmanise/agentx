package cli

import (
	"slices"
	"sort"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// settingsEdit collects the machine-settings changes of one command so that
// a mutation stages the file once however many things the command changed.
// Two replacements of one path in one journal would build the second from
// the content the first displaced and lose it, which is what a command that
// both enables a configuration and records copy modes would do.
type settingsEdit struct {
	s       home.Settings
	modes   map[string][]string
	changed bool
}

// beginSettings reads the settings for a command that is about to change
// them. Call it under the exclusive lock, so that what is staged is built
// from what the file holds now.
func (inv *invocation) beginSettings() (*settingsEdit, error) {
	s, err := inv.loadSettings()
	if err != nil {
		return nil, err
	}
	modes, err := inv.copyModes(s)
	if err != nil {
		return nil, err
	}
	return &settingsEdit{s: s, modes: modes}, nil
}

// copyModes reads copy_mode out of settings already loaded, skill name to
// the configurations that hold a copy of it, turning an entry of another
// shape into exit 10 as an unreadable settings file is.
func (inv *invocation) copyModes(s home.Settings) (map[string][]string, error) {
	modes, err := s.CopyModes()
	if err != nil {
		return nil, fail(exitInternal, "parse "+home.SettingsPath(inv.dirs.Home)+": copy_mode must map skill names to configuration ids",
			"fix copy_mode in the settings file")
	}
	return modes, nil
}

// addCopies records that the named configurations hold a copy of the skill
// instead of a symlink.
func (e *settingsEdit) addCopies(name string, ids []string) {
	for _, id := range ids {
		if !slices.Contains(e.modes[name], id) {
			e.modes[name] = append(e.modes[name], id)
			e.changed = true
		}
	}
	sort.Strings(e.modes[name])
}

// dropCopies takes the named configurations out of the skill's copy modes,
// for a removal that took those placements away. A skill left with no copy
// anywhere leaves no entry behind, so the file says what is rather than
// what once was.
func (e *settingsEdit) dropCopies(name string, ids []string) {
	have, ok := e.modes[name]
	if !ok {
		return
	}
	kept := make([]string, 0, len(have))
	for _, id := range have {
		if !slices.Contains(ids, id) {
			kept = append(kept, id)
		}
	}
	if len(kept) == len(have) {
		return
	}
	e.changed = true
	if len(kept) == 0 {
		delete(e.modes, name)
		return
	}
	e.modes[name] = kept
}

// copiesOf are the configurations the settings record as holding a copy of
// the skill, which is what tells a copy agentx placed from a directory it
// did not.
func (e *settingsEdit) copiesOf(name string) []string { return e.modes[name] }

// dropSkill takes every copy mode of the skill away, for a removal that
// takes the skill off the machine.
func (e *settingsEdit) dropSkill(name string) {
	if _, ok := e.modes[name]; ok {
		delete(e.modes, name)
		e.changed = true
	}
}

// enable takes a configuration out of disabled_configurations. It is always
// a write, as `config enable` on its own is: the user asked for the state,
// and a command that reports it changed something rewrites the file.
func (e *settingsEdit) enable(id string) {
	e.s.DisabledConfigurations = slices.DeleteFunc(e.s.DisabledConfigurations, func(x string) bool { return x == id })
	e.changed = true
}

// stage records the one settings write of the command, and nothing when
// nothing changed, so that a command that changed no setting leaves no
// journal entry for the file.
func (e *settingsEdit) stage(m *home.Mutation, dir string) error {
	if !e.changed {
		return nil
	}
	if err := e.s.SetCopyModes(e.modes); err != nil {
		return err
	}
	b, err := home.MarshalSettings(e.s)
	if err != nil {
		return err
	}
	return m.ReplaceFile(home.SettingsPath(dir), b)
}

// settings is what the file now holds, for the event a command emits after
// its mutation.
func (e *settingsEdit) settings() home.Settings { return e.s }
