package cli

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// TestNameRefusal holds the frontmatter names an install refuses before it
// writes anything to the thing that cannot hold them: a library directory,
// or one level of refs/heads/managed/<name>. A name only one of them
// accepts would otherwise be found out halfway through the mutation, with
// the journal on disk and every later command recovering it and failing the
// same way.
func TestNameRefusal(t *testing.T) {
	t.Parallel()
	const library, branch = "is not a name the library can hold as a directory", "is not a name the account repo can hold as an import branch"
	for _, c := range []struct {
		what, name, want string
	}{
		{"a plain name", "alpha", ""},
		{"a name beyond ASCII", "café", ""},
		{"a dash and a dot inside", "my-skill.v2", ""},
		{"nothing at all", "", library},
		{"a separator", "a/b", library},
		{"a branch-like separator", "feature/login", library},
		{"a backslash", `a\b`, library},
		{"a hidden name", ".hidden", library},
		{"a newline", "two\nlines", library},
		{"a carriage return", "a\rb", library},
		{"a NUL", "a\x00b", library},
		{"a space, which git refuses in a ref", "my skill", branch},
		{"a tab", "a\tb", branch},
		{"a DEL", "a\x7fb", branch},
		{"a colon", "we:ird", branch},
		{"a question mark", "who?", branch},
		{"a star", "a*b", branch},
		{"a bracket", "a[b", branch},
		{"a tilde", "a~b", branch},
		{"a caret", "a^b", branch},
		{"two dots", "a..b", branch},
		{"an @{", "a@{b", branch},
		{"@ alone", "@", branch},
		{"a trailing dot", "alpha.", branch},
		{"a trailing .lock", "alpha.lock", branch},
	} {
		if got := nameRefusal(c.name); got != c.want {
			t.Errorf("%s: nameRefusal(%q) = %q, want %q", c.what, c.name, got, c.want)
		}
	}
}

// TestSelectionCheck refuses the flag combinations of skill add that cannot
// mean anything, before a source is read or added.
func TestSelectionCheck(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		what string
		sel  selection
		says string // "" when the selection passes
	}{
		{"nothing named", selection{}, ""},
		{"--name", selection{names: []string{"alpha"}}, ""},
		{"--all", selection{all: true}, ""},
		{"--all --except", selection{all: true, except: []string{"beta"}}, ""},
		{"--all with --name", selection{all: true, names: []string{"alpha"}}, "--all and --name cannot both be given"},
		{"--except without --all", selection{names: []string{"alpha"}, except: []string{"beta"}}, "--except needs --all"},
		{"--except alone", selection{except: []string{"beta"}}, "--except needs --all"},
	} {
		err := c.sel.check()
		if c.says == "" {
			if err != nil {
				t.Errorf("%s: refused: %v", c.what, err)
			}
			continue
		}
		equal(t, c.what+": status", statusOf(t, err), exitUsage)
		contains(t, c.what+": the refusal", err.Error(), c.says)
	}
}

// TestSelectSkills picks what an install takes out of a listing: names
// match the frontmatter name, which is the directory name only when the
// frontmatter has none, in any case; the listing's order decides, not the
// order of the flags; a name given twice takes one skill, and a name two
// directories share takes the first of them. An --except or a --name that
// names no skill of the source is refused, since the run would otherwise
// install more or less than was asked.
func TestSelectSkills(t *testing.T) {
	t.Parallel()
	src := source.Source{URL: "https://github.com/example/skills"}
	listing := source.Listing{Skills: []source.Skill{
		{Subpath: "skills/alpha", Name: "alpha"},
		{Subpath: "skills/beta", Name: "beta"},
		{Subpath: "skills/gamma", Name: "gamma"},
		{Subpath: "skills/on-disk", Name: "fancy"}, // named otherwise in its frontmatter
	}}
	twins := source.Listing{Skills: []source.Skill{
		{Subpath: "skills/first", Name: "twin"},
		{Subpath: "skills/other", Name: "other"},
		{Subpath: "skills/second", Name: "Twin"},
	}}
	for _, c := range []struct {
		what    string
		listing source.Listing
		sel     selection
		want    string // the subpaths selected, or the refusal
		status  status // of the refusal
	}{
		{"--name in any case, twice, in another order", listing, selection{names: []string{"GAMMA", "Alpha", "alpha"}}, "skills/alpha skills/gamma", exitOK},
		{"--name by the frontmatter name", listing, selection{names: []string{"FANCY"}}, "skills/on-disk", exitOK},
		{"--all", listing, selection{all: true}, "skills/alpha skills/beta skills/gamma skills/on-disk", exitOK},
		{"--all --except in any case, twice", listing, selection{all: true, except: []string{"BeTa", "beta", "fancy"}}, "skills/alpha skills/gamma", exitOK},
		{"one skill needs no --name", source.Listing{Skills: listing.Skills[:1]}, selection{}, "skills/alpha", exitOK},
		{"a name two directories share takes the first", twins, selection{names: []string{"twin"}}, "skills/first", exitOK},
		{"--except leaves out every skill of the name", twins, selection{all: true, except: []string{"TWIN"}}, "skills/other", exitOK},
		{"--name by the directory name of a named skill", listing, selection{names: []string{"alpha", "on-disk"}}, `has no skill called "on-disk"`, exitNotFound},
		{"--name of nothing in the source, named once", listing, selection{names: []string{"delta", "Delta", "epsilon"}}, `has no skill called "delta", "epsilon"`, exitNotFound},
		{"--except of nothing in the source", listing, selection{all: true, except: []string{"delta"}}, `has no skill called "delta"`, exitNotFound},
		{"--except of everything", listing, selection{all: true, except: []string{"alpha", "beta", "gamma", "fancy"}}, "--except left no skill to install", exitUsage},
		{"several skills and nothing named", listing, selection{}, src.URL + " holds 4 skills", exitUsage},
		{"a source of no skill", source.Listing{}, selection{all: true}, "no skill in " + src.URL, exitNotFound},
	} {
		got, err := selectSkills(c.listing, c.sel, src)
		if c.status != exitOK {
			if err == nil {
				t.Errorf("%s: selected %v, want a refusal", c.what, got)
				continue
			}
			equal(t, c.what+": status", statusOf(t, err), c.status)
			contains(t, c.what+": the refusal", err.Error(), c.want)
			continue
		}
		if err != nil {
			t.Errorf("%s: refused: %v", c.what, err)
			continue
		}
		var subpaths []string
		for _, sk := range got {
			subpaths = append(subpaths, sk.Subpath)
		}
		equal(t, c.what, strings.Join(subpaths, " "), c.want)
	}
}

// statusOf is the exit code row a command error ends the run with.
func statusOf(t *testing.T, err error) status {
	t.Helper()
	var f *failure
	if !errors.As(err, &f) {
		t.Fatalf("%v is not a failure a run answers with", err)
	}
	return f.status
}

// TestThePlaceHintRunsAsItIsPrinted pins the command skill add offers for a
// name the library already holds: skill place with the --to and --copy that
// were given, every word quoted so that a POSIX shell reads back exactly the
// name and the clients, a name that starts with a dash after "--".
func TestThePlaceHintRunsAsItIsPrinted(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		to    []string
		copy  bool
		hint  string
		words []string // what a shell hands agentx
	}{
		{"alpha", []string{"cursor"}, false, "agentx skill place alpha --to cursor",
			[]string{"alpha", "--to", "cursor"}},
		{"alpha", []string{"cursor", "windsurf"}, true, "agentx skill place alpha --to cursor --to windsurf --copy",
			[]string{"alpha", "--to", "cursor", "--to", "windsurf", "--copy"}},
		{"alpha", nil, false, "agentx skill place alpha", []string{"alpha"}},
		{"mine", nil, true, "agentx skill place mine --copy", []string{"mine", "--copy"}},
		{"-mine", []string{"cursor"}, false, "agentx skill place --to cursor -- -mine",
			[]string{"--to", "cursor", "--", "-mine"}},
		{"it's mine", []string{"cursor"}, false, `agentx skill place 'it'\''s mine' --to cursor`,
			[]string{"it's mine", "--to", "cursor"}},
		{"alpha", []string{"my client"}, false, "agentx skill place alpha --to 'my client'",
			[]string{"alpha", "--to", "my client"}},
	} {
		hint := skillCommand("place", c.name, placeFlags(c.to, c.copy)...)
		equal(t, c.name+": the hint", hint, c.hint)
		want := append([]string{"agentx", "skill", "place"}, c.words...)
		if got := shellCommands(hint); len(got) != 1 || !reflect.DeepEqual(got[0], want) {
			t.Errorf("a shell reads %q as %q, want %q", hint, got, want)
		}
	}
}
