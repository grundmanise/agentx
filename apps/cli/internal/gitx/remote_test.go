package gitx

import (
	"reflect"
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
