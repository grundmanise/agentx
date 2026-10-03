package gitx

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// TestClassifyAccess reads the answers git and the hosts agentx knows give
// to the access check's dry run, as they print them.
func TestClassifyAccess(t *testing.T) {
	t.Parallel()
	const sshRead = "fatal: Could not read from remote repository.\n\nPlease make sure you have the correct access rights\nand the repository exists.\n"
	tests := []struct {
		name      string
		status    int
		stdout    string
		stderr    string
		access    string
		reason    string
		authorise bool
	}{
		{"writable", 0, "To file:///srv/skills.git\n-\t:refs/heads/agentx-access-check\t[deleted]\nDone\n", "", home.AccessWritable, "", false},
		{"exit 0 without the ref's status", 0, "To file:///srv/skills.git\nDone\n", "", home.AccessUnknown, "git push exited 0 and said nothing", false},
		{"the ref rejected", 1, "To file:///srv/skills.git\n!\t:refs/heads/agentx-access-check\t[remote rejected] (deletion prohibited)\nDone\n", "", home.AccessUnknown, "[remote rejected] (deletion prohibited)", false},
		{"GitHub over HTTPS", 128, "",
			"remote: Permission to acme/skills.git denied to someone.\nfatal: unable to access 'https://github.com/acme/skills/': The requested URL returned error: 403\n",
			home.AccessReadOnly, "remote: Permission to acme/skills.git denied to someone.", false},
		{"GitHub single sign-on", 128, "",
			"remote: The 'acme' organization has enabled or enforced SAML SSO.\nremote: To access this repository, you must re-authorize the OAuth Application.\nfatal: unable to access 'https://github.com/acme/skills/': The requested URL returned error: 403\n",
			home.AccessUnknown, "remote: The 'acme' organization has enabled or enforced SAML SSO.", true},
		{"an IP allow list", 128, "",
			"remote: Although you appear to have the correct authorization credentials,\nremote: the `acme` organization has an IP allow list enabled.\nfatal: unable to access 'https://github.com/acme/skills/': The requested URL returned error: 403\n",
			home.AccessUnknown, "remote: the `acme` organization has an IP allow list enabled.", true},
		{"GitHub single sign-on over SSH", 128, "", "ERROR: The 'acme' organization has enabled or enforced SAML SSO. To access this repository, you must use the HTTPS remote with a personal access token or SSH key and authorize it for this organization.\n" + sshRead,
			home.AccessUnknown, "ERROR: The 'acme' organization has enabled or enforced SAML SSO. To access this repository, you must use the HTTPS remote with a personal access token or SSH key and authorize it for this organization.", true},
		{"a 403 naming no denial", 128, "", "fatal: unable to access 'https://git.example.com/skills/': The requested URL returned error: 403\n",
			home.AccessUnknown, "fatal: unable to access 'https://git.example.com/skills/': The requested URL returned error: 403", false},
		{"a 403 with a remote line", 128, "", "remote: Forbidden\nfatal: unable to access 'https://git.example.com/skills/': The requested URL returned error: 403\n",
			home.AccessUnknown, "remote: Forbidden", false},
		{"GitHub over SSH", 128, "", "ERROR: Permission to acme/skills.git denied to deploy-key.\n" + sshRead,
			home.AccessReadOnly, "ERROR: Permission to acme/skills.git denied to deploy-key.", false},
		{"a GitHub deploy key", 128, "", "ERROR: The key you are authenticated with has been marked as read only.\n" + sshRead,
			home.AccessReadOnly, "ERROR: The key you are authenticated with has been marked as read only.", false},
		{"an archived repository", 128, "", "ERROR: This repository was archived so it is read-only.\n" + sshRead,
			home.AccessReadOnly, "ERROR: This repository was archived so it is read-only.", false},
		{"gitolite", 128, "", "FATAL: W any skills someone DENIED by fallthru\n(or you mis-spelled the reponame)\n" + sshRead,
			home.AccessReadOnly, "FATAL: W any skills someone DENIED by fallthru", false},
		{"a git daemon", 128, "", "fatal: remote error: access denied or repository not exported: /skills.git\n",
			home.AccessReadOnly, "fatal: remote error: access denied or repository not exported: /skills.git", false},
		{"a Bitbucket deploy key", 128, "", "repository access denied. access via a deployment key is read-only.\n" + sshRead,
			home.AccessReadOnly, "repository access denied. access via a deployment key is read-only.", false},
		{"GitLab", 1, "", "remote: \nremote: ========================================================================\nremote: \nremote: You are not allowed to push code to this project.\nremote: \nfatal: Could not read from remote repository.\n",
			home.AccessReadOnly, "remote: You are not allowed to push code to this project.", false},
		{"Azure DevOps", 128, "", "remote: TF401027: You need the Git 'GenericContribute' permission to perform this action.\nfatal: unable to access 'https://dev.azure.com/acme/skills/': The requested URL returned error: 403\n",
			home.AccessReadOnly, "remote: TF401027: You need the Git 'GenericContribute' permission to perform this action.", false},
		{"a path git cannot write", 1, "", "error: insufficient permission for adding an object to repository database ./objects\n",
			home.AccessReadOnly, "error: insufficient permission for adding an object to repository database ./objects", false},
		{"no user name without asking", 128, "", "fatal: could not read Username for 'https://github.com': terminal prompts disabled\n",
			home.AccessUnknown, noCredentials, false},
		{"prompts disabled", 128, "", "fatal: terminal prompts disabled\n", home.AccessUnknown, noCredentials, false},
		{"a token refused", 128, "", "remote: Invalid username or password.\nfatal: Authentication failed for 'https://github.com/acme/skills/'\n",
			home.AccessUnknown, "fatal: Authentication failed for 'https://github.com/acme/skills/'", false},
		{"no key", 255, "", "git@github.com: Permission denied (publickey).\n" + sshRead, home.AccessUnknown, noCredentials, false},
		{"a host not trusted yet", 128, "", "Host key verification failed.\n" + sshRead, home.AccessUnknown, "Host key verification failed.", false},
		{"no such host", 128, "", "ssh: Could not resolve hostname git.example.com: Name or service not known\n" + sshRead,
			home.AccessUnknown, "ssh: Could not resolve hostname git.example.com: Name or service not known", false},
		{"not a repository", 128, "", "fatal: '/srv/gone.git' does not appear to be a git repository\n" + sshRead,
			home.AccessUnknown, "fatal: '/srv/gone.git' does not appear to be a git repository", false},
		{"nothing said", 1, "", "", home.AccessUnknown, "git push exited 1 and said nothing", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ClassifyAccess(tt.status, tt.stdout, tt.stderr)
			want := Access{Access: tt.access, Reason: tt.reason, Authorise: tt.authorise}
			if got != want {
				t.Errorf("ClassifyAccess = %+v, want %+v", got, want)
			}
		})
	}
}

// TestProbeAccessRunsADryRunAndNothingElse pins what the access check asks
// git: a dry run of a deletion that signs nothing and runs no hook, after
// one read of the configuration that applies to the source's URL alone.
func TestProbeAccessRunsADryRunAndNothingElse(t *testing.T) {
	t.Parallel()
	env := stubGit(t)
	var mu sync.Mutex
	var commands []string
	logf := func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		if strings.HasPrefix(format, "git %s") {
			commands = append(commands, args[0].(string))
		}
	}
	got := New(env, false, logf).ProbeAccess(context.Background(), "https://github.com/acme/skills")
	if got.Access != home.AccessUnknown { // the stub prints no ref's status
		t.Errorf("access = %+v", got)
	}
	if len(commands) != 1 {
		t.Fatalf("commands = %q, want the push alone", commands)
	}
	push := strings.Fields(commands[0])
	for _, flag := range []string{"--dry-run", "--porcelain", "--no-signed", "--no-verify", "--no-recurse-submodules", "--no-follow-tags", "push.negotiate=false", "push.pushOption=", "core.hooksPath=/dev/null",
		"remote.agentx.url=https://github.com/acme/skills"} {
		if !contains(push, flag) {
			t.Errorf("push %q lacks %s", commands[0], flag)
		}
	}
	for _, word := range push {
		if word == "--force" || word == "--mirror" || word == "--all" || word == "--tags" || strings.HasPrefix(word, "+") || strings.HasPrefix(word, "--force-with-lease") {
			t.Errorf("push %q carries %s", commands[0], word)
		}
	}
	if want := []string{CheckRemote, ":" + AccessCheckRef}; !reflect.DeepEqual(push[len(push)-2:], want) {
		t.Errorf("push ends %q, want %q", push[len(push)-2:], want)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func equalString(t *testing.T, what, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %q, want %q", what, got, want)
	}
}

// TestProbeAccessAppliesTheIncludesOfItsOwnURL checks why the access check
// runs in a repository of its own: a setting the user keys on a remote's
// URL applies to that source alone and never to another, as it would
// inside the account repo, which holds every source's remote. It also
// checks that the user's pushInsteadOf applies to the check as it does to
// a real push.
func TestProbeAccessAppliesTheIncludesOfItsOwnURL(t *testing.T) {
	t.Parallel()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Each SSH command answers as GitHub does to a key of another account,
	// naming the one it stands for, so the answer tells which one ran.
	for _, name := range []string{"work", "personal"} {
		writeShim(t, filepath.Join(dir, name+"-ssh"), "#!/bin/sh\necho 'ERROR: Permission to x.git denied to "+name+".' >&2\nexit 128\n")
	}
	work := filepath.Join(dir, "work.gitconfig")
	write(t, work, "[core]\n\tsshCommand = "+filepath.Join(dir, "work-ssh")+"\n")
	global := filepath.Join(dir, "gitconfig")
	env := map[string]string{"PATH": os.Getenv("PATH"), "HOME": dir, "GIT_CONFIG_GLOBAL": global, "GIT_CONFIG_NOSYSTEM": "1"}
	r := New(env, false, func(string, ...any) {})
	ctx := context.Background()

	// The access check of each source runs the SSH command the user's
	// configuration names for it, the work one for the source the include
	// is keyed on and the user's own for every other. The URLs are
	// canonical, as checkSource passes them, so the include is keyed on the
	// canonical form. A source fetched over HTTPS that the user's
	// pushInsteadOf rewrites to SSH is checked over SSH, as a real push to
	// it would go.
	write(t, global, "[core]\n\tsshCommand = "+filepath.Join(dir, "personal-ssh")+"\n"+
		"[includeIf \"hasconfig:remote.*.url:ssh://git@github-work/**\"]\n\tpath = "+work+"\n"+
		"[url \"git@github.com:\"]\n\tpushInsteadOf = https://github.com/me/\n")
	for _, c := range []struct{ url, who string }{
		{"ssh://git@github-work/acme/skills", "work"},
		{"ssh://git@github.com/me/skills", "personal"},
		{"https://github.com/me/other", "personal"},
	} {
		got := r.ProbeAccess(ctx, c.url)
		equalString(t, "reason of "+c.url, got.Reason, "ERROR: Permission to x.git denied to "+c.who+".")
	}
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseSymref(t *testing.T) {
	t.Parallel()
	for out, want := range map[string]string{
		"ref: refs/heads/main\tHEAD\n1234567890123456789012345678901234567890\tHEAD\n": "main",
		"ref: refs/heads/release/1.x\tHEAD\n":                                          "release/1.x",
		"1234567890123456789012345678901234567890\tHEAD\n":                             "", // a detached HEAD
		"ref: refs/tags/v1\tHEAD\n":                                                    "",
		"ref: refs/heads/main\trefs/remotes/origin/HEAD\n":                             "",
		"": "",
	} {
		if got := ParseSymref(out); got != want {
			t.Errorf("ParseSymref(%q) = %q, want %q", out, got, want)
		}
	}
}

func TestLocalPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		url  string
		path string
		ok   bool
	}{
		{"file:///srv/skills.git", "/srv/skills.git", true},
		{"file://localhost/srv/skills.git", "/srv/skills.git", true},
		{"file:///srv/my%20skills", "/srv/my skills", true},
		{"/srv/skills.git", "/srv/skills.git", true},
		{"../skills", "../skills", true},
		{"https://github.com/acme/skills", "", false},
		{"ssh://git@github.com/acme/skills", "", false},
		{"git@github.com:acme/skills.git", "", false},
		{"host:skills", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		path, ok := LocalPath(tt.url)
		if path != tt.path || ok != tt.ok {
			t.Errorf("LocalPath(%q) = %q, %v, want %q, %v", tt.url, path, ok, tt.path, tt.ok)
		}
	}
}

// TestRepoDirs finds the directories a push writes the way git finds the
// repository: a bare one, one with a work tree, one whose .git is a file,
// a path that leaves out .git, and a reftable repository.
func TestRepoDirs(t *testing.T) {
	t.Parallel()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := New(map[string]string{"PATH": os.Getenv("PATH")}, false, func(string, ...any) {})
	git := func(args ...string) {
		t.Helper()
		if _, err := r.run(context.Background(), call{isolated: true}, args...); err != nil {
			t.Fatal(err)
		}
	}
	bare := filepath.Join(root, "bare.git")
	git("init", "--quiet", "--bare", bare)
	work := filepath.Join(root, "work")
	git("init", "--quiet", work)
	separate := filepath.Join(root, "separate")
	git("init", "--quiet", "--separate-git-dir", filepath.Join(root, "elsewhere.git"), separate)
	// A reftable repository keeps its refs in reftable/, which git 2.45
	// and newer write; the directory is what RepoDirs looks for.
	reftable := filepath.Join(root, "reftable.git")
	git("init", "--quiet", "--bare", reftable)
	if err := os.Mkdir(filepath.Join(reftable, "reftable"), 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		path string
		want []string
	}{
		{"bare", bare, []string{bare, bare + "/objects", bare + "/refs/heads"}},
		{"bare without .git", filepath.Join(root, "bare"), []string{bare, bare + "/objects", bare + "/refs/heads"}},
		{"work tree", work, []string{work + "/.git", work + "/.git/objects", work + "/.git/refs/heads"}},
		{".git file", separate, []string{root + "/elsewhere.git", root + "/elsewhere.git/objects", root + "/elsewhere.git/refs/heads"}},
		{"reftable", reftable, []string{reftable, reftable + "/objects", reftable + "/reftable"}},
		{"no repository", filepath.Join(root, "nothing"), nil},
	}
	for _, tt := range tests {
		got, ok := RepoDirs(tt.path)
		if ok != (tt.want != nil) || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: RepoDirs = %q, %v, want %q", tt.name, got, ok, tt.want)
		}
	}
}
