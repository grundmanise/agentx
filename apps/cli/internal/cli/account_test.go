package cli

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// TestAccountRemoteRefusesASharedSourceOfItsURL: a repository the
// settings hold as a shared source cannot become the account remote, exit
// 6, and nothing changes. Once that source is removed, source add
// --account makes it the account remote, and its fork branches are fetched
// whole, although the shared fetch of the same history left its blobs out
// of the account repo: the first fetch of an account remote on a machine
// is made with --refetch, so that no object the fork branch's history
// reaches is missing. The first is read from the account repo, not the
// settings: settings an import wrote can name as the account remote a
// repository this machine fetched as a shared source, and the run that
// writes its remote over the shared one, here source fetch, refetches too.
func TestAccountRemoteRefusesASharedSourceOfItsURL(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	s, _, _ := h.standardSource(true)
	s.bare("branch", "skills/alpha", "main") // a fork branch on the history main has
	h.mustRun("source", "add", s.url)
	imported := h.newSourceRepo("imported", true)
	imported.skill("two", "two", "Two", map[string]string{"notes.md": "two notes\n"})
	imported.commit("two")
	imported.bare("branch", "skills/two", "main")
	h.mustRun("source", "add", imported.url)
	// Another shared source stays: while any remote of the account repo is
	// a promisor, git takes the blobless commits the removed one left as
	// complete, and only --refetch brings their blobs.
	other := h.newSourceRepo("other", true)
	other.skill("one", "one", "One", nil)
	other.commit("one")
	h.mustRun("source", "add", other.url)
	whole := func(what, fork string) {
		t.Helper()
		for _, line := range strings.Split(h.accountGit("rev-list", "--objects", "--missing=print", fork), "\n") {
			if strings.HasPrefix(line, "?") {
				t.Errorf("%s: the fork branch's history lacks %s", what, line)
			}
		}
	}
	id := source.ID(s.url)
	shared := h.ref(source.Ref(id))
	before := readSettingsFile(t, h)

	out := h.run("--json", "source", "add", s.gitDir, "--account")
	equal(t, "exit over a shared source of the URL", out.exit, 6)
	e := h.one(out.stdout, "error")
	equal(t, "the refusal", e["message"], s.url+" is a shared source, and a repository cannot be both a shared source and the account remote")
	contains(t, "the hint", e["hint"].(string), "agentx source remove "+s.url)
	if !reflect.DeepEqual(readSettingsFile(t, h), before) {
		t.Errorf("the refusal changed the settings: %v", readSettingsFile(t, h))
	}
	equal(t, "the source ref after the refusal", h.ref(source.Ref(id)), shared)

	h.mustRun("source", "remove", s.url)
	ev := h.one(h.mustRun("--json", "source", "add", s.gitDir, "--account").stdout, "source")
	equal(t, "account", ev["account"], true)
	equal(t, "skills", ev["skills"], float64(1))
	fork := lineage.RemoteForkRef(source.RemoteName(id), "alpha")
	equal(t, "the fork branch", h.ref(fork), s.query("rev-parse", "main"))
	whole("source add --account", fork)

	// The settings an import writes, naming the shared source as the
	// account remote, while the account repo keeps the shared remote.
	editSettings(t, h, func(st *home.Settings) {
		st.RemoveSource(s.url)
		st.SetSource(home.Source{URL: imported.url, Account: true})
	})
	contains(t, "source fetch after the import", h.mustRun("source", "fetch", "--all").stdout, "re-fetched "+imported.url+": 1 skill")
	whole("source fetch after an import", lineage.RemoteForkRef(source.RemoteName(source.ID(imported.url)), "two"))
}

// TestTheAccountRemoteIsSetReplacedAndDetached attaches, replaces and
// detaches the account remote. With none set,
// publish is refused, exit 6, naming source add --account. One git cannot reach
// is refused, exit 3, before anything is written, an account repo included.
// source add --account records the settings entry and the remote src-<id>,
// with one fetch refspec, the fork branches, no tags and no promisor
// settings, then fetches its forks and checks what this machine may do
// there. A plain source add of its URL adds it again, still the account
// remote, and finds the fork published in between; source list marks it, and
// source fetch fetches its forks. Adding another URL with --account replaces
// the entry and forgets what the first one held, a fork's tracking following
// it, and the remote-removed markers its fetches recorded; source remove
// detaches it: its configuration, its remote-tracking branches, its
// markers and its entry go, and the local fork stays. A marker left with
// no account remote set says nothing of the skill's drift.
func TestTheAccountRemoteIsSetReplacedAndDetached(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	out := h.run("--json", "skill", "publish")
	equal(t, "publish: exit", out.exit, 6)
	e := h.one(out.stdout, "error")
	equal(t, "publish: message", e["message"], "no account remote is set")
	contains(t, "publish: hint", e["hint"].(string), "agentx source add <url> --account")
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
	equal(t, "account", ev["account"], true)
	equal(t, "access", ev["access"], home.AccessWritable)
	equal(t, "skills", ev["skills"], float64(0))
	equal(t, "fetch refspecs", h.accountGit("config", "--get-all", "remote."+name+".fetch"), gitx.ForkRefspec(name))
	equal(t, "tags", h.accountGit("config", "--get", "remote."+name+".tagOpt"), "--no-tags")
	equal(t, "the remote's URL", h.accountGit("config", "--get", "remote."+name+".url"), url)
	for _, key := range []string{"promisor", "partialclonefilter"} {
		if got, err := h.accountGitErr("config", "--get", "remote."+name+"."+key); err == nil {
			t.Errorf("the account remote has %s = %s", key, got)
		}
	}
	h.mustRun("skill", "publish", "notes")
	equal(t, "the remote-tracking branch", h.ref(lineage.RemoteForkRef(name, "notes")), h.ref(lineage.ForkRef("notes")))
	ev = h.one(h.mustRun("--json", "source", "add", remote).stdout, "source")
	equal(t, "added again: account", ev["account"], true)
	equal(t, "added again: skills", ev["skills"], float64(1))
	equal(t, "the entry's account flag", sourceEntryOf(t, h, url)["account"], true)
	contains(t, "source list", h.mustRun("source", "list").stdout, "  "+url+" (account)  writable  skills/*  1 skill  ")
	contains(t, "source fetch", h.mustRun("source", "fetch", "--all").stdout, "re-fetched "+url+": 1 skill; you can write to it")

	h.accountGit("config", "branch.skills/notes.remote", name)
	marker := lineage.RemoteRemovedRef("notes")
	h.accountGit("update-ref", marker, h.ref(lineage.ForkRef("notes")))
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
	equal(t, "the marker the first remote's fetch recorded", h.ref(marker), "")
	h.accountGit("update-ref", marker, h.ref(lineage.ForkRef("notes")))

	contains(t, "source remove", h.mustRun("source", "remove", otherURL).stdout,
		"the account remote "+otherURL+" is no longer set; your skills on this machine are as they were")
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
	equal(t, "the marker, after source remove", h.ref(marker), "")
	h.accountGit("update-ref", marker, h.ref(lineage.ForkRef("notes")))
	listed := h.mustRun("--json", "skill", "list").stdout
	excludes(t, "a marker with no account remote set", fmt.Sprint(h.librarySkill(listed, "notes")["drift"]), driftRemoteRemoved)
}

// TestAccountRefusal is when a repository cannot become the account
// remote: while a shared source of its URL, or of another URL naming it,
// is in the settings.
func TestAccountRefusal(t *testing.T) {
	t.Parallel()
	const forks = "https://github.com/me/forks"
	want := home.Source{URL: forks, Account: true}
	alias := func(host string) string {
		if host == "github-work" {
			return "github.com"
		}
		return host
	}
	for _, tc := range []struct {
		name    string
		sources []home.Source
		refusal string // a word of the refusal, "" for none
	}{
		{"nothing", nil, ""},
		{"the account remote itself", []home.Source{want}, ""},
		{"a shared source of the URL", []home.Source{{URL: forks}}, forks + " is a shared source"},
		{"another URL of the repository", []home.Source{{URL: "ssh://git@github-work/me/forks"}}, "names the repository of the shared source ssh://git@github-work/me/forks"},
		{"another repository", []home.Source{{URL: "https://github.com/me/skills"}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := accountRefusal(home.Settings{Sources: tc.sources}, want, alias)
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
