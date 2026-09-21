package scan

import (
	"strings"
	"testing"
	"time"
)

func TestParseFrontmatter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		text        string
		wantName    string
		wantDesc    string
		wantErr     string // a substring of the error, "" when there is none
		description string // what the case is about
	}{
		{
			name: "plain values", text: "---\nname: commit\ndescription: Write a commit message\n---\n",
			wantName: "commit", wantDesc: "Write a commit message",
		},
		{
			name: "quoted values", text: "---\nname: \"review\"\ndescription: 'Review a change'\n---\n",
			wantName: "review", wantDesc: "Review a change",
		},
		{
			name: "folded block", text: "---\nname: review\ndescription: >-\n  Review a change\n  before merging\n---\n",
			wantName: "review", wantDesc: "Review a change before merging",
		},
		{
			name: "literal block", text: "---\nname: a\ndescription: |\n  line one\n  line two\n---\n",
			wantName: "a", wantDesc: "line one\nline two",
		},
		{
			// The case this parser was rewritten for: a plain value continued
			// on the lines below its key, which several published skills use.
			name: "plain value on the following lines", text: "---\nname: a\ndescription:\n  React composition patterns that scale. Use when refactoring\n  components with boolean prop proliferation.\n---\n",
			wantName: "a", wantDesc: "React composition patterns that scale. Use when refactoring components with boolean prop proliferation.",
		},
		{
			name: "nested mapping is skipped", text: "---\nname: a\ndescription: d\nmetadata:\n  author: v\n  version: \"3.0.0\"\n---\n",
			wantName: "a", wantDesc: "d",
		},
		{
			name: "sequence contributes nothing", text: "---\nname: a\ndescription:\n  - one\n  - two\n---\n",
			wantName: "a", wantDesc: "",
		},
		{
			name: "mapping under description contributes nothing", text: "---\nname: a\ndescription:\n  text: one\n---\n",
			wantName: "a", wantDesc: "",
		},
		{
			name: "number without quotes", text: "---\nname: a\ndescription: 123\n---\n",
			wantName: "a", wantDesc: "123",
		},
		{
			name: "comments and blank lines", text: "---\n# what this is\n\nname: a\n\ndescription: d\n---\n",
			wantName: "a", wantDesc: "d",
		},
		{
			name: "colon inside a quoted value", text: "---\nname: a\ndescription: \"Use when: you must\"\n---\n",
			wantName: "a", wantDesc: "Use when: you must",
		},
		{
			name: "carriage returns", text: "---\r\nname: a\r\ndescription: d\r\n---\r\n",
			wantName: "a", wantDesc: "d",
		},
		{
			name: "keys agentx does not read", text: "---\nname: a\ndescription: d\nlicense: MIT\nallowed-tools: [Read, Write]\n---\n",
			wantName: "a", wantDesc: "d",
		},
		{
			name: "empty block", text: "---\n---\n",
			wantName: "", wantDesc: "",
		},
		{
			name: "body after the block is ignored", text: "---\nname: a\ndescription: d\n---\n\n# a\n\ndescription: not this one\n",
			wantName: "a", wantDesc: "d",
		},
		{
			name: "no frontmatter", text: "# Just a heading\n",
			wantErr: "no frontmatter",
		},
		{
			name: "block not closed", text: "---\nname: unclosed\n",
			wantErr: "unparsable frontmatter, the --- block is not closed",
		},
		{
			name: "line that is not a key", text: "---\nname: broken\nthis line is not a key\n---\n",
			wantErr: "unparsable frontmatter at line 3",
		},
		{
			// An unquoted colon makes the value ambiguous; YAML refuses it and
			// so does every other reader of these files.
			name: "colon inside an unquoted value", text: "---\nname: a\ndescription: Use when: you must\n---\n",
			wantErr: "unparsable frontmatter at line 3",
		},
		{
			name: "the same key twice", text: "---\nname: a\nname: b\n---\n",
			wantErr: "unparsable frontmatter at line 3",
		},
		{
			name: "not a mapping", text: "---\njust text\n---\n",
			wantErr: "unparsable frontmatter",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			name, description, err := SkillFrontmatter(tt.text)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("SkillFrontmatter(%q) = %v", tt.text, err)
			case tt.wantErr != "" && err == nil:
				t.Fatalf("SkillFrontmatter(%q) = %q, %q; want error %q", tt.text, name, description, tt.wantErr)
			case tt.wantErr != "":
				if got := err.Error(); len(got) < len(tt.wantErr) || got[:len(tt.wantErr)] != tt.wantErr {
					t.Errorf("error = %q, want it to start with %q", got, tt.wantErr)
				}
			}
			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
			if description != tt.wantDesc {
				t.Errorf("description = %q, want %q", description, tt.wantDesc)
			}
		})
	}
}

// TestFrontmatterBounds pins the bounds that keep a hostile SKILL.md from
// taking the process down. The parser builds its whole tree before it can
// report anything, and a deeply nested block costs memory far faster than
// it costs bytes: without the bound, the block below ended the process with
// a runtime out-of-memory that no recover can catch.
func TestFrontmatterBounds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		text     string
		wantErr  string
		wantName string
		wantDesc string
	}{
		{
			// Small enough to pass the byte bound, deep enough that the
			// parser would allocate for it without the second bound.
			name:    "nested past the bound",
			text:    "---\nname: evil\ndescription: " + strings.Repeat("[", 300) + strings.Repeat("]", 300) + "\n---\n",
			wantErr: "unparsable frontmatter: the --- block opens",
		},
		{
			name:    "longer than the bound",
			text:    "---\nname: evil\ndescription: d\n" + strings.Repeat("k: v\n", 20_000) + "---\n",
			wantErr: "unparsable frontmatter: the --- block is",
		},
		{
			// What ended the process before the bounds existed: 200 KB of
			// brackets, which the parser turned into gigabytes.
			name:    "the block that ended the process",
			text:    "---\nname: evil\ndescription: " + strings.Repeat("[", 100_000) + strings.Repeat("]", 100_000) + "\n---\n",
			wantErr: "unparsable frontmatter: the --- block is",
		},
		{
			name:     "a list at the bound still parses",
			text:     "---\nname: fine\ndescription: d\nallowed-tools: [Read, Write, Bash]\n---\n",
			wantName: "fine",
			wantDesc: "d",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			done := make(chan struct{})
			var name, description string
			var err error
			go func() {
				defer close(done)
				name, description, err = SkillFrontmatter(tt.text)
			}()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("parsing did not finish within 5s")
			}
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("SkillFrontmatter = %v", err)
				}
				if name != tt.wantName || description != tt.wantDesc {
					t.Errorf("= %q, %q; want %q, %q", name, description, tt.wantName, tt.wantDesc)
				}
				return
			}
			if err == nil {
				t.Fatalf("SkillFrontmatter = %q, %q; want an error", name, description)
			}
			if got := err.Error(); !strings.HasPrefix(got, tt.wantErr) {
				t.Errorf("error = %q, want it to start with %q", got, tt.wantErr)
			}
			if name != "" || description != "" {
				t.Errorf("a refused block yielded %q, %q", name, description)
			}
		})
	}
}

// TestClipBoundsTheReason keeps a go-yaml message that quotes the file from
// reaching a warning at the file's own length, or with the control
// characters that drive a terminal.
func TestClipBoundsTheReason(t *testing.T) {
	t.Parallel()
	_, _, err := SkillFrontmatter("---\nname: a\ndescription: *" + strings.Repeat("z", 60_000) + "\n---\n")
	if err == nil {
		t.Fatal("an undefined alias parsed")
	}
	if len(err.Error()) > 200 {
		t.Errorf("warning is %d bytes long:\n%.200s…", len(err.Error()), err)
	}
	if strings.ContainsAny(err.Error(), "\x1b\x00\r\n\t") {
		t.Errorf("warning carries a control character: %q", err)
	}
}
