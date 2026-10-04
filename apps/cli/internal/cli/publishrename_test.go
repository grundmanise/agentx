package cli

import (
	"fmt"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
)

// TestRenamedBranches: a branch of the account remote this machine holds
// no skill of pairs with the one skill of the publish that holds its fork
// id, and with nothing when no skill or two of them hold it, when its id
// is empty, or when the publish holds the name itself.
func TestRenamedBranches(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                 string
		selected, remoteOnly map[string]string
		want                 map[string]string
	}{
		{"one match", map[string]string{"new": "id-1", "other": "id-2"}, map[string]string{"old": "id-1"}, map[string]string{"old": "new"}},
		{"a different id", map[string]string{"new": "id-1"}, map[string]string{"old": "id-2"}, map[string]string{}},
		{"an ambiguous id", map[string]string{"new": "id-1", "old-installed": "id-1"}, map[string]string{"old": "id-1"}, map[string]string{}},
		{"a remote name held locally", map[string]string{"old": "id-1"}, map[string]string{"old": "id-1"}, map[string]string{}},
		{"an empty id", map[string]string{"new": ""}, map[string]string{"old": ""}, map[string]string{}},
	} {
		equal(t, tc.name, fmt.Sprint(renamedBranches(tc.selected, tc.remoteOnly)), fmt.Sprint(tc.want))
	}
}

// TestRenamesFrom: a skill's history since the old name's branch tells a
// rename from old only by the subject skill rename writes of it.
func TestRenamesFrom(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, subjects string
		want           bool
	}{
		{"the rename, under an edit", "an edit\nRename old to new", true},
		{"a rename onwards", "Rename old to new\nRename new to newer", true},
		{"no rename", "an edit\nanother", false},
		{"another skill's rename", "Rename old-2 to new", false},
		{"a rename to it", "Rename new to old", false},
		{"nothing since the branch", "", false},
	} {
		equal(t, tc.name, renamesFrom(tc.subjects, "old"), tc.want)
	}
}

// TestRenameDeleteWarning: a deletion of the old name's branch the
// account remote rejected says why, as a removal does, and that the next
// publish tries again, once the default branch is another one when that
// is why.
func TestRenameDeleteWarning(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		status gitx.PushStatus
		says   string
		hint   string
	}{
		{"the default branch",
			gitx.PushStatus{Flag: '!', Summary: "[remote rejected]", Reason: "refusing to delete the current branch: refs/heads/skills/old"},
			"the account remote still holds skills/old: the account remote rejected its deletion: refusing to delete the current branch",
			"make another branch the account remote's default branch on its hosting service; the next 'agentx skill publish new' tries again"},
		{"moved since the fetch",
			gitx.PushStatus{Flag: '!', Summary: "[rejected]", Reason: "stale info"},
			"rejected its deletion: stale info",
			"the next 'agentx skill publish new' tries again"},
	} {
		message, hint := renameDeleteWarning("old", "new", tc.status)
		if !strings.Contains(message, tc.says) {
			t.Errorf("%s: message = %q, want it to hold %q", tc.name, message, tc.says)
		}
		equal(t, tc.name+": hint", hint, tc.hint)
	}
}
