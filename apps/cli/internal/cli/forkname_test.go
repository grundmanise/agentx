package cli

import (
	"strings"
	"testing"

	"github.com/goccy/go-yaml"

	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

func TestForkNameRefusal(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		ok   bool
	}{
		{"pdf", true},
		{"my-skill-2", true},
		{"123", true},
		{strings.Repeat("a", 64), true},
		{strings.Repeat("a", 65), false},
		{"", false},
		{"My-Skill", false},
		{"my_skill", false},
		{"-pdf", false},
		{"pdf-", false},
		{"my--skill", false},
		{"pdf.lock", false},
		{"nested/pdf", false},
		{"café", false},
	} {
		got := forkNameRefusal(tc.name)
		if (got == "") != tc.ok {
			t.Errorf("forkNameRefusal(%q) = %q, want ok %v", tc.name, got, tc.ok)
		}
		if !tc.ok && !strings.Contains(got, forkNameRule) {
			t.Errorf("forkNameRefusal(%q) = %q, which does not state the rule", tc.name, got)
		}
	}
}

func TestNameTaken(t *testing.T) {
	t.Parallel()
	records := map[string]lineage.Record{
		"Foo": {Name: "Foo", Kind: lineage.KindManaged},
		"bar": {Name: "bar", Kind: lineage.KindFork},
	}
	for _, tc := range []struct {
		name, except, want string
	}{
		{"foo", "", "Foo"},
		{"Foo", "", "Foo"},
		{"bar", "", "bar"},
		{"BAR", "", "bar"},
		{"baz", "", ""},
		{"foo", "Foo", ""},
	} {
		rec, taken := nameTaken(records, tc.name, tc.except)
		if taken != (tc.want != "") || rec.Name != tc.want {
			t.Errorf("nameTaken(%q, except %q) = %q, %v; want %q", tc.name, tc.except, rec.Name, taken, tc.want)
		}
	}
	err := takenRefusal(records, "foo", "", "choose another name")
	if err == nil || err.Error() != "the account repo already holds Foo as a managed skill, which differs from foo only by case" {
		t.Errorf("the refusal of a name differing by case reads %v", err)
	}
}

func TestLibraryLink(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		lib, target, want string
	}{
		{"/home/u/.agents/skills", "/home/u/.agentx/worktrees/pdf/pdf", "../../.agentx/worktrees/pdf/pdf"},
		{"/tmp/t/library", "/tmp/t/agentx/worktrees/pdf/pdf", "../agentx/worktrees/pdf/pdf"},
		{"/Volumes/skills", "/home/u/.agentx/worktrees/pdf/pdf", "/home/u/.agentx/worktrees/pdf/pdf"},
		{"/library", "/agentx/worktrees/pdf/pdf", "/agentx/worktrees/pdf/pdf"},
		{"/", "/agentx/worktrees/pdf/pdf", "/agentx/worktrees/pdf/pdf"},
	} {
		if got := libraryLink(tc.lib, tc.target); got != tc.want {
			t.Errorf("libraryLink(%q, %q) = %q, want %q", tc.lib, tc.target, got, tc.want)
		}
	}
}

func TestSkillTemplate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		description string
		plain       bool
	}{
		{"Fill in PDF forms, and merge them.", true},
		{"Use it when: a PDF needs filling", false},
		{`Say "hello" to the user`, false},
		{"- a list item?", false},
		{"# not a comment", false},
		{"yes", false},
		{"Back\\slash", false},
		{"x", true},
		{"ends with a space ", false},
	} {
		text := string(skillTemplate("pdf", tc.description))
		block, _, ok := strings.Cut(strings.TrimPrefix(text, "---\n"), "\n---\n")
		if !ok || !strings.HasPrefix(text, "---\nname: pdf\n") || !strings.HasSuffix(text, "\n") {
			t.Errorf("%q: the template is\n%s", tc.description, text)
			continue
		}
		var fields map[string]any
		if err := yaml.Unmarshal([]byte(block), &fields); err != nil {
			t.Errorf("%q: the frontmatter does not parse: %v", tc.description, err)
			continue
		}
		if fields["description"] != tc.description || fields["name"] != "pdf" {
			t.Errorf("%q: the frontmatter reads back as %v", tc.description, fields)
		}
		if plain := strings.Contains(text, "\ndescription: "+tc.description+"\n"); plain != tc.plain {
			t.Errorf("%q: written plain %v, want %v:\n%s", tc.description, plain, tc.plain, text)
		}
	}
}
