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
			home.AccessUnknown, "remote: Although you appear to have the correct authorization credentials,", true},
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
			home.AccessUnknown, noCredentials, false},
		{"no key", 255, "", "git@github.com: Permission denied (publickey).\n" + sshRead, home.AccessUnknown, noCredentials, false},
		{"a host not trusted yet", 128, "", "Host key verification failed.\n" + sshRead, home.AccessUnknown, noCredentials, false},
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
// one read of the configuration that applies to the source's URLs alone.
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
	got := New(env, false, logf).ProbeAccess(context.Background(), "/acct.git", "src-0123456789abcdef", "https://github.com/acme/skills", "git@github-work:acme/skills.git")
	if got.Access != home.AccessUnknown { // the stub prints no ref's status
		t.Errorf("access = %+v", got)
	}
	if len(commands) != 2 {
		t.Fatalf("commands = %q, want the configuration read and the push", commands)
	}
	equalString(t, "configuration read", commands[0],
		"-c remote.agentx.url=https://github.com/acme/skills -c remote.agentx.url=git@github-work:acme/skills.git config -z --get-regexp "+targetKeys)
	push := strings.Fields(commands[1])
	for _, flag := range []string{"--dry-run", "--porcelain", "--no-signed", "--no-verify", "--no-recurse-submodules", "--no-follow-tags", "push.negotiate=false", "core.hooksPath=/dev/null"} {
		if !contains(push, flag) {
			t.Errorf("push %q lacks %s", commands[1], flag)
		}
	}
	for _, word := range push {
		if word == "--force" || word == "--mirror" || word == "--all" || word == "--tags" || strings.HasPrefix(word, "+") || strings.HasPrefix(word, "--force-with-lease") {
			t.Errorf("push %q carries %s", commands[1], word)
		}
	}
	if want := []string{"src-0123456789abcdef", ":" + AccessCheckRef}; !reflect.DeepEqual(push[len(push)-2:], want) {
		t.Errorf("push ends %q, want %q", push[len(push)-2:], want)
	}

	// Over paths no SSH command applies, and the configuration is not read.
	commands = nil
	New(env, false, logf).ProbeAccess(context.Background(), "/acct.git", "src-0123456789abcdef", "file:///srv/skills.git")
	if len(commands) != 1 || !strings.Contains(commands[0], " push --dry-run ") {
		t.Errorf("commands over a path = %q, want the push alone", commands)
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

// TestTargetConfigAppliesTheIncludesOfOneURL checks the reason
// TargetConfig exists: a setting the user keys on a remote's URL applies to
// that URL alone, whichever of the source's URLs it names.
func TestTargetConfigAppliesTheIncludesOfOneURL(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	work := filepath.Join(dir, "work.gitconfig")
	write(t, work, "[core]\n\tsshCommand = ssh -i ~/.ssh/work\n[user]\n\temail = me@work.example\n")
	global := filepath.Join(dir, "gitconfig")
	write(t, global, "[user]\n\tname = Someone\n\temail = me@home.example\n"+
		"[includeIf \"hasconfig:remote.*.url:git@github-work:*/**\"]\n\tpath = "+work+"\n")
	r := New(map[string]string{"PATH": os.Getenv("PATH"), "HOME": dir, "GIT_CONFIG_GLOBAL": global, "GIT_CONFIG_NOSYSTEM": "1"}, false, func(string, ...any) {})
	ctx := context.Background()

	work1, err := r.TargetConfig(ctx, "https://github.com/acme/skills", "git@github-work:acme/skills.git")
	if err != nil {
		t.Fatal(err)
	}
	equalString(t, "sshCommand for work", work1.SSHCommand, "ssh -i ~/.ssh/work")
	equalString(t, "email for work", work1.Values["user.email"], "me@work.example")
	equalString(t, "name for work", work1.Values["user.name"], "Someone")

	other, err := r.TargetConfig(ctx, "https://github.com/someone/skills")
	if err != nil {
		t.Fatal(err)
	}
	equalString(t, "sshCommand elsewhere", other.SSHCommand, "")
	equalString(t, "email elsewhere", other.Values["user.email"], "me@home.example")
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseTargetConfig(t *testing.T) {
	t.Parallel()
	got := ParseTargetConfig("user.name\nSomeone\x00user.email\nfirst@example.com\x00user.email\nlast@example.com\x00commit.gpgsign\x00core.sshCommand\nssh -i key\x00")
	want := TargetConfig{SSHCommand: "ssh -i key", Values: map[string]string{
		"user.name": "Someone", "user.email": "last@example.com", "commit.gpgsign": "", "core.sshcommand": "ssh -i key",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseTargetConfig = %+v, want %+v", got, want)
	}
	if got := ParseTargetConfig(""); got.SSHCommand != "" || len(got.Values) != 0 {
		t.Errorf("ParseTargetConfig of nothing = %+v", got)
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
