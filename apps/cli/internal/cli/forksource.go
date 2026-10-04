package cli

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
)

// kindPlugin is the kind of a skill a plugin provides, which the library
// does not hold and no branch records.
const kindPlugin = "plugin"

// forkSource is the skill skill fork makes a fork of: a managed skill, a
// fork, an unmanaged skill of the library, or a skill a plugin provides.
type forkSource struct {
	kind   string            // lineage.KindManaged, lineage.KindFork, lineage.KindUnmanaged or kindPlugin
	name   string            // its name: its library entry's, or its directory's in the plugin
	rec    lineage.Record    // its branch, for a managed skill or a fork
	lib    scan.LibrarySkill // its library entry, when held
	held   bool              // the library holds it
	plugin string            // the plugin that provides it
	dir    string            // the directory its content is read from, for every kind but a fork
}

// forkSourceOf finds the skill arg names. The library comes first, its
// entry's branch saying what it is; then the account repo, for a fork
// whose library entry is gone, which is forked from its branch all the
// same; then the plugins the scan found. A plugin's skill is named by its
// directory, or, where several plugins provide one of that name, as
// <plugin>:<skill>. scanned is the scan the plugins are read from, run
// only when the name is none of the library's or the account repo's.
func (inv *invocation) forkSourceOf(arg string, records map[string]lineage.Record, scanned func() (scan.Snapshot, error)) (forkSource, error) {
	if lib, ok := librarySkill(inv.dirs.Library, arg); ok {
		src := forkSource{kind: lineage.KindUnmanaged, name: arg, lib: lib, held: true, dir: lib.ResolvedPath}
		if rec, ok := records[arg]; ok {
			src.kind, src.rec = rec.Kind, rec
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
	snap, err := scanned()
	if err != nil {
		return forkSource{}, err
	}
	o, err := pluginOccurrence(snap, arg)
	if err != nil {
		return forkSource{}, err
	}
	return forkSource{kind: kindPlugin, name: filepath.Base(o.Path), plugin: o.Plugin, dir: o.ResolvedPath}, nil
}

// pluginOccurrence is the plugin's skill arg names among what the scan
// found: a skill directory called arg inside one plugin, or, for
// <plugin>:<skill>, the directory called skill inside that plugin. A name
// several plugins provide is refused with the plugins named, and the
// plugin's own copies in several configurations are one skill, read from
// the first by configuration. Pure.
func pluginOccurrence(snap scan.Snapshot, arg string) (scan.Occurrence, error) {
	plugin, skill, qualified := strings.Cut(arg, ":")
	if !qualified {
		plugin, skill = "", arg
	}
	var found []scan.Occurrence
	plugins := map[string]bool{}
	for _, node := range snap.Skills {
		for _, o := range node.Occurrences {
			if o.Plugin == "" || o.Scope != "user" || filepath.Base(o.Path) != skill || (qualified && o.Plugin != plugin) {
				continue
			}
			found = append(found, o)
			plugins[o.Plugin] = true
		}
	}
	switch len(plugins) {
	case 0:
		return scan.Occurrence{}, fail(exitNotFound, fmt.Sprintf("neither the library nor any plugin holds a skill called %q", sanitised(arg)),
			"run 'agentx skill list' to see what the library holds, or 'agentx scan' to see the plugins' skills")
	case 1:
	default:
		names := make([]string, 0, len(plugins))
		for p := range plugins {
			names = append(names, p)
		}
		sort.Strings(names)
		return scan.Occurrence{}, fail(exitRefused, sanitised(skill)+" is provided by several plugins: "+sanitised(strings.Join(names, ", ")),
			"name one as <plugin>:"+sanitised(skill)+", such as '"+skillCommand("fork", names[0]+":"+skill)+"'")
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].Configuration != found[j].Configuration {
			return found[i].Configuration < found[j].Configuration
		}
		return found[i].Path < found[j].Path
	})
	return found[0], nil
}

// pluginConfigurations are the configurations of the scan that have the
// plugin and do not record it as turned off, sorted by id: the ones a fork
// of one of its skills is placed into and noted for.
func pluginConfigurations(snap scan.Snapshot, plugin string) []string {
	seen := map[string]bool{}
	var ids []string
	for _, p := range snap.Plugins {
		if p.Name != plugin || (p.Enabled != nil && !*p.Enabled) || seen[p.Configuration] {
			continue
		}
		seen[p.Configuration] = true
		ids = append(ids, p.Configuration)
	}
	sort.Strings(ids)
	return ids
}
