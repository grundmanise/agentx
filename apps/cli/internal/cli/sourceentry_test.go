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
