package cli

import (
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// TestSourcesRefusal is every rule a source entry and the list of them are
// held to, against the entries agentx writes: a tree source, pinned or not,
// with a push URL over another transport, and the account remote.
func TestSourcesRefusal(t *testing.T) {
	t.Parallel()
	const (
		repo    = "https://github.com/acme/skills"
		forks   = "https://github.com/me/forks"
		checked = "2026-10-02T10:00:00Z"
	)
	tree := home.Source{URL: repo, PushURL: "git@github.com:acme/skills.git", Access: home.AccessWritable,
		AccessChecked: checked, DefaultBranch: "main", LastFetched: checked}
	account := home.Source{URL: forks, Layout: home.LayoutFork, Account: true, PushURL: "git@github-work:me/forks.git",
		AccessChecked: checked}
	with := func(base home.Source, change func(*home.Source)) []home.Source {
		change(&base)
		return []home.Source{base}
	}
	for _, tc := range []struct {
		name    string
		sources []home.Source
		says    string // "" for a list agentx writes
	}{
		{"what agentx writes", []home.Source{tree, account}, ""},
		{"a fork source pinned to a fork branch", with(account, func(s *home.Source) { s.Pin = "skills/pdf" }), ""},
		{"a push URL on an alias of the host", with(tree, func(s *home.Source) { s.PushURL = "git@github-work:acme/skills.git" }), ""},
		{"a push URL on a path on disk", []home.Source{{URL: "file:///srv/skills.git", PushURL: "ssh://host/srv/skills.git"}}, ""},
		{"the layout spelled out", with(tree, func(s *home.Source) { s.Layout = "tree" }), "agentx writes fork, or no layout"},
		{"a layout agentx does not know", with(tree, func(s *home.Source) { s.Layout = "mirror" }), "the layout mirror"},
		{"a tree source as the account remote", with(tree, func(s *home.Source) { s.Account = true }), "only a source of the fork layout"},
		{"a fork source of its own", with(account, func(s *home.Source) { s.Account = false }), "fork source other than the account remote"},
		{"two account remotes", []home.Source{account, {URL: "https://github.com/me/more", Layout: home.LayoutFork, Account: true}},
			"both " + forks + " and https://github.com/me/more are marked as the account remote"},
		{"a tree source and a fork source of one repository", []home.Source{{URL: forks, Layout: home.LayoutFork, Account: true},
			{URL: "https://github.com/me/forks.git.backup"}, {URL: "ssh://git@github.com/me/forks"}},
			"ssh://git@github.com/me/forks and the fork source " + forks + " name one repository"},
		{"a tree source pushed to the account remote", []home.Source{{URL: "https://example.com/me/forks", PushURL: "git@github.com:me/forks"}, account},
			"https://example.com/me/forks and the fork source " + forks + " name one repository"},
		{"a push URL with a token", with(tree, func(s *home.Source) { s.PushURL = "https://x:" + importToken + "@github.com/acme/skills" }),
			"the push_url of " + repo + " carries a password or a token"},
		{"a push URL with a user over HTTPS", with(tree, func(s *home.Source) { s.PushURL = "https://" + importToken + "@github.com/acme/skills" }),
			"carries a password or a token"},
		{"a push URL with a token in its query", with(tree, func(s *home.Source) { s.PushURL = "http://github.com/acme/skills?access_token=" + importToken }),
			"the push_url of " + repo + " is not a URL git can push to: holds a query"},
		{"a push URL naming a ref", with(tree, func(s *home.Source) { s.PushURL = "git@github.com:acme/skills#main" }), "names a ref"},
		{"a push URL over a helper", with(tree, func(s *home.Source) { s.PushURL = "hg::https://github.com/acme/skills" }), "not a URL git can push to"},
		{"a push URL over git://", with(tree, func(s *home.Source) { s.PushURL = "git://github.com/acme/skills" }), "over which nothing can be pushed"},
		{"a push URL of another repository", with(tree, func(s *home.Source) { s.PushURL = "git@github.com:acme/other.git" }), "names another repository"},
		{"a push URL that only respells the URL", with(tree, func(s *home.Source) { s.PushURL = "https://GitHub.com/acme/skills.git" }), "only respells it"},
		{"an access agentx does not write", with(tree, func(s *home.Source) { s.Access = home.AccessUnknown }), "the access unknown"},
		{"an access check at no time", with(tree, func(s *home.Source) { s.AccessChecked = "yesterday" }), "access checked at yesterday"},
		{"a default branch git refuses", with(tree, func(s *home.Source) { s.DefaultBranch = "-main" }), "default branch -main, which is not a ref"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := sourcesRefusal(tc.sources, nil)
			switch {
			case tc.says == "" && got != "":
				t.Errorf("refused what agentx writes: %s", got)
			case tc.says != "" && !strings.Contains(got, tc.says):
				t.Errorf("sourcesRefusal = %q, want it to say %q", got, tc.says)
			}
			if strings.Contains(got, importToken) {
				t.Errorf("the refusal repeats the credential: %s", got)
			}
		})
	}
}

// TestPushURLFor is what source add --push-url records for a source, and
// what it refuses, against a resolver that knows one SSH alias: a respelling
// records nothing, another transport, user or host alias records the URL as
// given, the same path on a host that stays another records it with a
// warning, and a URL git cannot push to, one with a credential and one of
// another repository are refused before anything is written.
func TestPushURLFor(t *testing.T) {
	t.Parallel()
	const repo = "https://github.com/acme/skills"
	resolve := func(host string) string {
		if host == "github-work" {
			return "github.com"
		}
		return host
	}
	for _, tc := range []struct {
		raw, canonical string
		record         string // what is recorded, "" for nothing
		warns          string // a part of the warning, "" for none
		refuses        string // a part of the refusal, "" for none
	}{
		{"", repo, "", "", ""},
		{"git@github.com:acme/skills.git", repo, "git@github.com:acme/skills.git", "", ""},
		{"ssh://git@github.com:2222/acme/skills", repo, "ssh://git@github.com:2222/acme/skills", "", ""},
		{"git@github-work:acme/skills.git", repo, "git@github-work:acme/skills.git", "", ""},
		{"git@ssh.github.com:acme/skills", repo, "git@ssh.github.com:acme/skills", "", ""},
		{"git@git.example.com:acme/skills.git", repo, "git@git.example.com:acme/skills.git", "name different hosts; agentx pushes to git@git.example.com:acme/skills.git", ""},
		{"ssh://build@host/srv/skills.git", "file:///srv/skills.git", "ssh://build@host/srv/skills.git", "name different hosts", ""},
		// Spelled differently and nothing more: pushes to the URL itself go there.
		{"https://github.com/acme/skills.git", repo, "", "", ""},
		{"HTTPS://GitHub.com./acme/skills/", repo, "", "", ""},
		{"git@github.com:acme/skills", "ssh://git@github.com/acme/skills", "", "", ""},
		{"/srv/skills.git", "file:///srv/skills.git", "", "", ""},
		// Refused.
		{"https://me:" + importToken + "@github.com/acme/skills", repo, "", "", "carries a password or a token"},
		{"https://" + importToken + "@github.com/acme/skills", repo, "", "", "carries a password or a token"},
		{"git@github.com:acme/skills#main", repo, "", "", "not a URL git can push to: names a ref"},
		{"hg::https://github.com/acme/skills", repo, "", "", "not a URL git can push to"},
		{"skills", repo, "", "", "not a URL git can push to"},
		{"git://github.com/acme/skills", repo, "", "", "over which nothing can be pushed"},
		{"git@github.com:acme/other.git", repo, "", "", "the push URL git@github.com:acme/other.git names another repository than " + repo},
		{"git@github.com:Acme/Skills.git", repo, "", "", "names another repository"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			record, warning, f := pushURLFor(tc.raw, tc.canonical, resolve)
			switch {
			case tc.refuses != "":
				if f == nil || f.status != exitUsage || !strings.Contains(f.message, tc.refuses) {
					t.Fatalf("pushURLFor = %q, %q, %v; want a usage refusal saying %q", record, warning, f, tc.refuses)
				}
				if strings.Contains(f.message+f.hint, importToken) {
					t.Errorf("the refusal repeats the credential: %s %s", f.message, f.hint)
				}
				return
			case f != nil:
				t.Fatalf("pushURLFor refused: %s", f.message)
			}
			equal(t, "record", record, tc.record)
			if tc.warns == "" && warning != "" || !strings.Contains(warning, tc.warns) {
				t.Errorf("warning = %q, want %q", warning, tc.warns)
			}
		})
	}
}

// TestForkSourceRefusal is when a tree source an add is about to write
// names the repository of a fork source, by its URL or its push URL, a host
// alias resolved, and when it does not.
func TestForkSourceRefusal(t *testing.T) {
	t.Parallel()
	const forks = "https://github.com/me/forks"
	s := home.Settings{Sources: []home.Source{
		{URL: "https://github.com/acme/skills"},
		{URL: forks, Layout: home.LayoutFork, Account: true, PushURL: "git@github-work:me/forks.git"},
	}}
	resolve := func(host string) string {
		if host == "github-work" {
			return "github.com"
		}
		return host
	}
	for _, tc := range []struct {
		name string
		src  home.Source
		says string // "" for no refusal
	}{
		{"another repository", home.Source{URL: "https://github.com/me/other"}, ""},
		{"a tree source of the settings", home.Source{URL: "https://github.com/acme/skills", PushURL: "git@github.com:acme/skills.git"}, ""},
		{"the fork source itself", home.Source{URL: forks}, forks + " is a fork source"},
		{"its SSH URL", home.Source{URL: "ssh://git@github.com/me/forks"}, "ssh://git@github.com/me/forks is the fork source " + forks},
		{"its push URL's alias", home.Source{URL: "ssh://git@github-work/me/forks"}, "is the fork source " + forks},
		{"a push URL naming it", home.Source{URL: "https://example.com/me/forks", PushURL: "git@github.com:me/forks.git"}, "is the fork source " + forks},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := forkSourceRefusal(s, tc.src, resolve)
			switch {
			case tc.says == "" && f != nil:
				t.Errorf("refused: %s", f.message)
			case tc.says != "" && (f == nil || f.status != exitRefused || !strings.Contains(f.message, tc.says)):
				t.Errorf("forkSourceRefusal = %v, want exit 6 saying %q", f, tc.says)
			case tc.says != "":
				contains(t, "hint", f.hint, "'agentx remote set git@github-work:me/forks.git'")
			}
		})
	}
}
