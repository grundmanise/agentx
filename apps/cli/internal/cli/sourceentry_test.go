package cli

import (
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// TestSourcesRefusal is every rule a source entry and the list of them are
// held to, against the entries agentx writes: a shared source, pinned or
// not, and the account remote.
func TestSourcesRefusal(t *testing.T) {
	t.Parallel()
	const (
		repo    = "https://github.com/acme/skills"
		forks   = "https://github.com/me/forks"
		checked = "2026-10-02T10:00:00Z"
	)
	tree := home.Source{URL: repo, Access: home.AccessWritable, AccessChecked: checked, DefaultBranch: "main", LastFetched: checked}
	account := home.Source{URL: forks, Account: true, AccessChecked: checked}
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
		{"the account remote pinned to a fork branch", with(account, func(s *home.Source) { s.Pin = "skills/pdf" }), ""},
		{"two account remotes", []home.Source{account, {URL: "https://github.com/me/more", Account: true}},
			"both " + forks + " and https://github.com/me/more are marked as the account remote"},
		{"a shared source and the account remote of one repository", []home.Source{{URL: forks, Account: true},
			{URL: "https://github.com/me/forks.git.backup"}, {URL: "ssh://git@github.com/me/forks"}},
			"ssh://git@github.com/me/forks and the account remote " + forks + " name one repository"},
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
		})
	}
}

// TestForkSourceRefusal is when a shared source an add is about to write
// names the repository of the account remote, a host alias resolved, and
// when it does not.
func TestForkSourceRefusal(t *testing.T) {
	t.Parallel()
	const forks = "https://github.com/me/forks"
	s := home.Settings{Sources: []home.Source{
		{URL: "https://github.com/acme/skills"},
		{URL: forks, Account: true},
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
		{"a shared source of the settings", home.Source{URL: "https://github.com/acme/skills"}, ""},
		{"the account remote itself", home.Source{URL: forks}, forks + " is the account remote"},
		{"its SSH URL", home.Source{URL: "ssh://git@github.com/me/forks"}, "ssh://git@github.com/me/forks names the repository of the account remote " + forks},
		{"its URL on a host alias", home.Source{URL: "ssh://git@github-work/me/forks"}, "names the repository of the account remote " + forks},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := forkSourceRefusal(s, tc.src, resolve)
			switch {
			case tc.says == "" && f != nil:
				t.Errorf("refused: %s", f.message)
			case tc.says != "" && (f == nil || f.status != exitRefused || !strings.Contains(f.message, tc.says)):
				t.Errorf("forkSourceRefusal = %v, want exit 6 saying %q", f, tc.says)
			case tc.says != "":
				contains(t, "hint", f.hint, "'agentx skill add --from-account <name>'")
			}
		})
	}
}
