package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
)

// The states of a config_removal event.
const (
	removalRemovable = "removable" // the preview: a removal would delete it
	removalRemoved   = "removed"   // the run deleted it
	removalKept      = "kept"      // it holds something a removal would lose, and stays
)

// osMetadata are the files an operating system's file manager writes into
// a directory on its own, by their exact names. They are the only files a
// removal passes over, while ignore_system_files is on: the patterns that
// setting also covers (editor backups and swap files, the leftovers of a
// network or FUSE file system, AppleDouble files) match names a person or
// a program can give a file that matters, so those hold a configuration
// back like any other file.
var osMetadata = []string{".DS_Store", "Thumbs.db", "ehthumbs.db", "desktop.ini"}

// holdsLimit bounds what one configuration is said to hold: a client in
// use holds hundreds of files, and the first few say why it stays.
const holdsLimit = 6

// configRemovalEvent is one configuration config remove judged: whether its
// directory can go, and what holds it back when it cannot.
type configRemovalEvent struct {
	event
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Path  string   `json:"path"`  // the configuration directory, the one path a removal deletes
	State string   `json:"state"` // removable, removed or kept
	Holds []string `json:"holds"` // what a removal would lose, or why it may not delete the directory; empty unless kept
	With  []string `json:"with"`  // the other configurations kept in the same directory, which go with it
}

func newConfigRemoveCommand(inv *invocation) *cobra.Command {
	return &cobra.Command{
		Use:         "remove [<configuration>...]",
		Short:       "Delete the leftover configuration of a client",
		Annotations: map[string]string{annotationSection: "settings", annotationSynopsis: "[<id>...]"},
		Long: "Delete the configuration directory of each client named, so that agentx no\n" +
			"longer detects it, and take it out of the settings. Without a name, list every\n" +
			"detected configuration with whether it can be removed, and change nothing.\n\n" +
			"A configuration is removed only when nothing in its directory would be lost:\n" +
			"symlinks in its skills directory, copies of skills the library or another\n" +
			"skills directory that stays also holds with the same content, and empty\n" +
			"directories. A skill that is only there, MCP server declarations, plugins,\n" +
			"settings, history or any other file holds the configuration back: the command\n" +
			"names what it found and removes nothing, of any configuration named.\n\n" +
			"Only the configuration directory is deleted. The library, and a skills\n" +
			"directory outside the configuration directory, stay as they are.",
		Example: examples(
			"agentx config remove", "which configurations can be removed, and what the others hold",
			"agentx config remove adal augment", "delete the leftover directories of two clients",
		),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return inv.previewConfigRemoval()
			}
			return inv.configRemove(cmd, args)
		},
	}
}

// removalScope is what removing one client's configuration touches, read
// off the registry's paths alone: the one directory it deletes, and what
// else lives in or reads that directory.
type removalScope struct {
	id, name string
	// dir is the configuration directory. A removal deletes it and nothing
	// else: never a parent, the library, or a skills directory outside it.
	dir string
	// skills are the client's own skills directories inside dir, whose
	// entries are judged as skills. One outside dir, such as the library a
	// universal client reads, is not the configuration's to delete.
	skills []string
	// with are the other clients whose configuration directory is dir or
	// lies inside it: deleting dir deletes theirs, so they are removed
	// together with it.
	with []string
	// nested are the other clients whose configuration directory lies
	// inside dir, by that directory.
	nested map[string]string
	// readers are the other clients that read a skills directory inside
	// dir without sharing dir, by that directory.
	readers map[string][]string
	// protected says why dir may never be deleted, "" when it may: it is,
	// or holds, a directory agentx or the user works from.
	protected string
}

// beneath reports whether path is dir or lies below it, as the two are
// spelled.
func beneath(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// removalScopeOf is the scope of removing c's configuration among clients,
// the registered ones. It reads no disk.
func removalScopeOf(c scan.Client, d home.Dirs, clients []scan.Client) removalScope {
	s := removalScope{id: c.Slug(), name: c.Name(), dir: filepath.Clean(c.ConfigDir(d)), nested: map[string]string{}, readers: map[string][]string{}}
	for _, p := range []struct{ path, what string }{
		{d.Library, "the library"}, {d.Home, "agentx home"}, {d.Config, "the configuration home of other programs"}, {d.User, "your home directory"},
	} {
		if beneath(filepath.Clean(p.path), s.dir) {
			s.protected = p.what
			break
		}
	}
	for _, dir := range c.SkillsDirs(d) {
		if dir = filepath.Clean(dir); dir != s.dir && beneath(dir, s.dir) {
			s.skills = append(s.skills, dir)
		}
	}
	for _, o := range clients {
		if o.Slug() == s.id {
			continue
		}
		other := filepath.Clean(o.ConfigDir(d))
		switch {
		case other == s.dir:
			s.with = append(s.with, o.Slug())
			continue
		case beneath(other, s.dir):
			s.with = append(s.with, o.Slug())
			s.nested[other] = o.Slug()
		}
		for _, dir := range o.SkillsDirs(d) {
			if dir = filepath.Clean(dir); beneath(dir, s.dir) && !beneath(dir, other) {
				s.readers[dir] = append(s.readers[dir], o.Slug())
			}
		}
	}
	sort.Strings(s.with)
	return s
}

// leadsTo reports whether a skills directory of s, or another client's
// configuration directory, lies below path, so that path is judged entry by
// entry and never as one empty directory.
func (s removalScope) leadsTo(path string) bool {
	for nested := range s.nested {
		if beneath(nested, path) {
			return true
		}
	}
	return slices.ContainsFunc(s.skills, func(skills string) bool { return beneath(skills, path) })
}

// registeredClients are the clients of the registry, in its order.
func registeredClients() []scan.Client {
	var clients []scan.Client
	for _, slug := range scan.Slugs() {
		if c, ok := scan.Lookup(slug); ok {
			clients = append(clients, c)
		}
	}
	return clients
}

// removalJudge decides, from the disk, whether a configuration directory
// holds anything a removal would lose.
type removalJudge struct {
	d        home.Dirs
	system   bool                   // ignore_system_files: the files of osMetadata do not count
	detected map[string]scan.Client // by slug
	named    map[string]bool        // the configurations this run covers
	// gone are the configuration directories this run has already found
	// removable: nothing in them counts as staying on the machine.
	gone    []string
	plugins map[string][]scan.InstalledPlugin // by slug, read once
	hashes  map[string]string                 // content hashes by path, each read once
	links   []skillLink                       // see allLinks, read once
}

// hash is contentHashAt, read once per path: the library's copy of a skill
// is compared with every configuration's.
func (j *removalJudge) hash(path string) string {
	if h, ok := j.hashes[path]; ok {
		return h
	}
	if j.hashes == nil {
		j.hashes = map[string]string{}
	}
	h := contentHashAt(path)
	j.hashes[path] = h
	return h
}

// judgeRemovals judges the named configurations, each as though the ones
// judged removable before it were already gone, so that the ones it calls
// removable can be removed together, or any of them alone. A configuration
// directory inside another is judged first. The events come back sorted by
// id.
func (inv *invocation) judgeRemovals(ids []string, system bool) []configRemovalEvent {
	j := &removalJudge{d: inv.dirs, system: system, detected: map[string]scan.Client{}, named: map[string]bool{}}
	for _, c := range scan.Detect(inv.dirs) {
		j.detected[c.Slug()] = c
	}
	clients := registeredClients()
	var scopes []removalScope
	for _, id := range ids {
		if c, ok := j.detected[id]; ok && !j.named[id] {
			j.named[id] = true
			scopes = append(scopes, removalScopeOf(c, inv.dirs, clients))
		}
	}
	sort.SliceStable(scopes, func(a, b int) bool {
		da, db := strings.Count(scopes[a].dir, string(filepath.Separator)), strings.Count(scopes[b].dir, string(filepath.Separator))
		if da != db {
			return da > db
		}
		return scopes[a].id < scopes[b].id
	})
	events := make([]configRemovalEvent, 0, len(scopes))
	for _, s := range scopes {
		ev := configRemovalEvent{event: newEvent("config_removal"), ID: s.id, Name: s.name, Path: s.dir, State: removalRemovable, Holds: []string{}, With: []string{}}
		for _, id := range s.with {
			if _, ok := j.detected[id]; ok {
				ev.With = append(ev.With, id)
			}
		}
		if holds := j.judge(s); len(holds) > 0 {
			ev.State, ev.Holds = removalKept, clipHolds(holds)
		} else {
			j.gone = append(j.gone, s.dir)
		}
		events = append(events, ev)
	}
	sort.Slice(events, func(a, b int) bool { return events[a].ID < events[b].ID })
	return events
}

// clipHolds keeps the first holdsLimit of holds and counts the rest.
func clipHolds(holds []string) []string {
	if len(holds) <= holdsLimit {
		return holds
	}
	return append(holds[:holdsLimit:holdsLimit], fmt.Sprintf("%d more", len(holds)-holdsLimit))
}

// judge is what the configuration of s holds that a removal would lose, or
// that forbids deleting its directory; nothing when it can be removed.
func (j *removalJudge) judge(s removalScope) []string {
	if s.protected != "" {
		return []string{s.protected}
	}
	if info, err := os.Lstat(s.dir); err != nil || !info.IsDir() {
		// A symlink holds nothing of its own: the directory it leads to is
		// the user's, kept somewhere they chose.
		return []string{"the directory is a symlink, which agentx leaves alone"}
	}
	// The same rule as the scope's, on the paths as they really are: a
	// library that is a symlink into the directory is spelled outside it.
	dir := realPath(s.dir)
	for _, p := range []struct{ path, what string }{
		{j.d.Library, "the library"}, {j.d.Home, "agentx home"}, {j.d.Config, "the configuration home of other programs"}, {j.d.User, "your home directory"},
	} {
		if beneath(realPath(p.path), dir) {
			return []string{p.what}
		}
	}
	var holds []string
	for _, id := range s.with {
		if c, ok := j.detected[id]; ok && !j.named[id] {
			holds = append(holds, "the configuration of "+c.Name()+" ("+id+"), which is not named")
		}
	}
	holds = append(holds, j.linksInto(s, dir)...)
	holds = append(holds, j.walk(s, s.dir)...)
	if len(holds) == 0 {
		// A removal captures the whole directory before it moves it, so a
		// file it cannot read stops the run: the judgment says so first.
		if at := unreadable(s.dir); at != "" {
			return []string{j.rel(s, at) + ", which cannot be read"}
		}
	}
	return holds
}

// unreadable is the first path in the directory dir that cannot be read as
// a removal reads it, to capture what it moves, and "" when all of it can.
func unreadable(dir string) string {
	_, err := home.Fingerprint(dir)
	if err == nil {
		return ""
	}
	var at *fs.PathError
	if errors.As(err, &at) {
		return at.Path
	}
	return dir
}

// skillLink is one symlink of a skills directory that stays: the entry, or
// the directory itself with name "", and where it really leads.
type skillLink struct {
	owner string // "the library", or the client's name
	id    string // the client's slug, "" for the library
	name  string
	real  string
}

// links are the symlinks in the library and in the skills directories of
// the detected clients, each resolved once however many configurations are
// judged.
func (j *removalJudge) allLinks() []skillLink {
	if j.links != nil {
		return j.links
	}
	j.links = []skillLink{}
	seen := map[string]bool{}
	add := func(owner, id, dir string) {
		dir = filepath.Clean(dir)
		if seen[dir] {
			return
		}
		seen[dir] = true
		if info, err := os.Lstat(dir); err != nil {
			return
		} else if info.Mode()&os.ModeSymlink != 0 {
			if real, err := filepath.EvalSymlinks(dir); err == nil {
				j.links = append(j.links, skillLink{owner: owner, id: id, real: real})
			}
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if e.Type()&os.ModeSymlink == 0 {
				continue
			}
			if real, err := filepath.EvalSymlinks(filepath.Join(dir, e.Name())); err == nil {
				j.links = append(j.links, skillLink{owner: owner, id: id, name: e.Name(), real: real})
			}
		}
	}
	add("the library's", "", j.d.Library)
	ids := make([]string, 0, len(j.detected))
	for id := range j.detected {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		c := j.detected[id]
		for _, dir := range c.SkillsDirs(j.d) {
			if !beneath(filepath.Clean(dir), filepath.Clean(c.ConfigDir(j.d))) {
				continue // the library, or another client's directory, listed under its owner
			}
			add(c.Name()+"'s", id, dir)
		}
	}
	return j.links
}

// linksInto are the skills that stay on the machine only as a link into
// the directory dir of s, as it really is: an entry of the library, or of
// the skills directory of a client that stays, that resolves into it.
// Deleting the directory would leave the link dangling, and the library,
// or that client, without the skill, whatever copy of it stays elsewhere.
func (j *removalJudge) linksInto(s removalScope, dir string) []string {
	var holds []string
	for _, l := range j.allLinks() {
		if l.id != "" {
			c := j.detected[l.id]
			own := filepath.Clean(c.ConfigDir(j.d))
			if l.id == s.id || beneath(own, s.dir) || j.leaving(own) {
				continue // it goes with this directory, or went before it
			}
		}
		if !beneath(l.real, dir) {
			continue
		}
		if l.name == "" {
			holds = append(holds, l.owner+" skills directory, which is a link into this directory")
		} else {
			holds = append(holds, l.owner+" skill "+sanitised(l.name)+", which is a link into this directory")
		}
	}
	return holds
}

// realPath is path made absolute with every symlink on it resolved, as far
// as it exists: the part that does not exist yet is kept as it is spelled.
func realPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = filepath.Clean(path)
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	parent := filepath.Dir(abs)
	if parent == abs {
		return abs
	}
	return filepath.Join(realPath(parent), filepath.Base(abs))
}

// passedOver reports whether a file of this name does not count: one of
// osMetadata, while ignore_system_files is on.
func (j *removalJudge) passedOver(name string) bool {
	return j.system && slices.Contains(osMetadata, name)
}

// leaving reports whether path is, or lies in, a configuration directory
// this run has already found removable.
func (j *removalJudge) leaving(path string) bool {
	return slices.ContainsFunc(j.gone, func(dir string) bool { return beneath(path, dir) })
}

// walk judges the entries of dir, a directory of s on the way to its
// skills directories or its root.
func (j *removalJudge) walk(s removalScope, dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []string{j.rel(s, dir) + ", which cannot be read"}
	}
	var holds []string
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		real := e.IsDir() // a symlink is no directory here
		switch {
		case j.leaving(path):
		case s.nested[path] != "":
			if c, ok := j.detected[s.nested[path]]; ok {
				holds = append(holds, "the configuration of "+c.Name()+" in "+j.rel(s, path))
			} else {
				holds = append(holds, j.rel(s, path))
			}
		case slices.Contains(s.skills, path):
			// A skills directory that is a symlink holds nothing of its own.
			if real {
				holds = append(holds, j.skills(s, path)...)
			} else if e.Type()&os.ModeSymlink == 0 {
				holds = append(holds, j.rel(s, path))
			}
		case real && s.leadsTo(path):
			holds = append(holds, j.walk(s, path)...)
		case real && j.holdsNothing(path):
		case e.Type().IsRegular() && j.passedOver(e.Name()):
		default:
			holds = append(holds, j.label(s, path))
		}
	}
	return holds
}

// rel names a path of the configuration by its place in the directory.
func (j *removalJudge) rel(s removalScope, path string) string {
	rel, err := filepath.Rel(s.dir, path)
	if err != nil || rel == "." {
		return "the directory"
	}
	return sanitised(filepath.ToSlash(rel))
}

// label names an entry that holds the configuration back by what the
// client keeps in it, where the registry says: MCP server declarations and
// plugins. Anything else is named by its path.
func (j *removalJudge) label(s removalScope, path string) string {
	rel := j.rel(s, path)
	c, ok := j.detected[s.id]
	if !ok {
		return rel
	}
	for _, cfg := range c.MCPConfigs(j.d) {
		if beneath(filepath.Clean(cfg.Path), path) {
			return "MCP server declarations in " + rel
		}
	}
	if j.plugins == nil {
		j.plugins = map[string][]scan.InstalledPlugin{}
	}
	if _, ok := j.plugins[s.id]; !ok {
		j.plugins[s.id] = c.Plugins(j.d, func(string) {})
	}
	n := 0
	for _, p := range j.plugins[s.id] {
		if beneath(filepath.Clean(p.Path), path) {
			n++
		}
	}
	if n > 0 {
		return plural(n, "plugin") + " in " + rel
	}
	return rel
}

// holdsNothing reports whether the directory at path holds no file at any
// depth: directories alone, and the files of osMetadata while they do not
// count.
func (j *removalJudge) holdsNothing(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	for _, e := range entries {
		switch {
		case e.IsDir():
			if !j.holdsNothing(filepath.Join(path, e.Name())) {
				return false
			}
		case e.Type().IsRegular() && j.passedOver(e.Name()):
		default:
			return false
		}
	}
	return true
}

// skills judges the entries of dir, a skills directory of s inside its
// configuration directory. A symlink holds nothing: what it leads to stays
// where it is, or is judged where it lives. A directory is accounted for
// when it is empty, or when it is a skill that the library, or another
// skills directory that stays, holds under the same name with the same
// content. A directory another client that stays reads is not the
// configuration's alone to delete once it holds anything.
func (j *removalJudge) skills(s removalScope, dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []string{j.rel(s, dir) + ", which cannot be read"}
	}
	var holds []string
	if len(entries) > 0 {
		for _, id := range s.readers[dir] {
			if c, ok := j.detected[id]; ok && !j.leaving(filepath.Clean(c.ConfigDir(j.d))) {
				holds = append(holds, j.rel(s, dir)+", which "+c.Name()+" reads too")
			}
		}
	}
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		switch {
		case e.Type()&os.ModeSymlink != 0:
		case e.IsDir() && j.holdsNothing(path):
		case e.IsDir():
			// Read before it is compared: a copy with a file that cannot be
			// read hashes as though the file were not there, which would
			// name the wrong reason, or none.
			if at := unreadable(path); at != "" {
				holds = append(holds, j.rel(s, at)+", which cannot be read")
				continue
			}
			hash := j.hash(path)
			switch {
			case hash == "":
				holds = append(holds, j.rel(s, path))
			case j.staysElsewhere(s, e.Name(), hash):
			case j.hash(filepath.Join(j.d.Library, e.Name())) != "":
				holds = append(holds, "the skill "+sanitised(e.Name())+", which differs from the library's")
			default:
				holds = append(holds, "the skill "+sanitised(e.Name())+", which nothing that stays on the machine holds")
			}
		case e.Type().IsRegular() && j.passedOver(e.Name()):
		default:
			holds = append(holds, j.rel(s, path))
		}
	}
	return holds
}

// staysElsewhere reports whether a skill of this name with this content
// stays on the machine once s is gone: in the library, or in a skills
// directory of a detected client, outside s and outside every configuration
// this run has already found removable. The copy is followed to where it
// really is, so that a link back into what is being deleted is no copy.
func (j *removalJudge) staysElsewhere(s removalScope, name, hash string) bool {
	dirs := []string{j.d.Library}
	for _, c := range j.detected {
		dirs = append(dirs, c.SkillsDirs(j.d)...)
	}
	seen := map[string]bool{}
	for _, dir := range dirs {
		real, err := filepath.EvalSymlinks(filepath.Join(dir, name))
		if err != nil || seen[real] || inside(real, s.dir) || beneath(real, s.dir) || j.leaving(real) {
			continue
		}
		seen[real] = true
		if slices.ContainsFunc(j.gone, func(gone string) bool { return inside(real, gone) }) {
			continue
		}
		if j.hash(real) == hash {
			return true
		}
	}
	return false
}

// previewConfigRemoval lists every detected configuration with whether it
// can be removed, and changes nothing.
func (inv *invocation) previewConfigRemoval() error {
	s, err := inv.loadSettings()
	if err != nil {
		return err
	}
	var ids []string
	for _, c := range scan.Detect(inv.dirs) {
		ids = append(ids, c.Slug())
	}
	events := inv.judgeRemovals(ids, s.IgnoreSystemFiles)
	for _, ev := range events {
		inv.out.emit(ev)
	}
	removable := inv.printRemovals(events)
	inv.summary = plural(len(removable), "configuration") + " can be removed, " + plural(len(events)-len(removable), "configuration") + " cannot"
	if len(events) == 0 {
		inv.out.print("No configuration is detected on this machine.")
		inv.summary = "no configuration is detected"
	}
	if len(removable) > 0 && !inv.out.json {
		inv.out.print()
		inv.out.print("Run ", inv.out.paint(label, "agentx config remove "+strings.Join(removable, " ")), " to remove them.")
	}
	return nil
}

// printRemovals writes the judged configurations in two groups, the ones
// that can be removed and the ones that cannot with what each holds, and
// returns the ids of the first.
func (inv *invocation) printRemovals(events []configRemovalEvent) []string {
	out := inv.out
	var removable []string
	can, cannot := &table{}, &table{wrap: out.columns(out.stdout)}
	for _, ev := range events {
		if ev.State == removalKept {
			cannot.add(c(ev.ID, heading), c(quotedPath(ev.Path), muted), c("holds "+andList(ev.Holds), plain))
			continue
		}
		removable = append(removable, ev.ID)
		can.add(c(ev.ID, heading), c(quotedPath(ev.Path), muted))
	}
	if len(removable) > 0 {
		out.print(out.paint(heading, fmt.Sprintf("Can be removed (%d)", len(removable))))
		out.render(can, "  ")
	}
	if kept := len(events) - len(removable); kept > 0 {
		if len(removable) > 0 {
			out.print()
		}
		out.print(out.paint(heading, fmt.Sprintf("Cannot be removed (%d)", kept)))
		out.render(cannot, "  ")
	}
	return removable
}

// configRemove deletes the configuration directory of each client named
// and takes the configurations out of the settings, as one journaled
// mutation, or refuses and changes nothing when any of them holds
// something a removal would lose.
//
// Everything is judged under the lock. Each directory is then captured as
// the journal records it and judged once more, so that a file written into
// it after the judgment is either seen by the second one or refuses the
// remove step, which deletes only the content it captured.
func (inv *invocation) configRemove(cmd *cobra.Command, ids []string) error {
	ctx := cmd.Context()
	for _, id := range ids {
		if err := inv.detectedConfiguration(id); err != nil {
			return err
		}
	}
	var events []configRemovalEvent
	var settings home.Settings
	refused := false
	err := home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		edit, err := inv.beginSettings()
		if err != nil {
			return err
		}
		events = inv.judgeRemovals(ids, edit.s.IgnoreSystemFiles)
		if len(events) == 0 {
			// Every one of them went between the check and the lock.
			return fail(exitNotFound, "the configurations named are no longer detected", "")
		}
		if kept(events) > 0 {
			refused = true
			return removalRefusal(events)
		}
		m := home.NewMutation(inv.dirs.Home)
		captured := map[string]bool{}
		for _, ev := range events {
			if slices.ContainsFunc(events, func(o configRemovalEvent) bool { return o.Path != ev.Path && beneath(ev.Path, o.Path) }) {
				continue // it goes with the directory it lies in
			}
			if captured[ev.Path] {
				continue // two configurations kept in one directory
			}
			captured[ev.Path] = true
			state, err := home.State(ev.Path)
			if err != nil {
				m.Discard()
				return err
			}
			m.Remove(ev.Path, state)
		}
		if again := inv.judgeRemovals(ids, edit.s.IgnoreSystemFiles); kept(again) > 0 || len(again) != len(events) {
			m.Discard()
			return fail(exitRefused, "a configuration directory changed while it was being removed, so nothing was removed",
				"run the command again")
		}
		for _, ev := range events {
			edit.dropConfiguration(ev.ID)
		}
		if err := edit.stage(m, inv.dirs.Home); err != nil {
			m.Discard()
			return err
		}
		// A run stopped part way is taken back, never finished: what lets
		// a directory go is what the rest of the machine holds, which the
		// journal does not capture.
		m.TakeBackUnfinished()
		if err := m.Apply(inv.refs(ctx)); err != nil {
			if errors.Is(err, home.ErrTakenBack) {
				return fail(exitRefused, "nothing was removed: "+strings.TrimPrefix(err.Error(), home.ErrTakenBack.Error()+": "),
					"run the command again")
			}
			return err
		}
		settings = edit.settings()
		return nil
	})
	if err != nil {
		if refused {
			for _, ev := range events {
				inv.out.emit(ev)
			}
		}
		return mutationFailure(err)
	}
	paths := &table{}
	names := make([]string, len(events))
	for i := range events {
		events[i].State = removalRemoved
		inv.out.emit(events[i])
		names[i] = events[i].ID
		paths.add(c(events[i].ID, heading), c(quotedPath(events[i].Path), muted))
	}
	inv.emitSettings(settings)
	inv.out.done("removed " + plural(len(events), "configuration"))
	inv.out.render(paths, "  ")
	inv.summary = "removed " + andList(names)
	return nil
}

// kept counts the configurations a judgment holds back.
func kept(events []configRemovalEvent) int {
	n := 0
	for _, ev := range events {
		if ev.State == removalKept {
			n++
		}
	}
	return n
}

// removalRefusal is the answer of a removal that changed nothing: every
// configuration held back, with what it holds, and the way on: the
// configurations that share a directory with one named and were left out,
// or the command that removes the others when there are any.
func removalRefusal(events []configRemovalEvent) error {
	var held, free, unnamed []string
	for _, ev := range events {
		if ev.State == removalKept {
			held = append(held, sanitised(ev.ID)+" holds "+andList(ev.Holds))
		} else {
			free = append(free, ev.ID)
		}
		for _, id := range ev.With {
			if !slices.ContainsFunc(events, func(o configRemovalEvent) bool { return o.ID == id }) && !slices.Contains(unnamed, id) {
				unnamed = append(unnamed, id)
			}
		}
	}
	hint := "move or delete what it holds, then run the command again"
	switch {
	case len(unnamed) > 0:
		hint = "name " + andList(unnamed) + " as well: the directory is theirs too"
	case len(free) > 0:
		hint = "'agentx config remove " + strings.Join(free, " ") + "' removes the others"
	}
	return fail(exitRefused, "nothing was removed: "+strings.Join(held, "; "), hint)
}
