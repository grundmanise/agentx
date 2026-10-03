package cli

import (
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
// installed from it, source add --account is refused, exit 6, and nothing
// changes. Once the skill is removed, the source becomes the account
// remote: one fork entry, no pin and no default branch, and the fork
// branches are fetched whole, although the tree fetch of the same history
// left its blobs out, so that no object their history reaches is missing;
// the source ref of the tree fetch goes.
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

	out := h.run("--json", "source", "add", s.gitDir, "--account")
	equal(t, "exit with a skill installed from it", out.exit, 6)
	equal(t, "the refusal", h.one(out.stdout, "error")["message"],
		s.url+" is a source this machine installed 1 skill from; the account remote must be a repository of your own that holds forks")
	if !reflect.DeepEqual(readSettingsFile(t, h), before) {
		t.Errorf("the refusal changed the settings: %v", readSettingsFile(t, h))
	}
	equal(t, "the source ref after the refusal", h.ref(source.Ref(id)), tree)

	h.mustRun("skill", "remove", "alpha")
	out = h.mustRun("--json", "source", "add", s.gitDir, "--account")
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

// TestTheAccountRemoteIsSetReplacedAndDetached attaches, replaces and
// detaches the account remote, a source of the fork layout. With none set,
// pull is refused, exit 6, naming source add --account. One
// git cannot reach is refused, exit 3, before anything is written, an
// account repo included. source add --account records the settings entry
// and the remote src-<id>, with one fetch refspec, the fork branches, no
// tags, no push URL and no promisor settings, then fetches its forks and
// checks what this machine may do there. A plain source add of its URL
// adds it again, still the account remote, and finds the fork published
// in between; source list marks it, and source fetch fetches its forks.
// Adding another URL with --account replaces the entry and forgets what
// the first one held, a fork's tracking following it; source remove
// detaches it: its configuration, its remote-tracking branches and its
// entry go, and the local fork stays.
func TestTheAccountRemoteIsSetReplacedAndDetached(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	out := h.run("--json", "pull")
	equal(t, "pull: exit", out.exit, 6)
	e := h.one(out.stdout, "error")
	equal(t, "pull: message", e["message"], "no account remote is set")
	contains(t, "pull: hint", e["hint"].(string), "agentx source add <url> --account")
	remote := newAccountRemote(t, h)
	equal(t, "an unreachable remote", h.run("source", "add", remote+"-missing", "--account").exit, 3)
	if _, err := os.Stat(gitx.AccountRepoPath(h.agentx)); err == nil {
		t.Error("a remote that could not be set created the account repo")
	}

	h.mustRun("skill", "new", "notes")
	url := "file://" + remote
	name := source.RemoteName(source.ID(url))
	ev := h.one(h.mustRun("--json", "source", "add", remote, "--account").stdout, "source")
	equal(t, "url", ev["url"], url)
	equal(t, "layout", ev["layout"], home.LayoutFork)
	equal(t, "account", ev["account"], true)
	equal(t, "access", ev["access"], home.AccessWritable)
	equal(t, "forks", ev["forks"], float64(0))
	equal(t, "fetch refspecs", h.accountGit("config", "--get-all", "remote."+name+".fetch"), gitx.ForkRefspec(name))
	equal(t, "tags", h.accountGit("config", "--get", "remote."+name+".tagOpt"), "--no-tags")
	equal(t, "the remote's URL", h.accountGit("config", "--get", "remote."+name+".url"), url)
	for _, key := range []string{"pushurl", "promisor", "partialclonefilter"} {
		if got, err := h.accountGitErr("config", "--get", "remote."+name+"."+key); err == nil {
			t.Errorf("the account remote has %s = %s", key, got)
		}
	}
	h.mustRun("publish", "notes")
	equal(t, "the remote-tracking branch", h.ref(lineage.RemoteForkRef(name, "notes")), h.ref(lineage.ForkRef("notes")))
	ev = h.one(h.mustRun("--json", "source", "add", remote).stdout, "source")
	equal(t, "added again: account", ev["account"], true)
	equal(t, "added again: forks", ev["forks"], float64(1))
	equal(t, "the entry's account flag", sourceEntryOf(t, h, url)["account"], true)
	contains(t, "source list", h.mustRun("source", "list").stdout, "  "+url+" (account)  fork  writable  skills/*  1 fork  ")
	contains(t, "source fetch", h.mustRun("source", "fetch", "--all").stdout, "re-fetched "+url+": 1 fork; you can write to it")

	h.accountGit("config", "branch.skills/notes.remote", name)
	other := newAccountRemote(t, h)
	otherURL := "file://" + other
	contains(t, "replaced", h.setAccount(other).stdout, "the account remote is now "+otherURL+"; it holds")
	if sources, _ := readSettingsFile(t, h)["sources"].([]any); len(sources) != 1 {
		t.Errorf("the settings hold %v, want the new account remote alone", readSettingsFile(t, h)["sources"])
	}
	equal(t, "the new entry", sourceEntryOf(t, h, otherURL)["account"], true)
	equal(t, "what the first remote held", h.ref(lineage.RemoteForkRef(name, "notes")), "")
	if got, err := h.accountGitErr("config", "--get-regexp", "^remote\\."+name+"\\."); err == nil {
		t.Errorf("the first remote's configuration is still there: %s", got)
	}
	equal(t, "the fork's tracking", h.accountGit("config", "--get", "branch.skills/notes.remote"), source.RemoteName(source.ID(otherURL)))

	contains(t, "source remove", h.mustRun("source", "remove", otherURL).stdout,
		"the account remote "+otherURL+" is no longer set; the forks of this machine are as they were")
	if out, err := h.accountGitErr("config", "--get-regexp", "^(remote\\.|branch\\.)"); err == nil {
		t.Errorf("source remove left configuration: %s", out)
	}
	equal(t, "remote-tracking branches", h.accountGit("for-each-ref", "refs/remotes/"), "")
	if sources, _ := readSettingsFile(t, h)["sources"].([]any); len(sources) != 0 {
		t.Errorf("source remove left the entry: %v", readSettingsFile(t, h)["sources"])
	}
	if h.ref(lineage.ForkRef("notes")) == "" {
		t.Error("source remove took the local fork")
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
