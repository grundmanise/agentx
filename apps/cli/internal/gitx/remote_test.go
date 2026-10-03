package gitx

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParsePushPorcelain(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		out  string
		want []PushStatus
		err  bool
	}{
		{"pushed and new", "To file:///remote.git\n \trefs/heads/skills/a:refs/heads/skills/a\t1a2b3c4..5d6e7f8\n*\trefs/heads/skills/b:refs/heads/skills/b\t[new branch]\nDone\n",
			[]PushStatus{
				{Flag: ' ', From: "refs/heads/skills/a", To: "refs/heads/skills/a", Summary: "1a2b3c4..5d6e7f8"},
				{Flag: '*', From: "refs/heads/skills/b", To: "refs/heads/skills/b", Summary: "[new branch]"},
			}, false},
		{"rejected with a reason", "To git@example.com:me/skills.git\n!\trefs/heads/skills/a:refs/heads/skills/a\t[rejected] (fetch first)\n=\trefs/heads/skills/b:refs/heads/skills/b\t[up to date]\n",
			[]PushStatus{
				{Flag: '!', From: "refs/heads/skills/a", To: "refs/heads/skills/a", Summary: "[rejected]", Reason: "fetch first"},
				{Flag: '=', From: "refs/heads/skills/b", To: "refs/heads/skills/b", Summary: "[up to date]"},
			}, false},
		{"refused by a hook of the remote", "!\trefs/heads/skills/a:refs/heads/skills/a\t[remote rejected] (pre-receive hook declined)\n",
			[]PushStatus{{Flag: '!', From: "refs/heads/skills/a", To: "refs/heads/skills/a", Summary: "[remote rejected]", Reason: "pre-receive hook declined"}}, false},
		{"nothing", "", nil, false},
		{"not a status", "error: something\n", nil, true},
		{"no refspec", "!\trefs/heads/skills/a\t[rejected]\n", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParsePushPorcelain(tc.out)
			if (err != nil) != tc.err {
				t.Fatalf("ParsePushPorcelain error = %v, want error %v", err, tc.err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ParsePushPorcelain = %#v, want %#v", got, tc.want)
			}
		})
	}
}

// TestUnsetRemoteTakesOutOnlyTheRemoteItNames: two remotes of fork
// branches in one account repo, each with a fork branch tracking it and a
// remote-tracking branch, and unsetting one leaves the other's
// configuration, tracking and remote-tracking branch as they were.
func TestUnsetRemoteTakesOutOnlyTheRemoteItNames(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	ctx := context.Background()
	gitDir := filepath.Join(t.TempDir(), "account.git")
	r := New(map[string]string{"PATH": os.Getenv("PATH"), "HOME": t.TempDir()}, false, func(string, ...any) {})
	if _, err := r.Isolated(ctx, gitDir, "init", "--bare", "--quiet", gitDir); err != nil {
		t.Fatal(err)
	}
	empty, err := r.Isolated(ctx, gitDir, "hash-object", "-t", "tree", "-w", "--stdin")
	if err != nil {
		t.Fatal(err)
	}
	commit, err := r.IsolatedAt(ctx, gitDir, "1700000000 +0000", "commit-tree", empty, "-m", "one")
	if err != nil {
		t.Fatal(err)
	}
	const kept, gone = "origin", "src-0123456789abcdef"
	for _, rb := range [][2]string{{kept, "skills/a"}, {gone, "skills/b"}} {
		remote, branch := rb[0], rb[1]
		if err := r.SetRemote(ctx, gitDir, remote, "file:///srv/"+remote+".git"); err != nil {
			t.Fatal(err)
		}
		if err := r.SetTracking(ctx, gitDir, remote, branch); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Isolated(ctx, gitDir, "update-ref", TrackingPrefix(remote)+branch, commit); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.UnsetRemote(ctx, gitDir, gone); err != nil {
		t.Fatal(err)
	}
	config, err := r.Isolated(ctx, gitDir, "config", "--local", "--get-regexp", `^(remote|branch)\.`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"remote.origin.url file:///srv/origin.git",
		"remote.origin.fetch " + ForkRefspec(kept),
		"remote.origin.tagopt --no-tags",
		"branch.skills/a.remote origin",
		"branch.skills/a.merge refs/heads/skills/a",
	}
	if got := strings.Split(config, "\n"); !reflect.DeepEqual(got, want) {
		t.Errorf("configuration after unsetting %s = %q, want %q", gone, got, want)
	}
	refs, err := r.Isolated(ctx, gitDir, "for-each-ref", "--format=%(refname)", "refs/remotes/")
	if err != nil {
		t.Fatal(err)
	}
	if refs != TrackingPrefix(kept)+"skills/a" {
		t.Errorf("remote-tracking branches after unsetting %s = %q, want only %s's", gone, refs, kept)
	}
}
