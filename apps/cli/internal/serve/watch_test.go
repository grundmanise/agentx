package serve

import "testing"

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
