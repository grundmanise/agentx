package cli

import (
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// TestRenamedDrops: a publish deletes the old name's branch of each
// skill it pushed whose branch it created on the account remote, once per
// old name, by the first skill by name renamed from it, and none of a
// skill it found up to date, of one whose name the remote held already,
// as every later publish finds it, or of one whose old name this machine
// holds a skill of its own of. An old name held here as a shared
// source's skill does not keep the branch.
func TestRenamedDrops(t *testing.T) {
	t.Parallel()
	own := func(name, id string, renamed ...string) lineage.Record {
		return lineage.Record{Name: name, Kind: lineage.KindFork, Fork: &lineage.ForkLineage{ID: id, Renamed: renamed}}
	}
	pushed := func(name string) *publishing {
		return &publishing{name: name, outcome: publishPushed, commit: "c-" + name}
	}
	walked := map[string]lineage.ForkLineage{"o": {ID: "1"}}
	for _, tc := range []struct {
		name    string
		records []lineage.Record
		list    []*publishing
		tips    map[string]string
		want    string
	}{
		{"the first publish of the new name", []lineage.Record{own("new", "1", "old")},
			[]*publishing{pushed("new")}, map[string]string{"old": "o"}, "old>new@c-new"},
		{"up to date", []lineage.Record{own("new", "1", "old")},
			[]*publishing{{name: "new", outcome: publishUpToDate, commit: "c-new"}}, map[string]string{"old": "o"}, ""},
		{"a later publish", []lineage.Record{own("new", "1", "old")},
			[]*publishing{pushed("new")}, map[string]string{"new": "n", "old": "o"}, ""},
		{"the old name held here", []lineage.Record{own("new", "1", "old"), own("old", "1")},
			[]*publishing{pushed("new")}, map[string]string{"old": "o"}, ""},
		{"the old name held here as a shared source's", []lineage.Record{own("new", "1", "old"), {Name: "old", Kind: lineage.KindManaged}},
			[]*publishing{pushed("new")}, map[string]string{"old": "o"}, "old>new@c-new"},
		{"two renamed from one old name", []lineage.Record{own("b2", "1", "old"), own("b1", "1", "old")},
			[]*publishing{pushed("b2"), pushed("b1")}, map[string]string{"old": "o"}, "old>b1@c-b1"},
	} {
		records := map[string]lineage.Record{}
		for _, r := range tc.records {
			records[r.Name] = r
		}
		var got []string
		for _, d := range renamedDrops(tc.list, records, remoteForks{tips: tc.tips, walked: walked}) {
			got = append(got, d.old+">"+d.name+"@"+d.tip)
		}
		equal(t, tc.name, strings.Join(got, " "), tc.want)
	}
}

// TestRenameDeleteWarning: a deletion of the old name's branch the
// account remote rejected says why, as a removal does, and names the
// removal that deletes it, since no later publish retries it: once the
// default branch is another one when that is why, and with the install
// beside the renamed skill when another machine moved it.
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
			"make another branch the account remote's default branch on its hosting service, then delete it with 'agentx skill remove old --remote'"},
		{"moved since the fetch",
			gitx.PushStatus{Flag: '!', Summary: "[rejected]", Reason: "stale info"},
			"rejected its deletion: stale info",
			"another machine moved it since the fetch: install it beside new with 'agentx skill add --name old', or delete it with 'agentx skill remove old --remote'"},
	} {
		message, hint := renameDeleteWarning("old", "new", tc.status)
		if !strings.Contains(message, tc.says) {
			t.Errorf("%s: message = %q, want it to hold %q", tc.name, message, tc.says)
		}
		equal(t, tc.name+": hint", hint, tc.hint)
	}
}
