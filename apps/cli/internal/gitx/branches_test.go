package gitx

import (
	"reflect"
	"testing"
	"time"
)

// TestParseBranchTips is the read of one ls-remote that asks for every
// report branch and the cloud machine's branch: a branch is kept by its
// short name when a pattern names it, and a ref the tail match of
// ls-remote lets in is not.
func TestParseBranchTips(t *testing.T) {
	t.Parallel()
	patterns := []string{ReportBranches, MachineBranch("cloud")}
	for _, tc := range []struct {
		name string
		out  string
		want map[string]string
	}{
		{"nothing", "", map[string]string{}},
		{"reports and the cloud machine",
			"1111\trefs/heads/machines/cloud\n2222\trefs/heads/reports/0123456789abcdef0123456789abcdef\n3333\trefs/heads/reports/fedcba9876543210fedcba9876543210\n",
			map[string]string{"machines/cloud": "1111", "reports/0123456789abcdef0123456789abcdef": "2222", "reports/fedcba9876543210fedcba9876543210": "3333"}},
		{"a report branch of any name is the caller's to judge",
			"1111\trefs/heads/reports/old/laptop\n",
			map[string]string{"reports/old/laptop": "1111"}},
		{"another machine's branch and a branch below the cloud machine's",
			"1111\trefs/heads/machines/cloudy\n2222\trefs/heads/machines/cloud/extra\n3333\trefs/heads/machines/0123456789abcdef0123456789abcdef\n",
			map[string]string{}},
		{"a ref whose tail matches",
			"1111\trefs/remotes/mirror/refs/heads/reports/a\n2222\trefs/tags/refs/heads/machines/cloud\n3333\trefs/heads/skills/reports/a\n",
			map[string]string{}},
		{"the prefix alone, a line with no commit and a line that is no ref",
			"1111\trefs/heads/reports/\n\trefs/heads/reports/a\nwarning: something\n\n2222\trefs/heads/reports/b\r\n",
			map[string]string{"reports/b": "2222"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := parseBranchTips(tc.out, patterns); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseBranchTips = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestParseTracked is the read of the remote-tracking refs the same
// patterns name: for-each-ref also answers for the refs below a name, and
// prints no time for an object that is no commit.
func TestParseTracked(t *testing.T) {
	t.Parallel()
	const prefix = "refs/remotes/src-1/"
	patterns := []string{ReportBranches, MachineBranch("cloud")}
	at := time.Unix(1700000000, 0).UTC()
	for _, tc := range []struct {
		name string
		out  string
		want map[string]Tracked
	}{
		{"nothing", "", map[string]Tracked{}},
		{"a report and the cloud machine",
			prefix + "reports/a\x001111\x001700000000\n" + prefix + "machines/cloud\x002222\x001700000000\n",
			map[string]Tracked{"reports/a": {Commit: "1111", Committed: at}, "machines/cloud": {Commit: "2222", Committed: at}}},
		{"a ref below the cloud machine's and one of another remote",
			prefix + "machines/cloud/extra\x001111\x001700000000\nrefs/remotes/src-2/reports/a\x002222\x001700000000\n",
			map[string]Tracked{}},
		{"an object that is no commit has no time",
			prefix + "reports/a\x001111\x00\n",
			map[string]Tracked{"reports/a": {Commit: "1111"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := parseTracked(tc.out, prefix, patterns); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseTracked = %v, want %v", got, tc.want)
			}
		})
	}
}
