package cli

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
	"github.com/grundmanise/agentx/apps/cli/internal/weburl"
)

// kindPlugin is the kind of a skill a plugin provides, which the library
// does not hold and no branch records.
const kindPlugin = "plugin"

// forkSource is the skill a fork is made of: a managed skill, a fork or a
// skill a plugin provides, for skill fork, and an unmanaged skill, of the
// library or of an agent client's own skills directory, for agentx adopt.
type forkSource struct {
	kind   string            // lineage.KindManaged, lineage.KindFork, lineage.KindUnmanaged or kindPlugin
	name   string            // its name: its library entry's, or its directory's in the plugin or the client
	rec    lineage.Record    // its branch, for a managed skill or a fork
	lib    scan.LibrarySkill // its library entry, when held
	held   bool              // the library holds it
	plugin string            // the plugin that provides it
	// marketplace is where the plugin was installed from, and url where
	// it can be seen on the web, each "" when the scan does not know.
	marketplace, url string
	dir              string // the directory its content is read from, for every kind but a fork
}

// forkMode is what a run of the fork machinery is for. It decides the
// wording of a refusal, the hints, the subject of the creation commit and
// the report, and whether an unmanaged skill is taken.
type forkMode int

const (
	modeFork   forkMode = iota // skill fork
	modeRename                 // the first step of skill rename, which removes the source next
	modeAdopt                  // agentx adopt, which takes an unmanaged skill over as it is
)

// takesUnmanaged reports whether a run in this mode takes an unmanaged
// skill: adopt does, and skill fork and skill rename send it to adopt.
// Every place that decides it asks here.
func (m forkMode) takesUnmanaged() bool { return m == modeAdopt }

// done is what the mode does to a skill, as a refusal says it of one it
// did not do it to.
func (m forkMode) done() string {
	if m == modeAdopt {
		return "adopted"
	}
	return "forked"
}

// adoptCommand is the agentx adopt command a line tells the user to run on
// the skill called name, quoted for a shell.
func adoptCommand(name string) string {
	return "agentx adopt --skill " + shellWord(name)
}

// unmanagedRefusal refuses the unmanaged skill called name for a mode that
// does not take one, in the words of the command that named it.
func unmanagedRefusal(name string, mode forkMode) error {
	message := sanitised(name) + " is not managed by agentx, so it cannot be forked"
	if mode == modeRename {
		message = sanitised(name) + " is not a skill of the account remote, so it cannot be renamed"
	}
	return fail(exitRefused, message, "adopt it first with '"+adoptCommand(name)+"'")
}

// forkSourceOf finds the skill arg names. The library comes first, its
// entry's branch saying what it is; then the account repo, for a fork
// whose library entry is gone, which is forked from its branch all the
// same; then the plugins the scan found. A plugin's skill is named as
// <plugin>:<skill>, its plugin and its directory. scanned is the scan the plugins are read from, run
// only when the name is none of the library's or the account repo's. A
// library entry no branch records is unmanaged, and refused unless mode
// takes one, see forkMode.takesUnmanaged.
func (inv *invocation) forkSourceOf(arg string, records map[string]lineage.Record, scanned func() (scan.Inventory, error), mode forkMode) (forkSource, error) {
	if lib, ok := librarySkill(inv.dirs.Library, arg); ok {
		src := forkSource{kind: lineage.KindUnmanaged, name: arg, lib: lib, held: true, dir: lib.ResolvedPath}
		if rec, ok := records[arg]; ok {
			src.kind, src.rec = rec.Kind, rec
		} else if !mode.takesUnmanaged() {
			return forkSource{}, unmanagedRefusal(arg, mode)
		}
		return src, nil
	}
	if rec, ok := records[arg]; ok {
		if rec.Kind == lineage.KindManaged {
			what, wayOut := skillContext{records: records}.absentNotice(inv, arg)
			return forkSource{}, fail(exitRefused, what+", so there is nothing to fork", wayOut)
		}
		return forkSource{kind: lineage.KindFork, name: arg, rec: rec}, nil
	}
	inventory, err := scanned()
	if err != nil {
		return forkSource{}, err
	}
	o, err := pluginOccurrence(inventory, arg, "fork")
	if err != nil {
		return forkSource{}, err
	}
	src := forkSource{kind: kindPlugin, name: filepath.Base(o.Path), plugin: o.Plugin, dir: o.ResolvedPath}
	for _, p := range inventory.Plugins {
		if p.Name == o.Plugin && p.Configuration == o.Configuration && src.url == "" {
			src.marketplace, src.url = p.Marketplace, p.URL
		}
	}
	return src, nil
}

// origin is what the creation commit of a fork of src records of where it
// came from: the skill's name, and for a plugin's skill the plugin, its
// marketplace and its URL, each left out when a trailer cannot carry it.
// A managed skill has none, since the import commit the fork starts from
// records its source, and a name that cannot be recorded leaves none.
func (src forkSource) origin() lineage.Origin {
	if src.kind == lineage.KindManaged || !lineage.IsOriginName(src.name) {
		return lineage.Origin{}
	}
	o := lineage.Origin{Skill: src.name}
	if src.kind == kindPlugin && lineage.IsOriginName(src.plugin) {
		o.Plugin = src.plugin
		if lineage.IsOriginName(src.marketplace) {
			o.Marketplace = src.marketplace
		}
		if weburl.Is(src.url) {
			o.URL = src.url
		}
	}
	return o
}

// pluginOccurrence is the plugin's skill arg names among what the scan
// found: for <plugin>:<skill>, the directory called skill inside that
// plugin. A plugin's skill is named that way alone, so one name never means
// two skills: a bare name the plugins provide is refused with the plugins
// named and the qualified name to use instead, in the words of the command
// verb, and a name no plugin provides is not found. The plugin's own copies
// in several configurations are one skill, read from the first by
// configuration. Pure.
func pluginOccurrence(inventory scan.Inventory, arg, verb string) (scan.Occurrence, error) {
	plugin, skill, qualified := strings.Cut(arg, ":")
	if !qualified {
		names := pluginsProviding(inventory, arg)
		if len(names) == 0 {
			return scan.Occurrence{}, notHeld(arg)
		}
		message := sanitised(arg) + " is provided by the plugin " + sanitised(names[0])
		if len(names) > 1 {
			message = sanitised(arg) + " is provided by several plugins: " + sanitised(strings.Join(names, ", "))
		}
		return scan.Occurrence{}, fail(exitRefused, message+", so name it with its plugin",
			"name it as <plugin>:"+sanitised(arg)+", such as '"+skillCommand(verb, names[0]+":"+arg)+"'")
	}
	var found []scan.Occurrence
	for _, o := range pluginSkills(inventory, skill) {
		if o.Plugin == plugin {
			found = append(found, o)
		}
	}
	if len(found) == 0 {
		return scan.Occurrence{}, notHeld(arg)
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].Configuration != found[j].Configuration {
			return found[i].Configuration < found[j].Configuration
		}
		return found[i].Path < found[j].Path
	})
	return found[0], nil
}

// pluginSkills are the occurrences of the scan in a plugin, for the user's
// scope, whose skill directory is called skill.
func pluginSkills(inventory scan.Inventory, skill string) []scan.Occurrence {
	var found []scan.Occurrence
	for _, node := range inventory.Skills {
		for _, o := range node.Occurrences {
			if o.Plugin != "" && o.Scope == "user" && filepath.Base(o.Path) == skill {
				found = append(found, o)
			}
		}
	}
	return found
}

// pluginsProviding are the plugins that provide a skill directory called
// skill, sorted.
func pluginsProviding(inventory scan.Inventory, skill string) []string {
	seen := map[string]bool{}
	var names []string
	for _, o := range pluginSkills(inventory, skill) {
		if !seen[o.Plugin] {
			seen[o.Plugin] = true
			names = append(names, o.Plugin)
		}
	}
	sort.Strings(names)
	return names
}

// notHeld is the failure for a name neither the library nor any plugin holds.
func notHeld(arg string) error {
	return fail(exitNotFound, fmt.Sprintf("neither the library nor any plugin holds a skill called %q", sanitised(arg)),
		"run 'agentx skill list' to see what the library holds, or 'agentx scan' to see the plugins' skills")
}

// pluginConfigurations are the configurations of the scan that have the
// plugin and do not record it as turned off, sorted by id: the ones a fork
// of one of its skills is placed into and noted for.
func pluginConfigurations(inventory scan.Inventory, plugin string) []string {
	seen := map[string]bool{}
	var ids []string
	for _, p := range inventory.Plugins {
		if p.Name != plugin || (p.Enabled != nil && !*p.Enabled) || seen[p.Configuration] {
			continue
		}
		seen[p.Configuration] = true
		ids = append(ids, p.Configuration)
	}
	sort.Strings(ids)
	return ids
}
