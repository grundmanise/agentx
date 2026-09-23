package source

import (
	"errors"
	"testing"
)

// parseCase is one accepted input and the Source it must parse to.
type parseCase struct {
	in       string
	url      string
	subpath  string
	ref      string
	stripped bool
}

// parseCases is the table of accepted inputs, shared by TestParse and the
// round trip test, so that a case added for one is covered by the other.
func parseCases() []parseCase {
	return []parseCase{
		{"owner/repo", "https://github.com/owner/repo", "", "", false},
		{"owner/repo/skills", "https://github.com/owner/repo", "skills", "", false},
		{"owner/repo/skills/alpha/", "https://github.com/owner/repo", "skills/alpha", "", false},
		{"owner/repo#v1.2.0", "https://github.com/owner/repo", "", "v1.2.0", false},
		{"owner/repo/skills#release/1.x", "https://github.com/owner/repo", "skills", "release/1.x", false},
		{"https://github.com/owner/repo", "https://github.com/owner/repo", "", "", false},
		{"https://github.com/owner/repo.git", "https://github.com/owner/repo", "", "", false},
		{"https://github.com/owner/repo/", "https://github.com/owner/repo", "", "", false},
		{"HTTPS://GitHub.com/owner/Repo.git/", "https://github.com/owner/Repo", "", "", false},
		{"https://github.com/owner/repo/tree/main", "https://github.com/owner/repo", "", "main", false},
		{"https://github.com/owner/repo/tree/main/skills/alpha", "https://github.com/owner/repo", "skills/alpha", "main", false},
		{"https://github.com/owner/repo/tree/main/skills#main", "https://github.com/owner/repo", "skills", "main", false},
		{"https://github.com/owner/repo/skills", "https://github.com/owner/repo", "skills", "", false},
		{"https://gitlab.com/group/sub/repo", "https://gitlab.com/group/sub/repo", "", "", false},
		{"https://gitlab.com/group/sub/repo.git", "https://gitlab.com/group/sub/repo", "", "", false},
		{"https://gitlab.com/group/sub/repo/-/tree/main/skills", "https://gitlab.com/group/sub/repo", "skills", "main", false},
		{"https://git.example.com:8443/team/repo.git", "https://git.example.com:8443/team/repo", "", "", false},
		{"https://git.example.com/team/repo.git/skills/alpha", "https://git.example.com/team/repo", "skills/alpha", "", false},
		{"https://git.example.com/team/repo/-/tree/dev/skills", "https://git.example.com/team/repo", "skills", "dev", false},
		{"git@git.example.com:team/repo.git/skills", "ssh://git@git.example.com/team/repo", "skills", "", false},
		{"http://git.example.com/team/repo", "http://git.example.com/team/repo", "", "", false},
		{"git@github.com:owner/repo.git", "ssh://git@github.com/owner/repo", "", "", false},
		{"git@github.com:owner/repo", "ssh://git@github.com/owner/repo", "", "", false},
		{"git@github.com:owner/repo.git#v1", "ssh://git@github.com/owner/repo", "", "v1", false},
		{"git@gitlab.com:group/sub/repo.git", "ssh://git@gitlab.com/group/sub/repo", "", "", false},
		{"ssh://git@github.com/owner/repo.git", "ssh://git@github.com/owner/repo", "", "", false},
		{"ssh://git@github.com:2222/owner/repo.git", "ssh://git@github.com:2222/owner/repo", "", "", false},
		{"git://git.example.com/repo.git", "git://git.example.com/repo", "", "", false},
		{"file:///tmp/sources/skills.git", "file:///tmp/sources/skills.git", "", "", false},
		{"file:///tmp/sources/skills/", "file:///tmp/sources/skills", "", "", false},
		{"file://localhost/tmp/skills.git#v1", "file:///tmp/skills.git", "", "v1", false},
		{"file:///tmp/skills.git/skills/alpha", "file:///tmp/skills.git", "skills/alpha", "", false},
		{"file:///tmp/skills/-/tree/main/skills", "file:///tmp/skills", "skills", "main", false},
		{"file:///.git", "file:///.git", "", "", false},
		{"https://user:s3cret@github.com/owner/repo", "https://github.com/owner/repo", "", "", true},
		{"https://ghp_token@github.com/owner/repo.git#main", "https://github.com/owner/repo", "", "main", true},
		{"https://oauth2:tok@gitlab.com/group/repo.git", "https://gitlab.com/group/repo", "", "", true},
		{"ssh://git:pass@github.com/owner/repo", "ssh://git@github.com/owner/repo", "", "", true},
		{"file://user:pw@/tmp/skills.git", "file:///tmp/skills.git", "", "", true},
		{"  owner/repo  ", "https://github.com/owner/repo", "", "", false},

		// A percent escape in the path survives canonicalisation, so that the
		// canonical URL parses back to itself instead of to a URL with a
		// fragment (#) or a raw space in it.
		{"file:///tmp/re%23po.git", "file:///tmp/re%23po.git", "", "", false},
		{"file:///tmp/re%20po.git", "file:///tmp/re%20po.git", "", "", false},
		{"https://github.com/owner/re%23po", "https://github.com/owner/re%23po", "", "", false},
		{"https://github.com/owner/repo/sk%20ills", "https://github.com/owner/repo", "sk ills", "", false},
		{"https://git.example.com/team/re%2Fpo.git", "https://git.example.com/team/re%2Fpo", "", "", false},
		{"https://git.example.com/team/repo.git.git", "https://git.example.com/team/repo", "", "", false},
		{"https://git.\u4f8b\u3048.jp/team/repo.git", "https://git.\u4f8b\u3048.jp/team/repo", "", "", false},

		// A trailing dot on the host names the same host, and must not take
		// the URL out of the GitHub branch.
		{"https://github.com./owner/repo/skills", "https://github.com/owner/repo", "skills", "", false},
		{"https://github.com../owner/repo", "https://github.com/owner/repo", "", "", false},
		{"HTTPS://GitHub.com/owner/repo/skills", "https://github.com/owner/repo", "skills", "", false},
		{"file://localhost./tmp/skills.git", "file:///tmp/skills.git", "", "", false},

		// A default port is the same address as no port at all.
		{"https://github.com:443/owner/repo/skills", "https://github.com/owner/repo", "skills", "", false},
		{"http://git.example.com:80/team/repo.git", "http://git.example.com/team/repo", "", "", false},
		{"ssh://git@github.com:22/owner/repo.git", "ssh://git@github.com/owner/repo", "", "", false},
		{"git://git.example.com:9418/repo.git", "git://git.example.com/repo", "", "", false},

		// An IP literal host has one spelling too, and the canonical URL
		// must be one a URL parser reads back.
		{"https://[2001:DB8::0:1]/team/repo.git", "https://[2001:db8::1]/team/repo", "", "", false},
		{"ssh://git@[::1]:22/owner/repo.git", "ssh://git@[::1]/owner/repo", "", "", false},
		{"ssh://git@[::1]:2222/owner/repo.git", "ssh://git@[::1]:2222/owner/repo", "", "", false},

		// Refs git accepts stay accepted.
		{"owner/repo#0123456789abcdef0123456789abcdef01234567", "https://github.com/owner/repo", "", "0123456789abcdef0123456789abcdef01234567", false},
		{"owner/repo#h\u00e9llo", "https://github.com/owner/repo", "", "h\u00e9llo", false},
		{"owner/repo#feature/a-b", "https://github.com/owner/repo", "", "feature/a-b", false},
		{"owner/repo#v1.lockfile", "https://github.com/owner/repo", "", "v1.lockfile", false},
		{"owner/repo#a./b", "https://github.com/owner/repo", "", "a./b", false},
		{"owner/repo#x-", "https://github.com/owner/repo", "", "x-", false},
	}
}

func TestParse(t *testing.T) {
	t.Parallel()
	for _, tt := range parseCases() {
		t.Run(tt.in, func(t *testing.T) {
			got, err := Parse(tt.in)
			if err != nil {
				t.Fatalf("Parse(%q) = %v", tt.in, err)
			}
			want := Source{URL: tt.url, Subpath: tt.subpath, Ref: tt.ref, Stripped: tt.stripped}
			if got != want {
				t.Errorf("Parse(%q) = %+v, want %+v", tt.in, got, want)
			}
		})
	}
}

// TestParseRoundTrip pins the invariant the canonical URL is an identity
// under: whatever Parse accepts, re-parsing the canonical URL it produced
// yields the same canonical URL and the same id, names the repository alone
// and carries no credential. Without it a URL printed by 'source list' can
// resolve to a different, unknown source when it is fed back in.
func TestParseRoundTrip(t *testing.T) {
	t.Parallel()
	for _, tt := range parseCases() {
		t.Run(tt.in, func(t *testing.T) {
			first, err := Parse(tt.in)
			if err != nil {
				t.Fatalf("Parse(%q) = %v", tt.in, err)
			}
			again, err := Parse(first.URL)
			if err != nil {
				t.Fatalf("Parse(%q) = %v; the canonical URL of %q does not parse", first.URL, err, tt.in)
			}
			if again.URL != first.URL {
				t.Errorf("Parse(%q).URL = %q, want %q", first.URL, again.URL, first.URL)
			}
			if again.ID() != first.ID() {
				t.Errorf("id of %q is %q on the second parse, %q on the first", first.URL, again.ID(), first.ID())
			}
			if again.Subpath != "" || again.Ref != "" || again.Stripped {
				t.Errorf("Parse(%q) = %+v; a canonical URL is the repository alone", first.URL, again)
			}
		})
	}
}

// FuzzParseRoundTrip checks the same invariant over generated inputs, which
// reach spellings no table covers.
func FuzzParseRoundTrip(f *testing.F) {
	for _, tt := range parseCases() {
		f.Add(tt.in)
	}
	for _, seed := range []string{
		"", "owner/repo#", "https://h/a%2e%2e/b", "ssh://gi%40t@h/a/b", "https://[::1]:443/a/b",
		"file://localhost/a b", "https://h/a%25b", "git@h:a/b#r", "https://H./A/B?q#r",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, in string) {
		first, err := Parse(in)
		if err != nil {
			return
		}
		again, err := Parse(first.URL)
		if err != nil {
			t.Fatalf("Parse(%q) = %v; the canonical URL of %q does not parse", first.URL, err, in)
		}
		if again.URL != first.URL {
			t.Errorf("Parse(%q).URL = %q; %q does not round trip", first.URL, again.URL, in)
		}
		if again.Stripped {
			t.Errorf("the canonical URL %q of %q still carries a credential", first.URL, in)
		}
	})
}

func TestParseRejects(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"", "   ", "owner", "owner/", "/owner/repo", "owner//repo", "owner/repo#", "owner/repo#a b", "owner/repo#a..b",
		"https://github.com/owner", "https://github.com/owner/repo/blob/main/SKILL.md",
		"https://github.com/owner/repo/pull/1", "https://github.com/owner/repo/tree",
		"https://github.com/owner/repo/tree/main/x#dev", "https://gitlab.com/group/repo/-/blob/main/x",
		"https://gitlab.com/-/tree/main", "ftp://example.com/repo", "https:///repo", "https://example.com/",
		"https://github.com/owner/../repo", "file://", "file:///", "not a url", "C:\\repo",
		"ssh://::/0", "https://[:]/team/repo", "http://%25/team/repo", "https://a%20b/team/repo",
		"0/..git", "https://github.com/owner/..git", "https://git.example.com/team/..git/skills",
		"0/.git.git", "https://github.com/owner/.git.git",
		// A subpath is held to the rule of a tree entry once decoded: a
		// segment that decodes to hold a slash cannot carry . or .. past
		// the per-segment check, nor a .git component or a backslash.
		"file:///srv/hostile.git/skills%2Fevil%2F..%2F..", "https://github.com/owner/repo/skills%2F..",
		"https://github.com/owner/repo/tree/main/a%2F.%2Fb", "https://git.example.com/team/r.git/.git",
		"https://git.example.com/team/r.git/x/.GIT/y", "https://github.com/owner/repo/a%2F%2Fb",
		"https://github.com/owner/repo/a%5Cb", "https://github.com/owner/repo/a%00b",
		// A ref in a tree URL is a ref like any other.
		"https://github.com/owner/repo/tree/-x/skills",
		"https://gitlab.com/group/repo/-/tree/a.lock/skills",

		// Refs git rejects, refused as a usage error rather than deferred to
		// a fetch error with a hint about credential helpers.
		"owner/repo#-x", "owner/repo#--all", "owner/repo#x.lock", "owner/repo#a/b.lock",
		"owner/repo#.hidden", "owner/repo#a/.b", "owner/repo#a\x01b", "owner/repo#a\x7fb",
		"owner/repo#a\nb", "owner/repo#@", "owner/repo#a@{b", "owner/repo#a\\b",
		"owner/repo#a//b", "owner/repo#/a", "owner/repo#a/", "owner/repo#x.",
		"owner/repo#.", "owner/repo#a/./b",
	} {
		t.Run(in, func(t *testing.T) {
			if got, err := Parse(in); !errors.Is(err, ErrForm) {
				t.Errorf("Parse(%q) = %+v, %v; want ErrForm", in, got, err)
			}
		})
	}
}

func TestID(t *testing.T) {
	t.Parallel()
	a, b := ID("https://github.com/owner/repo"), ID("https://github.com/owner/other")
	if !IsID(a) || !IsID(b) || a == b {
		t.Errorf("ids %q and %q", a, b)
	}
	if ID("https://github.com/owner/repo") != a {
		t.Error("id is not stable")
	}
	if IsID("src-"+a) || IsID(a[:15]) || IsID("https://x") {
		t.Error("IsID accepts what is not an id")
	}
	for _, tt := range []struct{ canonical, want string }{
		{"https://github.com/owner/repo", "repo"},
		{"https://github.com/owner/repo/", "repo"},
		{"file:///tmp/sources/skills.git", "skills.git"},
		// The canonical URL escapes its path; the name does not carry the
		// escapes, since it names a skill and is hashed with its content.
		{"file:///tmp/re%23po.git", "re#po.git"},
		{"https://github.com/owner/a%20b", "a b"},
		{"https://github.com/owner/a%zzb", "a%zzb"},
	} {
		if got := RepoName(tt.canonical); got != tt.want {
			t.Errorf("RepoName(%q) = %q, want %q", tt.canonical, got, tt.want)
		}
	}
}
