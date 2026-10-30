// Package skillshare reads the install records Skillshare keeps, which is
// how a machine that installed skills with that tool records where each
// one came from. Nothing here writes: the files belong to another tool,
// and agentx only ever reads them.
package skillshare

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
)

// StoreName is the file Skillshare keeps its install records in, inside
// the folder it installs skills into.
const StoreName = ".metadata.json"

// ConfigName is Skillshare's configuration file, which can move that
// folder.
const ConfigName = "config.yaml"

// Version is the store schema this reader was written against. A file that
// says something else is still read, entry by entry, with a warning: the
// fields a newer file holds are either the ones below or ones agentx does
// not read.
const Version = 1

// maxBytes is the largest store or configuration file that is read. Both
// files are a third party's and a user's to edit, so each is bounded before
// it is read whole; a store of a few hundred skills is a few hundred
// kilobytes.
const maxBytes = 8 << 20

// ErrStore is the error of a file that is not a store, or a configuration,
// this can read.
var ErrStore = errors.New("not a Skillshare file agentx can read")

// Entry is one skill of the store: the path it was installed at below the
// skills folder, where it came from and how.
type Entry struct {
	Name    string // the map key: the skill's path below the skills folder, with a / for a grouped skill
	Source  string // the source as the user gave it, github.com/owner/repo/path once that tool expanded it
	Type    string // github, git-https, git-ssh, local or unknown, with -subdir appended when a subdirectory was given
	RepoURL string // the clone URL, "" for a local path
	Subdir  string // the skill directory inside the repository, "" for its root
	Branch  string // the branch it was installed from, "" for the default branch
	Tracked bool   // a whole repository checked out as _name/, with skills inside it
	Kind    string // "" or skill for a skill, agent for an agent file
	File    string // the store the entry was read from
}

// Paths is where Skillshare keeps its files on this machine.
type Paths struct {
	Base   string // the base directory, where config.yaml lives
	Config string // the configuration file
}

// Locate is where Skillshare keeps its files: $XDG_CONFIG_HOME/skillshare,
// or ~/.config/skillshare when that variable is not set, with the
// configuration at config.yaml there unless SKILLSHARE_CONFIG names another
// file. configHome is the XDG config home already resolved.
func Locate(env map[string]string, configHome string) Paths {
	p := Paths{Base: filepath.Join(configHome, "skillshare")}
	p.Config = env["SKILLSHARE_CONFIG"]
	if p.Config == "" {
		p.Config = filepath.Join(p.Base, ConfigName)
	}
	return p
}

// Config is what agentx reads out of Skillshare's configuration: the
// folder it installs skills into, where the store is and where the links
// it makes in every client's folder lead.
type Config struct {
	Skills string
}

// ReadConfig reads the configuration. The skills folder is sources.skills,
// else the older source key, else skills below the base directory, a
// leading ~ standing for the user's home. A configuration that is not
// there leaves the default; one that cannot be read as YAML is an
// ErrStore, since the folder it names may be anywhere.
func ReadConfig(p Paths, userHome string) (Config, error) {
	cfg := Config{Skills: filepath.Join(p.Base, "skills")}
	b, err := readBounded(p.Config)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return Config{}, err
	}
	var file struct {
		Sources struct {
			Skills string `yaml:"skills"`
		} `yaml:"sources"`
		Source string `yaml:"source"`
	}
	if err := yaml.Unmarshal(b, &file); err != nil {
		return Config{}, fmt.Errorf("%w: %s: %v", ErrStore, p.Config, err)
	}
	for _, dir := range []string{file.Sources.Skills, file.Source} {
		if dir = strings.TrimSpace(dir); dir != "" {
			cfg.Skills = expandHome(dir, userHome)
			break
		}
	}
	return cfg, nil
}

// expandHome replaces a leading ~ with the user's home, as Skillshare
// expands a path of its configuration.
func expandHome(path, userHome string) string {
	if path == "~" {
		return userHome
	}
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		return filepath.Join(userHome, rest)
	}
	return path
}

// LibraryName is the name the agentx library holds a skill of the store
// under: the key as it is for a plain skill, and for a grouped one such as
// frontend/ui the name Skillshare itself gives it in every tool's skills
// folder, the path with each / written as a double underscore.
func LibraryName(key string) string {
	return strings.ReplaceAll(key, "/", "__")
}

// Read reads the store in dir and returns its entries by name, sorted by
// name, with the warnings the read produced. A store that is not there
// contributes nothing; one that is not a store at all is an error, since a
// caller asked to adopt what it holds cannot be told that it holds nothing.
func Read(dir string) ([]Entry, []string, error) {
	path := filepath.Join(dir, StoreName)
	b, err := readBounded(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	// The file is read field by field rather than into a struct: a version
	// written as something other than a number may not cost the reader the
	// entries beside it, which are the part an adoption needs.
	var file map[string]json.RawMessage
	if err := json.Unmarshal(b, &file); err != nil {
		return nil, nil, fmt.Errorf("%w: %s: %v", ErrStore, path, err)
	}
	var warnings []string
	var version int
	if raw, ok := file["version"]; !ok || json.Unmarshal(raw, &version) != nil {
		warnings = append(warnings, fmt.Sprintf("%s says no version; it is read as version %d", path, Version))
	} else if version != Version {
		warnings = append(warnings, fmt.Sprintf("%s is version %d and this agentx reads version %d; an entry it cannot read is left out", path, version, Version))
	}
	raw, ok := file["entries"]
	if !ok {
		return nil, warnings, fmt.Errorf("%w: %s holds no entries map", ErrStore, path)
	}
	fields, dup, err := objectFields(raw)
	if err != nil {
		return nil, warnings, fmt.Errorf("%w: %s: %v", ErrStore, path, err)
	}
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	entries := make([]Entry, 0, len(fields))
	for _, name := range names {
		if dup[name] {
			// The file says two things about one skill and there is no
			// telling which install is the one on disk.
			warnings = append(warnings, fmt.Sprintf("%s names %s twice and is ambiguous about it; it is left out", path, name))
			continue
		}
		e, notes, err := parseEntry(name, fields[name])
		for _, note := range notes {
			warnings = append(warnings, fmt.Sprintf("%s in %s: %s", name, path, note))
		}
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s in %s is left out: %v", name, path, err))
			continue
		}
		e.File = path
		entries = append(entries, e)
	}
	return entries, warnings, nil
}

// readBounded reads path, refusing a file larger than one of these files
// may be before it is in memory.
func readBounded(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxBytes {
		return nil, fmt.Errorf("%w: %s is larger than %d bytes", ErrStore, path, maxBytes)
	}
	return b, nil
}

// objectFields splits a JSON object into its fields without losing a key
// given twice, which encoding/json resolves silently by keeping the last.
// A store that names one skill twice says two things about it, and the
// caller has to know rather than be handed one of them.
func objectFields(raw json.RawMessage) (fields map[string]json.RawMessage, duplicate map[string]bool, err error) {
	duplicate = map[string]bool{}
	dec := json.NewDecoder(bytes.NewReader(raw))
	// The opening brace is read before anything else: JSON null unmarshals
	// into a map without an error and leaves it nil, so a file whose
	// entries are null would otherwise read as a file holding no skill
	// rather than as one holding no entries map at all.
	if open, err := dec.Token(); err != nil || open != json.Delim('{') {
		return nil, nil, errors.New("entries is not an object")
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, nil, err
	}
	seen := map[string]bool{}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		name, ok := key.(string)
		if !ok {
			return nil, nil, errors.New("entries is not an object")
		}
		if seen[name] {
			duplicate[name] = true
		}
		seen[name] = true
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return nil, nil, err
		}
	}
	return fields, duplicate, nil
}

// parseEntry reads one entry of the store. What it insists on is what an
// adoption cannot do without: a name, and a source to look it up in. An
// entry is left out for that and for nothing else, so a field written as
// something other than what Skillshare writes there is one field the
// reader cannot use and not a skill it forgets: the field is dropped, the
// note says which one, and the entry is read on. What a note says is the
// field the user can look at in their file, never the shape of the value
// agentx tried to read it into.
func parseEntry(name string, raw json.RawMessage) (Entry, []string, error) {
	if name == "" {
		return Entry{}, nil, errors.New("it has no name")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return Entry{}, nil, errors.New("it is not an object")
	}
	e, notes := Entry{Name: name}, []string(nil)
	for field, into := range map[string]*string{
		"source":   &e.Source,
		"type":     &e.Type,
		"repo_url": &e.RepoURL,
		"subdir":   &e.Subdir,
		"branch":   &e.Branch,
		"kind":     &e.Kind,
	} {
		raw, ok := fields[field]
		if !ok || string(raw) == "null" {
			continue
		}
		if err := json.Unmarshal(raw, into); err != nil {
			*into = ""
			notes = append(notes, field+" is not text and is left out of the entry")
		}
	}
	if raw, ok := fields["tracked"]; ok && string(raw) != "null" {
		if err := json.Unmarshal(raw, &e.Tracked); err != nil {
			notes = append(notes, "tracked is not true or false and is left out of the entry")
		}
	}
	sort.Strings(notes)
	if e.Source == "" && e.RepoURL == "" {
		return Entry{}, notes, errors.New("it names no source")
	}
	e.Subdir = strings.Trim(strings.ReplaceAll(e.Subdir, `\`, "/"), "/")
	return e, notes, nil
}
