package gitx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

// TestIndexInfo translates diff-tree's change set of a skill directory
// into index records under the skill's path from the branch's root.
func TestIndexInfo(t *testing.T) {
	t.Parallel()
	const (
		zero = "0000000000000000000000000000000000000000"
		a    = "1111111111111111111111111111111111111111"
		b    = "2222222222222222222222222222222222222222"
	)
	tests := []struct {
		name, diff, prefix, want, err string
	}{
		{"an addition", ":000000 100644 " + zero + " " + b + " A\x00new.md\x00", "skills/pdf",
			"100644 " + b + "\tskills/pdf/new.md\x00", ""},
		{"a change, its mode kept", ":100755 100755 " + a + " " + b + " M\x00run.sh\x00", "pdf",
			"100755 " + b + "\tpdf/run.sh\x00", ""},
		{"a deletion", ":100644 000000 " + a + " " + zero + " D\x00old.md\x00", "pdf",
			"0 " + zero + "\tpdf/old.md\x00", ""},
		{"a mode change", ":100644 100755 " + a + " " + a + " M\x00run.sh\x00", "pdf",
			"100755 " + a + "\tpdf/run.sh\x00", ""},
		{"a nested prefix and path", ":100644 100644 " + a + " " + b + " M\x00docs/a.md\x00", "group/skills/pdf",
			"100644 " + b + "\tgroup/skills/pdf/docs/a.md\x00", ""},
		{"a skill at the root", ":100644 100644 " + a + " " + b + " M\x00SKILL.md\x00:000000 100644 " + zero + " " + a + " A\x00b.md\x00", "",
			"100644 " + b + "\tSKILL.md\x00100644 " + a + "\tb.md\x00", ""},
		{"nothing changed", "", "pdf", "", ""},
		{"a symlink added", ":000000 120000 " + zero + " " + a + " A\x00link\x00", "pdf", "", "pdf/link is a symlink"},
		{"a file turned into a symlink", ":100644 120000 " + a + " " + b + " T\x00link\x00", "pdf", "", "pdf/link is a symlink"},
		{"a submodule link", ":000000 160000 " + zero + " " + a + " A\x00vendor\x00", "pdf", "", "pdf/vendor is a submodule link"},
		{"a rename", ":100644 100644 " + a + " " + a + " R100\x00old.md\x00new.md\x00", "pdf", "", "not an addition"},
		{"a record with no path", ":100644 100644 " + a + " " + b + " M", "pdf", "", "with no path"},
		{"a record that is not a change", "100644 " + a + "\x00SKILL.md\x00", "pdf", "", "not a change"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := IndexInfo(tt.diff, tt.prefix)
			if tt.err != "" {
				link := strings.HasSuffix(tt.err, "link")
				if err == nil || errors.Is(err, ErrUnpublishable) != link || !strings.Contains(err.Error(), tt.err) {
					t.Errorf("IndexInfo = %q, %v; want an error naming %q, ErrUnpublishable %v", got, err, tt.err, link)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("IndexInfo = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

// TestApplyChangesKeepsWhatTheBranchHoldsBesideTheEdits applies a skill's
// edits, a change, an addition and a deletion, onto a root that holds a
// file beside the skill and a symlink in it the installed version never
// held, both of which stay.
func TestApplyChangesKeepsWhatTheBranchHoldsBesideTheEdits(t *testing.T) {
	t.Parallel()
	r, gitDir, _ := checkoutRepo(t)
	ctx := context.Background()
	tree := func(edits ...TreeEdit) string {
		t.Helper()
		id, err := r.EditTree(ctx, gitDir, "", edits)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	file := func(path, body string) TreeEdit {
		return TreeEdit{Path: path, Mode: treeid.FileMode, Content: []byte(body)}
	}
	root := tree(file("README.md", "readme\n"), file("skills/pdf/SKILL.md", "one\n"), file("skills/pdf/old.md", "old\n"),
		TreeEdit{Path: "skills/pdf/link", Mode: treeid.SymlinkMode, Content: []byte("SKILL.md")})
	base := tree(file("SKILL.md", "one\n"), file("old.md", "old\n"))
	written := tree(file("SKILL.md", "two\n"), TreeEdit{Path: "new.sh", Mode: treeid.ExecutableMode, Content: []byte("echo\n")})
	got, err := r.ApplyChanges(ctx, gitDir, root, "skills/pdf", base, written)
	if err != nil {
		t.Fatal(err)
	}
	listing, err := r.Isolated(ctx, gitDir, "ls-tree", "-r", "--format=%(objectmode) %(path)", got)
	if err != nil {
		t.Fatal(err)
	}
	equalString(t, "the tree", listing, "100644 README.md\n100644 skills/pdf/SKILL.md\n120000 skills/pdf/link\n100755 skills/pdf/new.sh")
	if body, err := r.Isolated(ctx, gitDir, "cat-file", "blob", got+":skills/pdf/SKILL.md"); err != nil || body != "two" {
		t.Errorf("SKILL.md holds %q, %v; want the edit", body, err)
	}
}

// TestClassifyPush reads the remote's answer to the push of one branch, as
// git push --porcelain and the hosts print it.
func TestClassifyPush(t *testing.T) {
	t.Parallel()
	const (
		ref     = "refs/heads/main"
		sshRead = "fatal: Could not read from remote repository.\n\nPlease make sure you have the correct access rights\nand the repository exists.\n"
	)
	status := func(flag, summary string) string {
		return "To file:///srv/skills.git\n" + flag + "\t1a2b3c4:" + ref + "\t" + summary + "\nDone\n"
	}
	tests := []struct {
		name, stdout, stderr string
		exit                 int
		class                PushClass
		reason               string
	}{
		{"pushed", status(" ", "1111111..2222222"), "", 0, PushPushed, ""},
		{"a new branch", status("*", "[new branch]"), "", 0, PushPushed, ""},
		{"a forced update the lease allowed", status("+", "1111111...2222222 (forced update)"), "", 0, PushPushed, ""},
		{"up to date", status("=", "[up to date]"), "", 0, PushUpToDate, ""},
		{"a status no push of a commit gives", status("-", "[deleted]"), "", 0, PushUnreachable, "git push reported [deleted] for " + ref},
		{"fetch first", status("!", "[rejected] (fetch first)"), "error: failed to push some refs\n", 1, PushMoved, "fetch first"},
		{"non-fast-forward", status("!", "[rejected] (non-fast-forward)"), "", 1, PushMoved, "non-fast-forward"},
		{"a lease that no longer holds", status("!", "[rejected] (stale info)"), "", 1, PushMoved, "stale info"},
		{"a hook with remote lines, three of them kept", status("!", "[remote rejected] (pre-receive hook declined)"),
			"remote: no pushes on Fridays\nremote:\nremote: ask the owner\nremote: see the wiki\nremote: a fourth line\nerror: failed to push some refs to '/srv/skills.git'\n", 1,
			PushDeclined, "pre-receive hook declined: no pushes on Fridays; ask the owner; see the wiki"},
		{"a protected branch", status("!", "[remote rejected] (protected branch hook declined)"),
			"remote: error: GH006: Protected branch update failed for refs/heads/main.\n", 1,
			PushDeclined, "protected branch hook declined: error: GH006: Protected branch update failed for refs/heads/main."},
		{"a rejection git gives no reason for", status("!", "[remote rejected]"), "", 1, PushDeclined, "[remote rejected]"},
		{"a remote that failed to answer", status("!", "[remote failure] (remote failed to report status)"), "", 1, PushUnreachable, "remote failed to report status"},
		{"permission denied", "", "remote: Permission to acme/skills.git denied to someone.\nfatal: unable to access 'https://github.com/acme/skills/': The requested URL returned error: 403\n", 128,
			PushDenied, "Permission to acme/skills.git denied to someone."},
		{"a 403 naming no denial", "", "remote: Forbidden\nfatal: unable to access 'https://git.example.com/skills/': The requested URL returned error: 403\n", 128,
			PushDeclined, "Forbidden"},
		{"single sign-on", "", "remote: The 'acme' organization has enabled or enforced SAML SSO.\nfatal: unable to access 'https://github.com/acme/skills/': The requested URL returned error: 403\n", 128,
			PushDenied, "The 'acme' organization has enabled or enforced SAML SSO."},
		{"could not read Username", "", "fatal: could not read Username for 'https://github.com': terminal prompts disabled\n", 128, PushDenied, noCredentials},
		{"a token refused", "", "fatal: Authentication failed for 'https://github.com/acme/skills/'\n", 128, PushDenied, "fatal: Authentication failed for 'https://github.com/acme/skills/'"},
		{"a DNS failure", "", "ssh: Could not resolve hostname git.example.com: Name or service not known\n" + sshRead, 128,
			PushUnreachable, "ssh: Could not resolve hostname git.example.com: Name or service not known"},
		{"Done after a rejection", "To file:///srv/skills.git\n!\t1a2b3c4:" + ref + "\t[remote rejected] (hook declined)\nDone\n", "", 1, PushDeclined, "hook declined"},
		{"Done and no status", "To file:///srv/skills.git\nDone\n", "", 0, PushUnreachable, "git push exited 0 and said nothing of the branch"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			list, err := ParsePushPorcelain(tt.stdout)
			if err != nil {
				t.Fatal(err)
			}
			var s PushStatus
			found := false
			for _, st := range list {
				if st.To == ref {
					s, found = st, true
				}
			}
			class, reason := ClassifyPush(s, found, tt.stderr, tt.exit)
			if class != tt.class || reason != tt.reason {
				t.Errorf("ClassifyPush = %q, %q; want %q, %q", class, reason, tt.class, tt.reason)
			}
		})
	}
}

// TestDeniedFix is what the user is told to do about a push ClassifyPush
// calls denied, read from the reason it gives.
func TestDeniedFix(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, stderr string
		want         DenialFix
	}{
		{"read-only", "remote: Permission to me/skills.git denied to someone.\nfatal: unable to access 'https://github.com/me/skills.git/': The requested URL returned error: 403\n", FixCredentials},
		{"no credential", "fatal: could not read Username for 'https://github.com': terminal prompts disabled\n", FixCredentials},
		{"expired token", "remote: Invalid username or token.\nfatal: Authentication failed for 'https://github.com/me/skills.git/'\n", FixCredentials},
		{"host not trusted", "Host key verification failed.\nfatal: Could not read from remote repository.\n", FixHostKey},
		{"single sign-on", "remote: The 'acme' organization has enabled or enforced SAML SSO.\nfatal: unable to access 'https://github.com/acme/skills/': The requested URL returned error: 403\n", FixAuthorise},
		{"IP allow list", "ERROR: The 'acme' organization has an IP allow list enabled, and your IP address is not permitted.\n", FixAuthorise},
	} {
		class, reason := ClassifyPush(PushStatus{}, false, tc.stderr, 128)
		if class != PushDenied {
			t.Fatalf("%s: ClassifyPush = %s %q, want denied", tc.name, class, reason)
		}
		if got := DeniedFix(reason); got != tc.want {
			t.Errorf("%s: DeniedFix(%q) = %d, want %d", tc.name, reason, got, tc.want)
		}
	}
}

// TestStderrFor keeps, of a push's stderr, what a branch's answer is
// told: the lines that name no other branch of the push.
func TestStderrFor(t *testing.T) {
	t.Parallel()
	branches := []string{"skills/alpha", "skills/alpha-2", "skills/beta"}
	stderr := "remote: no pushes on Fridays\n" +
		"remote: Create a pull request for 'skills/beta' on GitHub by visiting:\n" +
		"remote:      https://github.com/me/skills/pull/new/skills/beta\n" +
		"remote: refs/heads/skills/alpha-2 is frozen\n" +
		"remote: skills/alpha waits for skills/beta\n" +
		"remote: myskills/beta is not a branch of the push"
	tests := []struct{ branch, want string }{
		{"skills/alpha", "remote: no pushes on Fridays\nremote: skills/alpha waits for skills/beta\nremote: myskills/beta is not a branch of the push"},
		{"skills/alpha-2", "remote: no pushes on Fridays\nremote: refs/heads/skills/alpha-2 is frozen\nremote: myskills/beta is not a branch of the push"},
		{"skills/beta", "remote: no pushes on Fridays\n" +
			"remote: Create a pull request for 'skills/beta' on GitHub by visiting:\n" +
			"remote:      https://github.com/me/skills/pull/new/skills/beta\n" +
			"remote: skills/alpha waits for skills/beta\n" +
			"remote: myskills/beta is not a branch of the push"},
	}
	for _, tt := range tests {
		if got := StderrFor(stderr, tt.branch, branches); got != tt.want {
			t.Errorf("StderrFor(%s) = %q, want %q", tt.branch, got, tt.want)
		}
	}
}

// TestPushBranchNeverForces pins what a publish's push asks git: one
// commit onto one branch, leased on the commit it was read at, signing
// nothing, running no hook, sending no thin pack and fetching nothing
// lazily, and never forced, mirrored or carrying every branch or tag. The
// account remote's pushes, of fork branches and of a deletion, sign
// nothing and carry no push option or tag of the user's either.
func TestPushBranchNeverForces(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out := filepath.Join(dir, "seen")
	script := "#!/bin/sh\n{ echo \"$@\"; /usr/bin/env; } > \"$SEEN\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	r := New(map[string]string{"PATH": dir, "HOME": "/home/someone", "SEEN": out}, false, func(string, ...any) {})
	_, found, _, exit, err := r.PushBranch(context.Background(), "/srv/account.git", "src-abc", "c0ffee", "main", "beef")
	if err != nil || found || exit != 0 {
		t.Fatalf("PushBranch = found %v, exit %d, %v; want git's silence read as no status", found, exit, err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	seen := strings.Split(string(b), "\n")
	push := strings.Fields(seen[0])
	guards := []string{"--porcelain", "--no-signed", "--no-verify", "--no-recurse-submodules", "--no-follow-tags",
		"push.negotiate=false", "push.pushOption=", "core.hooksPath=/dev/null"}
	for _, flag := range append(guards, "--no-thin", "--force-with-lease=refs/heads/main:beef") {
		if !contains(push, flag) {
			t.Errorf("push %q lacks %s", seen[0], flag)
		}
	}
	for _, word := range push {
		if word == "--force" || word == "-f" || word == "--mirror" || word == "--all" || word == "--tags" || strings.HasPrefix(word, "+") {
			t.Errorf("push %q carries %s", seen[0], word)
		}
	}
	if want := []string{"src-abc", "c0ffee:refs/heads/main"}; strings.Join(push[len(push)-2:], " ") != strings.Join(want, " ") {
		t.Errorf("push ends %q, want %q", push[len(push)-2:], want)
	}
	if !contains(seen, "GIT_NO_LAZY_FETCH=1") {
		t.Errorf("the push's environment lacks GIT_NO_LAZY_FETCH=1:\n%s", b)
	}
	for _, tc := range []struct {
		name, refspec string
		push          func()
	}{
		{"Push", "refs/heads/skills/pdf:refs/heads/skills/pdf", func() {
			_, _, _, _ = r.Push(context.Background(), "/srv/account.git", "src-abc", []string{"skills/pdf"})
		}},
		{"DeleteRemoteBranch", ":refs/heads/skills/pdf", func() {
			_, _ = r.DeleteRemoteBranch(context.Background(), "/srv/account.git", "src-abc", "skills/pdf", "beef")
		}},
	} {
		if err := os.Remove(out); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		tc.push()
		b, err := os.ReadFile(out)
		if err != nil {
			t.Fatalf("%s ran no git: %v", tc.name, err)
		}
		line, _, _ := strings.Cut(string(b), "\n")
		words := strings.Fields(line)
		for _, flag := range guards {
			if !contains(words, flag) {
				t.Errorf("%s's push %q lacks %s", tc.name, line, flag)
			}
		}
		if len(words) == 0 || words[len(words)-1] != tc.refspec {
			t.Errorf("%s's push %q does not end with %s", tc.name, line, tc.refspec)
		}
	}
}
