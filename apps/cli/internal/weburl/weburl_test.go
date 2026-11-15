package weburl

import (
	"strings"
	"testing"
)

func TestOf(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]string{
		"https://github.com/acme/formatter":             "https://github.com/acme/formatter",
		"  https://github.com/acme/formatter/  ":        "https://github.com/acme/formatter",
		"https://github.com/acme/formatter.git":         "https://github.com/acme/formatter",
		"HTTPS://example.com/docs?page=1#install":       "https://example.com/docs?page=1#install",
		"http://example.com:8080/plugin":                "http://example.com:8080/plugin",
		"https://me:secret@gitlab.com/acme/tools.git":   "https://gitlab.com/acme/tools",
		"https://token@github.com/acme/formatter":       "https://github.com/acme/formatter",
		"git+https://github.com/acme/formatter.git":     "https://github.com/acme/formatter",
		"git+ssh://git@github.com/acme/formatter.git":   "https://github.com/acme/formatter",
		"ssh://git@gitlab.example.com:2222/acme/tools":  "https://gitlab.example.com/acme/tools",
		"git://example.org/acme/tools.git":              "https://example.org/acme/tools",
		"git@github.com:acme/formatter.git":             "https://github.com/acme/formatter",
		"github.com:acme/formatter":                     "https://github.com/acme/formatter",
		"acme/formatter":                                "https://github.com/acme/formatter",
		"acme/.github":                                  "https://github.com/acme/.github",
		"acme/formatter.git":                            "https://github.com/acme/formatter",
		"./formatter":                                   "",
		"../../formatter":                               "",
		"acme/..":                                       "",
		"https://github.com/acme/a%20b":                 "https://github.com/acme/a%20b",
		"":                                              "",
		"file:///home/me/plugins/formatter":             "",
		"/home/me/plugins/formatter":                    "",
		"./plugins/formatter":                           "",
		"ftp://example.com/formatter":                   "",
		"github:acme/formatter":                         "",
		"ssh://git@localhost/acme/tools":                "",
		"https://github.com/acme/for matter":            "",
		"https://github.com/acme/\x00formatter":         "",
		"https://github.com/acme/formatter\n":           "https://github.com/acme/formatter",
		"https:///no-host":                              "",
		"just words":                                    "",
		"https://" + strings.Repeat("a", 2048) + ".com": "",
	} {
		got := Of(raw)
		if got != want {
			t.Errorf("Of(%q) = %q, want %q", raw, got, want)
		}
		if got != "" && !Is(got) {
			t.Errorf("Is(%q) = false for what Of returned", got)
		}
	}
}

func TestIs(t *testing.T) {
	t.Parallel()
	for s, want := range map[string]bool{
		"https://github.com/acme/formatter":           true,
		"http://192.0.2.1/formatter":                  true,
		"https://example.com/docs?page=1#install":     true,
		"HTTPS://github.com/acme/formatter":           false,
		"https://me:secret@github.com/acme/formatter": false,
		"ssh://git@github.com/acme/formatter":         false,
		"https://github.com/acme/for matter":          false,
		"https://github.com/acme/formatter\t":         false,
		"https://localhost/formatter":                 false,
		"https:formatter":                             false,
		"":                                            false,
	} {
		if got := Is(s); got != want {
			t.Errorf("Is(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestTree(t *testing.T) {
	t.Parallel()
	const commit = "3f2a9c1e8d7b6a5f4e3d2c1b0a9f8e7d6c5b4a39"
	for _, tc := range []struct{ repo, path, want string }{
		{"https://github.com/acme/skills", "skills/pdf", "https://github.com/acme/skills/tree/" + commit + "/skills/pdf"},
		{"https://github.com/acme/skills", "", "https://github.com/acme/skills/tree/" + commit},
		{"https://gitlab.com/acme/skills", "pdf", "https://gitlab.com/acme/skills/-/tree/" + commit + "/pdf"},
		{"https://bitbucket.org/acme/skills", "pdf", "https://bitbucket.org/acme/skills/src/" + commit + "/pdf"},
		{"https://github.com/acme/skills", "two words/a#b", "https://github.com/acme/skills/tree/" + commit + "/two%20words/a%23b"},
		{"https://git.example.com/acme/skills", "pdf", ""},
		{"", "pdf", ""},
	} {
		if got := Tree(tc.repo, commit, tc.path); got != tc.want {
			t.Errorf("Tree(%q, %q) = %q, want %q", tc.repo, tc.path, got, tc.want)
		}
	}
	if got := Tree("https://github.com/acme/skills", "", "pdf"); got != "" {
		t.Errorf("Tree with no commit = %q, want none", got)
	}
}
