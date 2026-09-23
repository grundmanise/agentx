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
