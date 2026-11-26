package source

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
)

// Index is the skill list of every source of this machine, held in memory
// so that a search is answered without a git process and without the lock.
// It is never changed in place: a rebuild is a new Index, and BuildIndex
// returns the one it was given when nothing changed.
type Index struct {
	Sources []Indexed          // sorted by canonical URL
	read    map[string]reading // what this build made of each source, keyed by its URL and commit
}

// reading is what one build made of one source at one commit: the skills
// the store held for it, none for a source not fetched yet.
// A source whose listing failed has no reading at all, so the next build
// reads it again instead of serving an empty listing for as long as the ref
// stands still: the failure is the store's answer now, not a
// property of the commit.
type reading struct {
	skills []Skill
}

// readingAt is what the build that produced idx made of the source that key
// names, and whether it made anything of it. A nil index has nothing.
func (idx *Index) readingAt(key string) (reading, bool) {
	if idx == nil {
		return reading{}, false
	}
	got, ok := idx.read[key]
	return got, ok
}

// Indexed is one source in the index at the commit its ref holds.
type Indexed struct {
	URL    string
	Commit string  // empty when the source has not been fetched
	Skills []Skill // as List reads them, sorted by subpath
}

// Match is one search result: a skill and the source it is in. The source
// is named by its canonical URL, which is what `agentx skill add` takes and
// what `agentx source list` prints, so a result can be acted on as it is.
type Match struct {
	Source      string `json:"source"`
	Subpath     string `json:"subpath"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Tree        string `json:"tree"`
}

// Search lists the skills whose name or description contains query, compared
// case-insensitively, sorted by source canonical URL, then name, then
// subpath. Nothing matching is an empty, non-nil slice, and so is a search
// of a nil index. It reads memory only.
func (idx *Index) Search(query string) []Match {
	matches := []Match{}
	if idx == nil {
		return matches
	}
	q := strings.ToLower(query)
	for _, src := range idx.Sources {
		for _, s := range src.Skills {
			if strings.Contains(strings.ToLower(s.Name), q) || strings.Contains(strings.ToLower(s.Description), q) {
				matches = append(matches, Match{Source: src.URL, Subpath: s.Subpath, Name: s.Name, Description: s.Description, Tree: s.Tree})
			}
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		a, b := matches[i], matches[j]
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Subpath < b.Subpath
	})
	return matches
}

// Hub is the hub as the index reads it: its canonical URL and the tip of
// each of its forks' branches, skills/<name>, as the store holds them, by
// name. The zero Hub is no hub.
type Hub struct {
	URL  string
	Tips map[string]string
}

// key names what a build made of the hub: its URL and every fork's tip, so
// a build that finds the same tips keeps the last build's skills.
func (h Hub) key() string {
	names := make([]string, 0, len(h.Tips))
	for name := range h.Tips {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString(h.URL)
	for _, name := range names {
		b.WriteString("\x00" + name + " " + h.Tips[name])
	}
	return b.String()
}

// BuildIndex lists the skills of every source in urls, the canonical URLs
// the machine settings hold, from the store alone: no network, and
// the whole of each source rather than a subpath, since a subpath scopes one
// listing and is never stored. It lists the hub's forks too, one skill per
// branch, named after it, as `skill add --name` takes it, with the
// description of the SKILL.md at its tip.
//
// prev is the index of the previous build. The sources and their commits are
// read in one for-each-ref, and a source still at the commit prev read it at
// keeps prev's skills rather than being listed again; when every source is,
// prev itself is returned and nothing else runs, so a rescan that follows a
// change elsewhere on the machine costs one git process, and a machine with
// no source costs none. A source prev could not list is listed again
// whatever its commit, so an index built while the store could not
// answer for one source recovers on the next build rather than holding that
// source empty until its ref moves. warnings, sorted, name the sources the
// store cannot list whole, by every build, since each lists them again. A
// source never fetched is no warning: it is in the index with no skills,
// and the update check that fetches it says when it cannot.
func BuildIndex(ctx context.Context, r *gitx.Runner, gitDir string, urls []string, hub Hub, prev *Index) (idx *Index, warnings []string, err error) {
	idx = &Index{Sources: []Indexed{}, read: map[string]reading{}}
	if len(urls) == 0 && hub.URL == "" {
		return idx, nil, nil
	}
	var commits map[string]string
	if len(urls) > 0 {
		if commits, err = commitsOf(ctx, r, gitDir); err != nil {
			return nil, nil, err
		}
	}
	seen := map[string]bool{hub.URL: hub.URL != ""}
	for _, url := range urls {
		if url == "" || seen[url] {
			continue
		}
		seen[url] = true
		idx.Sources = append(idx.Sources, Indexed{URL: url, Commit: commits[ID(url)], Skills: []Skill{}})
	}
	if hub.URL != "" {
		idx.Sources = append(idx.Sources, Indexed{URL: hub.URL, Skills: []Skill{}})
	}
	sort.Slice(idx.Sources, func(i, j int) bool { return idx.Sources[i].URL < idx.Sources[j].URL })
	// Every source served from prev and no source gone or arrived means the
	// index prev holds is this one, so prev is returned as it is and nothing
	// ran but the for-each-ref above.
	unchanged := prev != nil && len(prev.Sources) == len(idx.Sources)
	for i := range idx.Sources {
		src := &idx.Sources[i]
		key := src.URL + "\x00" + src.Commit
		read := func() (reading, string) { return readSource(ctx, r, gitDir, src.URL, src.Commit) }
		if src.URL == hub.URL {
			key = hub.key()
			read = func() (reading, string) { return readHub(ctx, r, gitDir, hub) }
		}
		got, ok := prev.readingAt(key)
		if !ok {
			unchanged = false
			var failed string
			if got, failed = read(); failed != "" {
				// Deliberately not remembered: the next build asks the
				// store again rather than caching what it could not
				// answer.
				warnings = append(warnings, failed)
				continue
			}
		}
		idx.read[key] = got
		src.Skills = got.skills
	}
	if unchanged {
		return prev, nil, nil
	}
	sort.Strings(warnings)
	return idx, warnings, nil
}

// readSource lists one source of the index from the store. It
// returns the reading to remember, or, when the store could not
// answer for the source at all, the warning that says so and no reading.
func readSource(ctx context.Context, r *gitx.Runner, gitDir, url, commit string) (reading, string) {
	if commit == "" {
		// Not a failure to try again: nothing but a fetch gives an unfetched
		// source a ref, and a fetch moves it, which is a reading of its own.
		return reading{skills: []Skill{}}, ""
	}
	listing, err := List(ctx, r, gitDir, Source{URL: url})
	if err != nil {
		return reading{}, "source " + url + ": " + err.Error() + ", its skills cannot be searched"
	}
	return reading{skills: listing.Skills}, ""
}

// readHub lists the hub's forks from the store: one skill per branch,
// named after it, at the root of its tree, with the description of the
// SKILL.md at its tip. One cat-file --batch-check names each tip's tree and
// SKILL.md, and one cat-file --batch reads those files. A branch whose tip
// holds no SKILL.md the store can read is listed with no description. It
// returns the reading to remember, or, when the store could not answer for
// the hub at all, the warning that says so and no reading.
func readHub(ctx context.Context, r *gitx.Runner, gitDir string, hub Hub) (reading, string) {
	if len(hub.Tips) == 0 {
		return reading{skills: []Skill{}}, ""
	}
	names := make([]string, 0, len(hub.Tips))
	for name := range hub.Tips {
		names = append(names, name)
	}
	sort.Strings(names)
	var in strings.Builder
	for _, name := range names {
		in.WriteString(hub.Tips[name] + "^{tree}\n" + hub.Tips[name] + ":SKILL.md\n")
	}
	failed := func(err error) (reading, string) {
		return reading{}, "the hub " + hub.URL + ": " + err.Error() + ", its skills cannot be searched"
	}
	out, err := r.IsolatedInput(ctx, gitDir, strings.NewReader(in.String()), "cat-file", "--batch-check=%(objectname) %(objecttype)")
	if err != nil {
		return failed(err)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 2*len(names) {
		return failed(fmt.Errorf("%w: cat-file named %d objects for %d forks", ErrIncomplete, len(lines), len(names)))
	}
	object := func(line, want string) string {
		id, typ, _ := strings.Cut(line, " ")
		if typ != want || !objectID(id) {
			return ""
		}
		return id
	}
	var blobs []string
	for i := range names {
		if blob := object(lines[2*i+1], "blob"); blob != "" {
			blobs = append(blobs, blob)
		}
	}
	files, err := ReadBlobs(ctx, r, gitDir, blobs)
	if err != nil {
		return failed(err)
	}
	skills := make([]Skill, 0, len(names))
	for i, name := range names {
		_, description, _ := scan.SkillFrontmatter(files[object(lines[2*i+1], "blob")])
		skills = append(skills, Skill{Name: name, Description: description, Tree: object(lines[2*i], "tree")})
	}
	return reading{skills: skills}, ""
}

// commitsOf is Commits over a store that may not exist yet, which
// on a machine whose sources were recorded but never fetched is the same as
// one holding no source ref. The directory is checked without running git,
// so a rebuild that finds nothing to do spawns nothing.
func commitsOf(ctx context.Context, r *gitx.Runner, gitDir string) (map[string]string, error) {
	if _, err := os.Stat(gitDir); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return Commits(ctx, r, gitDir)
}
