package source

import (
	"errors"
	"testing"
)

// TestCheckPath refuses every tree entry path that could be laid out
// outside the directory it is read below, and keeps every other name, one
// git would quote included.
func TestCheckPath(t *testing.T) {
	t.Parallel()
	for _, p := range []string{
		"", "/etc/passwd", "..", "../pwned.txt", "a/../../b", ".", "./a", "a/.", "a//b", "a/",
		".git", ".GIT", "sub/.Git/config", `a\b`, "..\\pwned", "a\x00b",
	} {
		if err := CheckPath(p); !errors.Is(err, ErrUnsafePath) {
			t.Errorf("CheckPath(%q) = %v, want ErrUnsafePath", p, err)
		}
	}
	for _, p := range []string{
		"SKILL.md", "scripts/run.sh", ".github/workflow.yml", ".gitignore", "..a", "a..", "...", `"weird".md`, "a\nb.md", "ends\n",
	} {
		if err := CheckPath(p); err != nil {
			t.Errorf("CheckPath(%q) = %v, want nil", p, err)
		}
	}
}

// TestParsePushBranch reads the branch a publish pushes to out of what
// ls-remote --symref prints for HEAD and the refs a pin may name.
func TestParsePushBranch(t *testing.T) {
	t.Parallel()
	const (
		head   = "ref: refs/heads/main\tHEAD\n1111111111111111111111111111111111111111\tHEAD\n"
		branch = "2222222222222222222222222222222222222222\trefs/heads/release\n"
		tag    = "3333333333333333333333333333333333333333\trefs/tags/release\n"
	)
	tests := []struct {
		name, out, pin, want string
	}{
		{"HEAD's symref", head, "", "main"},
		{"a branch pin", head + branch, "release", "release"},
		{"a tag pin", head + "3333333333333333333333333333333333333333\trefs/tags/v1\n4444444444444444444444444444444444444444\trefs/tags/v1^{}\n", "v1", ""},
		{"a name both tag and branch, which git reads as the tag", head + tag + branch, "release", ""},
		{"a commit pin", head, "1111111111111111111111111111111111111111", ""},
		{"a refs/heads/ pin", head + branch, "refs/heads/release", "release"},
		{"a refs/tags/ pin", head + tag, "refs/tags/release", ""},
		{"no HEAD", "", "", ""},
		{"a HEAD that names no branch", "1111111111111111111111111111111111111111\tHEAD\n", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parsePushBranch(tt.out, tt.pin)
			if tt.want == "" {
				if !errors.Is(err, ErrNotABranch) {
					t.Errorf("parsePushBranch = %q, %v, want ErrNotABranch", got, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("parsePushBranch = %q, %v, want %q", got, err, tt.want)
			}
		})
	}
}
