package cli

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// TestAccountRemoteTakesTheSourceOfItsURL makes the account remote of a
// repository the settings hold as a tree source. While a managed skill is
// installed from it, remote set is refused, exit 6, and nothing changes.
// Once the skill is removed, the source becomes the account remote: one
// fork entry, no pin and no default branch, and the fork branches are
// fetched whole, although the tree fetch of the same history left its
// blobs out, so that no object their history reaches is missing; the
// source ref of the tree fetch goes.
func TestAccountRemoteTakesTheSourceOfItsURL(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s, _, _ := h.standardSource(true)
	s.bare("branch", "skills/alpha", "main") // a fork branch on the history main has
	h.mustRun("skill", "add", s.url, "--skill", "alpha")
	id := source.ID(s.url)
	tree := h.ref(source.Ref(id))
	before := readSettingsFile(t, h)

	out := h.run("--json", "remote", "set", s.gitDir)
	equal(t, "exit with a skill installed from it", out.exit, 6)
	equal(t, "the refusal", h.one(out.stdout, "error")["message"],
		s.url+" is a source this machine installed 1 skill from; the account remote must be a repository of your own that holds forks")
	if !reflect.DeepEqual(readSettingsFile(t, h), before) {
		t.Errorf("the refusal changed the settings: %v", readSettingsFile(t, h))
	}
	equal(t, "the source ref after the refusal", h.ref(source.Ref(id)), tree)

	h.mustRun("skill", "remove", "alpha")
	out = h.mustRun("--json", "remote", "set", s.gitDir)
	ev := h.one(out.stdout, "source")
	equal(t, "layout", ev["layout"], home.LayoutFork)
	equal(t, "forks", ev["forks"], float64(1))
	sources, _ := readSettingsFile(t, h)["sources"].([]any)
	equal(t, "entries", len(sources), 1)
	entry := sourceEntryOf(t, h, s.url)
	equal(t, "the entry's account flag", entry["account"], true)
	for _, key := range []string{"pin", "default_branch"} {
		if v, ok := entry[key]; ok {
			t.Errorf("the account remote kept the %s %v of the tree source", key, v)
		}
	}
	equal(t, "the tree fetch's source ref", h.ref(source.Ref(id)), "")
	fork := lineage.RemoteForkRef(source.RemoteName(id), "alpha")
	equal(t, "the fork branch", h.ref(fork), s.query("rev-parse", "main"))
	for _, line := range strings.Split(h.accountGit("rev-list", "--objects", "--missing=print", fork), "\n") {
		if strings.HasPrefix(line, "?") {
			t.Errorf("the fork branch's history lacks %s", line)
		}
	}
}

// legacyAccountHome is a machine as an earlier agentx left it once the
// user had set an account remote and published two forks: the remote
// origin of the account repo, with the fork refspec and no tags, its two
// remote-tracking branches, notes' tracking naming origin, and no settings
// entry. It returns the home, the remote's path and the canonical URL the
// move gives it.
func legacyAccountHome(t *testing.T) (h *harness, remote, url string) {
	t.Helper()
	h = newHarness(t)
	h.mustRun("skill", "new", "notes")
	h.mustRun("skill", "new", "other")
	remote = newAccountRemote(t, h)
	url = "file://" + remote
	h.mustRun("remote", "set", remote)
	h.mustRun("publish", "--all")
	h.accountGit("remote", "remove", source.RemoteName(source.ID(url)))
	if err := home.Mutate(h.agentx, nil, func() error {
		settings, err := home.LoadSettings(h.agentx)
		if err != nil {
			return err
		}
		settings.RemoveSource(url)
		return home.SaveSettings(h.agentx, settings)
	}); err != nil {
		t.Fatal(err)
	}
	h.accountGit("config", "remote.origin.url", remote)
	h.accountGit("config", "remote.origin.fetch", gitx.ForkRefspec(gitx.OriginRemote))
	h.accountGit("config", "remote.origin.tagOpt", "--no-tags")
	for _, name := range []string{"notes", "other"} {
		h.accountGit("update-ref", lineage.RemoteForkRef(gitx.OriginRemote, name), h.ref(lineage.ForkRef(name)))
	}
	h.accountGit("config", "branch.skills/notes.remote", gitx.OriginRemote)
	h.accountGit("config", "branch.skills/notes.merge", "refs/heads/skills/notes")
	return h, remote, url
}

// movedAccount checks that the account remote of h, at url, was moved out
// of the remote origin into the settings: the entry, the remote-tracking
// branches of both forks under the source's remote, notes' tracking naming
// it, and nothing of origin left.
func movedAccount(t *testing.T, h *harness, url string) {
	t.Helper()
	name := source.RemoteName(source.ID(url))
	entry := sourceEntryOf(t, h, url)
	equal(t, "the entry's layout", entry["layout"], home.LayoutFork)
	equal(t, "the entry's account flag", entry["account"], true)
	equal(t, "the remote's refspec", h.accountGit("config", "--get-all", "remote."+name+".fetch"), gitx.ForkRefspec(name))
	for _, fork := range []string{"notes", "other"} {
		equal(t, fork+"'s remote-tracking branch", h.ref(lineage.RemoteForkRef(name, fork)), h.ref(lineage.ForkRef(fork)))
	}
	equal(t, "notes' tracking", h.accountGit("config", "--get", "branch.skills/notes.remote"), name)
	if got, err := h.accountGitErr("config", "--get-regexp", `^remote\.origin\.`); err == nil {
		t.Errorf("the remote origin is still configured: %s", got)
	}
	equal(t, "origin's remote-tracking branches", h.accountGit("for-each-ref", gitx.TrackingPrefix(gitx.OriginRemote)), "")
}

// TestALegacyAccountRemoteMovesIntoTheSettings is a machine an earlier
// agentx left with the account remote as the remote origin. doctor says
// the next command that changes something moves it; skill list --remote,
// which changes nothing, reads and fetches it where it is, and moves
// nothing; publish moves it into the settings before it reads anything,
// then publishes through it, and doctor finds it in the settings.
func TestALegacyAccountRemoteMovesIntoTheSettings(t *testing.T) {
	t.Parallel()
	h, remote, url := legacyAccountHome(t)
	rows, _ := doctorRows(t, h.events(h.mustRun("--json", "doctor").stdout))
	equal(t, "account_remote before", rows["account_remote"]["status"], "warn")
	equal(t, "its detail", rows["account_remote"]["detail"], "the account remote "+remote+" is still the account repo's remote origin")
	contains(t, "its hint", rows["account_remote"]["hint"].(string), "any agentx command that changes something moves it into the settings")

	out := h.mustRun("--json", "skill", "list", "--remote")
	equal(t, "what skill list --remote fetched", h.one(out.stdout, "progress")["subject"], remote)
	equal(t, "origin after skill list --remote", h.accountGit("config", "--get", "remote.origin.url"), remote)
	if sources, _ := readSettingsFile(t, h)["sources"].([]any); len(sources) != 0 {
		t.Errorf("skill list --remote wrote the settings: %v", sources)
	}

	writeFile(t, h.forkDir("notes", "notes")+"/more.md", "more\n")
	h.mustRun("skill", "commit", "notes")
	out = h.mustRun("--json", "publish", "notes")
	equal(t, "the publish", h.one(out.stdout, "publish")["outcome"], publishPushed)
	equal(t, "the remote's notes", remoteGit(t, h, remote, "rev-parse", "refs/heads/skills/notes"), h.ref(lineage.ForkRef("notes")))
	movedAccount(t, h, url)
	rows, _ = doctorRows(t, h.events(h.mustRun("--json", "doctor").stdout))
	equal(t, "account_remote after", rows["account_remote"]["detail"], "the account remote is "+url)
}

// migrateChildEnv names the command TestMigrateChildProcess runs.
const migrateChildEnv = "AGENTX_TEST_MIGRATE_CHILD"

// TestMigrateChildProcess is the command a crash test kills while it moves
// the account remote: agentx with the arguments migrateChildEnv names, one
// per line, and the environment it was given.
func TestMigrateChildProcess(t *testing.T) {
	args := os.Getenv(migrateChildEnv)
	if args == "" {
		t.Skip("not the migrate child process")
	}
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	os.Exit(Run(context.Background(), strings.Split(args, "\n"), env, strings.NewReader(""), os.Stdout, os.Stderr))
}

// TestTheMoveOfALegacyAccountRemoteRecoversWhereItWasKilled kills a command
// with SIGKILL while it moves the account remote out of the remote origin,
// at its two boundaries: once the remote-tracking branches are copied,
// before the settings entry is written, and once the entry is written,
// before the tracking is rewritten and origin goes. Each step repeats
// safely, and the remote origin is the last thing to go, so the next
// command that changes something finishes the move.
func TestTheMoveOfALegacyAccountRemoteRecoversWhereItWasKilled(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, script string }{
		{"once the branches are copied", `
case " $* " in
*" update-ref --stdin"*)
	%GIT% "$@"
	status=$?
	kill -9 $PPID
	exit $status
	;;
esac
exec %GIT% "$@"
`},
		{"once the entry is written", `
case " $* " in
*"^branch"*)
	kill -9 $PPID
	exit 1
	;;
esac
exec %GIT% "$@"
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h, _, url := legacyAccountHome(t)
			out := killedChild(t, h, "TestMigrateChildProcess", migrateChildEnv, "config\nset\naccept_operations\ntrue", tc.script)
			if h.accountGit("config", "--get", "remote.origin.url") == "" {
				t.Fatalf("the killed run moved the remote origin:\n%s", out)
			}
			h.mustRun("source", "fetch", "--all")
			movedAccount(t, h, url)
		})
	}
}

// TestAccountRefusal is when a repository cannot become the account
// remote: while a tree source of its URL has skills installed from it, and
// while a tree source of another URL names it.
func TestAccountRefusal(t *testing.T) {
	t.Parallel()
	const forks = "https://github.com/me/forks"
	want := home.Source{URL: forks, Layout: home.LayoutFork, Account: true}
	alias := func(host string) string {
		if host == "github-work" {
			return "github.com"
		}
		return host
	}
	for _, tc := range []struct {
		name      string
		sources   []home.Source
		installed int
		refusal   string // a word of the refusal, "" for none
	}{
		{"nothing", nil, 0, ""},
		{"the account remote itself", []home.Source{want}, 0, ""},
		{"a tree source of the URL", []home.Source{{URL: forks}}, 0, ""},
		{"one skills are installed from", []home.Source{{URL: forks}}, 2, "installed 2 skills from"},
		{"another URL of the repository", []home.Source{{URL: "ssh://git@github-work/me/forks"}}, 0, "names the repository of the source ssh://git@github-work/me/forks"},
		{"its push URL", []home.Source{{URL: "https://example.com/me/forks", PushURL: "git@github.com:me/forks.git"}}, 0, "names the repository of the source"},
		{"another repository", []home.Source{{URL: "https://github.com/me/skills"}}, 3, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			installed := map[string]int{}
			for _, src := range tc.sources {
				installed[src.URL] = tc.installed
			}
			f := accountRefusal(home.Settings{Sources: tc.sources}, want, installed, alias)
			switch {
			case tc.refusal == "" && f != nil:
				t.Errorf("refused: %s", f.message)
			case tc.refusal != "" && f == nil:
				t.Errorf("not refused, want %q", tc.refusal)
			case f != nil:
				contains(t, "the refusal", f.message, tc.refusal)
				equal(t, "exit", f.status.exit, 6)
				contains(t, "the hint", f.hint, "agentx source remove")
			}
		})
	}
}

// TestLayoutRefusal is which layout flags source add takes.
func TestLayoutRefusal(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		layout  string
		account bool
		exit    int // 0 for flags that are taken
	}{
		{"", false, 0},
		{"tree", false, 0},
		{"", true, 0},
		{"fork", true, 0},
		{"fork", false, 6},
		{"tree", true, 1},
		{"Fork", false, 1},
		{"flat", true, 1},
	} {
		f := layoutRefusal(tc.layout, tc.account)
		got := 0
		if f != nil {
			got = f.status.exit
		}
		equal(t, "--layout "+tc.layout+" --account="+map[bool]string{true: "true", false: "false"}[tc.account], got, tc.exit)
	}
}

// TestOriginSection is when the configuration file of the account repo
// names a remote origin: a section header git reads as remote "origin",
// whatever the case of its section name.
func TestOriginSection(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		config string
		want   bool
	}{
		{"[core]\n\tbare = true\n", false},
		{"[core]\n\tbare = true\n[remote \"origin\"]\n\turl = /srv/forks.git\n", true},
		{"\t[Remote \"origin\"]\n", true},
		{"[remote \"Origin\"]\n", false},
		{"[remote \"origin2\"]\n", false},
		{"[remote \"src-0123456789abcdef\"]\n", false},
		{"# [remote \"origin\"]\n", false},
	} {
		equal(t, tc.config, originSection.MatchString(tc.config), tc.want)
	}
}

// TestMigrates is which commands move an account remote left as the
// remote origin before they run.
func TestMigrates(t *testing.T) {
	t.Parallel()
	root := newRoot(&invocation{out: &writer{}})
	for _, tc := range []struct {
		path []string
		want bool
	}{
		{[]string{"publish"}, true},
		{[]string{"source", "fetch"}, true},
		{[]string{"config", "set"}, true},
		{[]string{"skill", "list"}, false},
		{[]string{"remote", "show"}, false},
		{[]string{"serve"}, false},
		{[]string{"source"}, false},
	} {
		cmd, _, err := root.Find(tc.path)
		if err != nil {
			t.Fatal(err)
		}
		equal(t, strings.Join(tc.path, " "), migrates(cmd), tc.want)
	}
}
