package source

import (
	"errors"
	"strings"
	"testing"
)

func TestParseAddress(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in   string
		want Address
	}{
		{"https://github.com/acme/skills", Address{Scheme: "https", Host: "github.com", Path: "acme/skills"}},
		{"https://GitHub.com.:443/acme/skills.git/", Address{Scheme: "https", Host: "github.com", Path: "acme/skills"}},
		{"http://git.example.com:8080/team/repo", Address{Scheme: "http", Host: "git.example.com", Port: "8080", Path: "team/repo"}},
		{"git@github.com:acme/skills.git", Address{Scheme: "ssh", User: "git", Host: "github.com", Path: "acme/skills"}},
		{"github-work:acme/skills.git", Address{Scheme: "ssh", Host: "github-work", Path: "acme/skills"}},
		{"git@host:/srv/skills", Address{Scheme: "ssh", User: "git", Host: "host", Path: "srv/skills"}},
		{"ssh://git@github.com:22/acme/skills", Address{Scheme: "ssh", User: "git", Host: "github.com", Path: "acme/skills"}},
		{"ssh://git@ssh.github.com:443/acme/skills", Address{Scheme: "ssh", User: "git", Host: "ssh.github.com", Port: "443", Path: "acme/skills"}},
		{"git+ssh://git@host/a", Address{Scheme: "ssh", User: "git", Host: "host", Path: "a"}},
		{"ssh+git://host/a", Address{Scheme: "ssh", Host: "host", Path: "a"}},
		{"git://host/a.git", Address{Scheme: "git", Host: "host", Path: "a"}},
		{"file:///srv/skills.git", Address{Scheme: "file", Path: "srv/skills"}},
		{"file://localhost/srv/skills", Address{Scheme: "file", Path: "srv/skills"}},
		{"/srv/skills.git", Address{Scheme: "file", Path: "srv/skills"}},
		{"https://example.com/Team/Re%20po", Address{Scheme: "https", Host: "example.com", Path: "Team/Re po"}},
		{"/srv/acme skills/repo", Address{Scheme: "file", Path: "srv/acme skills/repo"}},
		{"git@host:a?x=1", Address{Scheme: "ssh", User: "git", Host: "host", Path: "a?x=1"}},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseAddress(tc.in)
			if err != nil || got != tc.want {
				t.Errorf("ParseAddress(%q) = %+v, %v, want %+v", tc.in, got, err, tc.want)
			}
		})
	}
	for _, tc := range []struct{ in, says string }{
		{"", "empty"},
		{" https://host/a", "space"},
		{"-uhttps://host/a", "dash"},
		{"https://host/a#main", "ref"},
		{"hg::https://host/a", "unsupported scheme"},
		{"s3://bucket/a", "unsupported scheme"},
		{"ext::ssh -o x host %S", "unsupported scheme"},
		{"https://host/a?access_token=ghp_secret", "holds a query"},
		{"https://host/a?", "holds a query"},
		{"ssh://git@host/a?x=1", "holds a query"},
		{"https:///a", "no host"},
		{"https://host/", "no repository path"},
		{"https://host/a/../b", `".."`},
		{"skills", "neither"},
		{"https://user:ghp_secret@host/a", "password or a token"},
		{"https://ghp_secret@host/a", "password or a token"},
		{"ssh://git:ghp_secret@host/a", "password or a token"},
		{"git:ghp_secret@host:a", "password or a token"},
	} {
		t.Run("refuses "+tc.in, func(t *testing.T) {
			_, err := ParseAddress(tc.in)
			if err == nil || !strings.Contains(err.Error(), tc.says) {
				t.Fatalf("ParseAddress(%q) = %v, want an error saying %q", tc.in, err, tc.says)
			}
			if strings.Contains(err.Error(), "ghp_secret") {
				t.Errorf("the error repeats the credential: %v", err)
			}
			if !errors.Is(err, ErrForm) && !errors.Is(err, ErrCredential) {
				t.Errorf("ParseAddress(%q) = %v, neither an ErrForm nor an ErrCredential", tc.in, err)
			}
		})
	}
}

// TestParseAddressReadsEveryCanonicalURL: the canonical URL of every source
// is an address of its repository, so that its push URL can be compared
// with it.
func TestParseAddressReadsEveryCanonicalURL(t *testing.T) {
	t.Parallel()
	for _, tt := range parseCases() {
		a, err := ParseAddress(tt.url)
		if err != nil {
			t.Errorf("ParseAddress(%q) = %v", tt.url, err)
			continue
		}
		if Compare(a, a, nil) != SameRepository {
			t.Errorf("%q does not name the repository it names", tt.url)
		}
	}
}

func TestCompare(t *testing.T) {
	t.Parallel()
	aliases := map[string]string{"github-work": "github.com", "work": "GitHub.com."}
	resolve := func(host string) string {
		if to, ok := aliases[host]; ok {
			return to
		}
		return host
	}
	for _, tc := range []struct {
		a, b      string
		want      Kinship
		transport bool
	}{
		{"https://github.com/acme/skills", "https://github.com/acme/skills.git/", SameRepository, true},
		{"https://github.com/acme/skills", "git@github.com:acme/skills.git", SameRepository, false},
		{"ssh://git@github.com/acme/skills", "git@github.com:acme/skills", SameRepository, true},
		{"https://github.com/acme/skills", "ssh://git@github.com:2222/acme/skills", SameRepository, false},
		{"https://github.com/acme/skills", "git@github-work:acme/skills.git", SameRepository, false},
		{"https://github.com/acme/skills", "work:acme/skills", SameRepository, false},
		{"https://github.com/acme/skills", "ssh://git@ssh.github.com:443/acme/skills", SameRepository, false},
		{"https://gitlab.com/g/r", "git@altssh.gitlab.com:g/r", SameRepository, false},
		{"file:///srv/skills.git", "/srv/skills", SameRepository, true},
		{"https://github.com/acme/skills", "git@github.com:acme/other", OtherRepository, false},
		{"https://github.com/acme/skills", "https://github.com/Acme/Skills", OtherRepository, true},
		{"https://github.com/acme/skills", "git@elsewhere.example:acme/skills", OtherHost, false},
		// An alias reads through only over SSH, where an SSH configuration
		// is what names hosts.
		{"https://github.com/acme/skills", "https://github-work/acme/skills", OtherHost, false},
	} {
		t.Run(tc.a+" "+tc.b, func(t *testing.T) {
			a, err := ParseAddress(tc.a)
			if err != nil {
				t.Fatal(err)
			}
			b, err := ParseAddress(tc.b)
			if err != nil {
				t.Fatal(err)
			}
			if got := Compare(a, b, resolve); got != tc.want {
				t.Errorf("Compare = %d, want %d", got, tc.want)
			}
			if got := Compare(b, a, resolve); got != tc.want {
				t.Errorf("Compare the other way = %d, want %d", got, tc.want)
			}
			if got := SameTransport(a, b); got != tc.transport {
				t.Errorf("SameTransport = %v, want %v", got, tc.transport)
			}
		})
	}
}
