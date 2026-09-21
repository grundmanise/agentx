package source

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"sort"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
)

// Index is the skill list of every source of this machine, held in memory
// so that a search is answered without a git process and without the lock.
// It is never changed in place: a rebuild is a new Index, and BuildIndex
// returns the one it was given when nothing changed.
type Index struct {
	Sources []Indexed // sorted by canonical URL
	stamp   string    // the sources and their commits; equal stamps mean an equal index
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

// BuildIndex lists the skills of every source in urls, the canonical URLs
// the machine settings hold, from the account repo alone: no network, and
// the whole of each source rather than a subpath, since a subpath scopes one
// listing and is never stored.
//
// prev is the index of the previous build. The sources and their commits are
// read in one for-each-ref; when they are as they were, prev is returned
// unchanged and nothing else runs, so a rescan that follows a change
// elsewhere on the machine costs one git process, and a machine with no
// source costs none. warnings, sorted, name the sources whose skills are not
// in the index: one that was never fetched, and one the account repo cannot
// list whole.
func BuildIndex(ctx context.Context, r *gitx.Runner, gitDir string, urls []string, prev *Index) (idx *Index, warnings []string, err error) {
	idx = &Index{Sources: []Indexed{}}
	if len(urls) == 0 {
		return idx, nil, nil
	}
	commits, err := commitsOf(ctx, r, gitDir)
	if err != nil {
		return nil, nil, err
	}
	seen := map[string]bool{}
	for _, url := range urls {
		if url == "" || seen[url] {
			continue
		}
		seen[url] = true
		idx.Sources = append(idx.Sources, Indexed{URL: url, Commit: commits[ID(url)], Skills: []Skill{}})
	}
	sort.Slice(idx.Sources, func(i, j int) bool { return idx.Sources[i].URL < idx.Sources[j].URL })
	var stamp strings.Builder
	for _, src := range idx.Sources {
		stamp.WriteString(src.URL + "\x00" + src.Commit + "\n")
	}
	idx.stamp = stamp.String()
	if prev != nil && prev.stamp == idx.stamp {
		return prev, nil, nil
	}
	for i := range idx.Sources {
		src := &idx.Sources[i]
		if src.Commit == "" {
			warnings = append(warnings, "source "+src.URL+" has not been fetched, its skills cannot be searched")
			continue
		}
		listing, err := List(ctx, r, gitDir, Source{URL: src.URL})
		if err != nil {
			warnings = append(warnings, "source "+src.URL+": "+err.Error()+", its skills cannot be searched")
			continue
		}
		src.Skills = listing.Skills
	}
	sort.Strings(warnings)
	return idx, warnings, nil
}

// commitsOf is Commits over an account repo that may not exist yet, which
// on a machine whose sources were recorded but never fetched is the same as
// one holding no source ref. The directory is checked without running git,
// so a rebuild that finds nothing to do spawns nothing.
func commitsOf(ctx context.Context, r *gitx.Runner, gitDir string) (map[string]string, error) {
	if _, err := os.Stat(gitDir); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return Commits(ctx, r, gitDir)
}
