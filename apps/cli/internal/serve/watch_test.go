package serve

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAcceptKeepsTheDirectoriesTheWatchesCover(t *testing.T) {
	t.Parallel()
	flat := []string{"/h/agentx", "/h/agentx/account.git"}
	trees := []string{"/h/agentx/worktrees", "/h/library"}
	tests := []struct {
		dir  string
		want bool
	}{
		{"/h/agentx", true},
		{"/h/agentx/account.git", true},
		{"/h/agentx/account.git/refs", false},
		{"/h/agentx/mutations", false},
		{"/h/library", true},
		{"/h/library/commit", true},
		{"/h/library/commit/scripts", true},
		{"/h/library/.git", false},
		{"/h/library/commit/node_modules/x", false},
		{"/h/library-other", false},
		{"/h", false},
		{"/h/agentx/worktrees/fork/skill", true},
	}
	for _, tt := range tests {
		if got := accept(tt.dir, flat, trees); got != tt.want {
			t.Errorf("accept(%s) = %v, want %v", tt.dir, got, tt.want)
		}
	}
}

func TestExtendAddsTheDirectoriesSymlinksLeadOutOfTheTrees(t *testing.T) {
	t.Parallel()
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	library, outside := filepath.Join(tmp, "library"), filepath.Join(tmp, "outside")
	for _, dir := range []string{
		filepath.Join(library, "commit", "scripts"),
		filepath.Join(library, ".hidden", "skill"),
		filepath.Join(outside, "sub"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for link, target := range map[string]string{
		filepath.Join(library, "linked"):         outside,
		filepath.Join(library, "unhidden"):       filepath.Join(library, ".hidden", "skill"),
		filepath.Join(library, "commit", "self"): filepath.Join(library, "commit"), // a loop, already covered
		filepath.Join(library, "home"):           tmp,                              // a flat directory, watched already
	} {
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}
	got := extend([]string{tmp, library}, []string{library})
	want := []string{library, outside, filepath.Join(library, ".hidden", "skill")} // in ReadDir order: linked, then unhidden
	if !reflect.DeepEqual(got, want) {
		t.Errorf("extend = %v, want %v", got, want)
	}
}
