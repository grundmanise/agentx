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
	fromPath bool // the ref came from the path of a tree or SKILL.md link
}

// parseCases is the table of accepted inputs, shared by TestParse and the
// round trip test, so that a case added for one is covered by the other.
func parseCases() []parseCase {
	return []parseCase{
		{"owner/repo", "https://github.com/owner/repo", "", "", false, false},
		{"owner/repo/skills", "https://github.com/owner/repo", "skills", "", false, false},
		{"owner/repo/skills/alpha/", "https://github.com/owner/repo", "skills/alpha", "", false, false},
		{"owner/repo#v1.2.0", "https://github.com/owner/repo", "", "v1.2.0", false, false},
		{"owner/repo/skills#release/1.x", "https://github.com/owner/repo", "skills", "release/1.x", false, false},
		{"https://github.com/owner/repo", "https://github.com/owner/repo", "", "", false, false},
		{"https://github.com/owner/repo.git", "https://github.com/owner/repo", "", "", false, false},
		{"https://github.com/owner/repo/", "https://github.com/owner/repo", "", "", false, false},
		{"HTTPS://GitHub.com/owner/Repo.git/", "https://github.com/owner/Repo", "", "", false, false},
		{"https://github.com/owner/repo/tree/main", "https://github.com/owner/repo", "", "main", false, true},
		{"https://github.com/owner/repo/tree/main/skills/alpha", "https://github.com/owner/repo", "skills/alpha", "main", false, true},
		{"https://github.com/owner/repo/tree/main/skills#main", "https://github.com/owner/repo", "skills", "main", false, false},
		{"https://github.com/owner/repo/skills", "https://github.com/owner/repo", "skills", "", false, false},
		// A link to a SKILL.md reads as the tree of the directory holding it.
		{"https://github.com/owner/repo/blob/main/skills/pdf/SKILL.md", "https://github.com/owner/repo", "skills/pdf", "main", false, true},
		{"https://github.com/owner/repo/blob/v2/SKILL.md", "https://github.com/owner/repo", "", "v2", false, true},
		{"https://github.com/owner/repo/blob/main/skills/pdf/SKILL.md#main", "https://github.com/owner/repo", "skills/pdf", "main", false, false},
		{"https://gitlab.com/group/sub/repo/-/blob/main/skills/pdf/SKILL.md", "https://gitlab.com/group/sub/repo", "skills/pdf", "main", false, true},
		{"https://git.example.com/team/repo/-/blob/dev/SKILL.md", "https://git.example.com/team/repo", "", "dev", false, true},
		// The line anchor a browser adds to a link to a SKILL.md is no ref;
		// on any other URL the same fragment is the pin.
		{"https://github.com/owner/repo/blob/main/skills/pdf/SKILL.md#L10", "https://github.com/owner/repo", "skills/pdf", "main", false, true},
		{"https://github.com/owner/repo/blob/main/skills/pdf/SKILL.md#L10-L20", "https://github.com/owner/repo", "skills/pdf", "main", false, true},
		{"https://github.com/owner/repo/blob/v2/SKILL.md#L3", "https://github.com/owner/repo", "", "v2", false, true},
		{"https://gitlab.com/group/repo/-/blob/main/skills/pdf/SKILL.md#L10", "https://gitlab.com/group/repo", "skills/pdf", "main", false, true},
		{"https://gitlab.com/group/repo/-/blob/main/skills/pdf/SKILL.md#L10-20", "https://gitlab.com/group/repo", "skills/pdf", "main", false, true},
		{"owner/repo#L10", "https://github.com/owner/repo", "", "L10", false, false},
		{"https://github.com/owner/repo/tree/L10/skills#L10", "https://github.com/owner/repo", "skills", "L10", false, false},
		{"https://gitlab.com/group/sub/repo", "https://gitlab.com/group/sub/repo", "", "", false, false},
		{"https://gitlab.com/group/sub/repo.git", "https://gitlab.com/group/sub/repo", "", "", false, false},
		{"https://gitlab.com/group/sub/repo/-/tree/main/skills", "https://gitlab.com/group/sub/repo", "skills", "main", false, true},
		{"https://git.example.com:8443/team/repo.git", "https://git.example.com:8443/team/repo", "", "", false, false},
		{"https://git.example.com/team/repo.git/skills/alpha", "https://git.example.com/team/repo", "skills/alpha", "", false, false},
		{"https://git.example.com/team/repo/-/tree/dev/skills", "https://git.example.com/team/repo", "skills", "dev", false, true},
		{"git@git.example.com:team/repo.git/skills", "ssh://git@git.example.com/team/repo", "skills", "", false, false},
		{"http://git.example.com/team/repo", "http://git.example.com/team/repo", "", "", false, false},
		{"git@github.com:owner/repo.git", "ssh://git@github.com/owner/repo", "", "", false, false},
		{"git@github.com:owner/repo", "ssh://git@github.com/owner/repo", "", "", false, false},
		{"git@github.com:owner/repo.git#v1", "ssh://git@github.com/owner/repo", "", "v1", false, false},
		{"git@gitlab.com:group/sub/repo.git", "ssh://git@gitlab.com/group/sub/repo", "", "", false, false},
		{"ssh://git@github.com/owner/repo.git", "ssh://git@github.com/owner/repo", "", "", false, false},
		{"ssh://git@github.com:2222/owner/repo.git", "ssh://git@github.com:2222/owner/repo", "", "", false, false},
		{"git://git.example.com/repo.git", "git://git.example.com/repo", "", "", false, false},
		{"file:///tmp/sources/skills.git", "file:///tmp/sources/skills.git", "", "", false, false},
		{"file:///tmp/sources/skills/", "file:///tmp/sources/skills", "", "", false, false},
		{"file://localhost/tmp/skills.git#v1", "file:///tmp/skills.git", "", "v1", false, false},
		{"file:///tmp/skills.git/skills/alpha", "file:///tmp/skills.git", "skills/alpha", "", false, false},
		{"file:///tmp/skills/-/tree/main/skills", "file:///tmp/skills", "skills", "main", false, true},
		{"file:///.git", "file:///.git", "", "", false, false},
		{"https://user:s3cret@github.com/owner/repo", "https://github.com/owner/repo", "", "", true, false},
		{"https://ghp_token@github.com/owner/repo.git#main", "https://github.com/owner/repo", "", "main", true, false},
		{"https://oauth2:tok@gitlab.com/group/repo.git", "https://gitlab.com/group/repo", "", "", true, false},
		{"ssh://git:pass@github.com/owner/repo", "ssh://git@github.com/owner/repo", "", "", true, false},
		{"file://user:pw@/tmp/skills.git", "file:///tmp/skills.git", "", "", true, false},
		{"  owner/repo  ", "https://github.com/owner/repo", "", "", false, false},

		// A percent escape in the path survives canonicalisation, so that the
		// canonical URL parses back to itself instead of to a URL with a
		// fragment (#) or a raw space in it.
		{"file:///tmp/re%23po.git", "file:///tmp/re%23po.git", "", "", false, false},
		{"file:///tmp/re%20po.git", "file:///tmp/re%20po.git", "", "", false, false},
		{"https://github.com/owner/re%23po", "https://github.com/owner/re%23po", "", "", false, false},
		{"https://github.com/owner/repo/sk%20ills", "https://github.com/owner/repo", "sk ills", "", false, false},
		{"https://git.example.com/team/re%2Fpo.git", "https://git.example.com/team/re%2Fpo", "", "", false, false},
		{"https://git.example.com/team/repo.git.git", "https://git.example.com/team/repo", "", "", false, false},
		{"https://git.\u4f8b\u3048.jp/team/repo.git", "https://git.\u4f8b\u3048.jp/team/repo", "", "", false, false},

		// A trailing dot on the host names the same host, and must not take
		// the URL out of the GitHub branch.
		{"https://github.com./owner/repo/skills", "https://github.com/owner/repo", "skills", "", false, false},
		{"https://github.com../owner/repo", "https://github.com/owner/repo", "", "", false, false},
		{"HTTPS://GitHub.com/owner/repo/skills", "https://github.com/owner/repo", "skills", "", false, false},
		{"file://localhost./tmp/skills.git", "file:///tmp/skills.git", "", "", false, false},

		// A default port is the same address as no port at all.
		{"https://github.com:443/owner/repo/skills", "https://github.com/owner/repo", "skills", "", false, false},
		{"http://git.example.com:80/team/repo.git", "http://git.example.com/team/repo", "", "", false, false},
		{"ssh://git@github.com:22/owner/repo.git", "ssh://git@github.com/owner/repo", "", "", false, false},
		{"git://git.example.com:9418/repo.git", "git://git.example.com/repo", "", "", false, false},

		// An IP literal host has one spelling too, and the canonical URL
		// must be one a URL parser reads back.
		{"https://[2001:DB8::0:1]/team/repo.git", "https://[2001:db8::1]/team/repo", "", "", false, false},
		{"ssh://git@[::1]:22/owner/repo.git", "ssh://git@[::1]/owner/repo", "", "", false, false},
		{"ssh://git@[::1]:2222/owner/repo.git", "ssh://git@[::1]:2222/owner/repo", "", "", false, false},

		// Refs git accepts stay accepted.
		{"owner/repo#0123456789abcdef0123456789abcdef01234567", "https://github.com/owner/repo", "", "0123456789abcdef0123456789abcdef01234567", false, false},
		{"owner/repo#h\u00e9llo", "https://github.com/owner/repo", "", "h\u00e9llo", false, false},
		{"owner/repo#feature/a-b", "https://github.com/owner/repo", "", "feature/a-b", false, false},
		{"owner/repo#v1.lockfile", "https://github.com/owner/repo", "", "v1.lockfile", false, false},
		{"owner/repo#a./b", "https://github.com/owner/repo", "", "a./b", false, false},
		{"owner/repo#x-", "https://github.com/owner/repo", "", "x-", false, false},

		// HEAD is the remote's default branch, which an unpinned source
		// follows anyway: it is no pin.
		{"owner/repo#HEAD", "https://github.com/owner/repo", "", "", false, false},
		{"https://github.com/owner/repo/tree/HEAD/skills", "https://github.com/owner/repo", "skills", "", false, false},

		// Every other form git reads as the URL of a remote: a path on disk,
		// the SSH shorthand without a user, and the older spellings of an
		// SSH URL.
		{"/srv/skills.git", "file:///srv/skills.git", "", "", false, false},
		{"/owner/repo", "file:///owner/repo", "", "", false, false},
		{"/srv/skills.git/tools#v1", "file:///srv/skills.git", "tools", "v1", false, false},
		{"/srv/a%b?c/skills", "file:///srv/a%25b%3Fc/skills", "", "", false, false},
		{"git.example.com:team/repo.git", "ssh://git.example.com/team/repo", "", "", false, false},
		{"git.example.com:/srv/repo.git#main", "ssh://git.example.com/srv/repo", "", "main", false, false},
		{"git.example.com:skills@2.git", "ssh://git.example.com/skills@2", "", "", false, false},
		{"git+ssh://git@github.com/owner/repo.git", "ssh://git@github.com/owner/repo", "", "", false, false},
		{"SSH+GIT://github.com:22/owner/repo", "ssh://github.com/owner/repo", "", "", false, false},
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
			want := Source{URL: tt.url, Subpath: tt.subpath, Ref: tt.ref, Stripped: tt.stripped, RefFromPath: tt.fromPath}
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
			if again.Subpath != "" || again.Ref != "" || again.Stripped || again.RefFromPath {
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
		"", "   ", "owner", "owner/", "owner//repo", "owner/repo#", "owner/repo#a b", "owner/repo#a..b",
		"https://github.com/owner", "https://github.com/owner/repo/blob/main/README.md",
		"https://github.com/owner/repo/blob/main", "https://github.com/owner/repo/blob/main/skills/pdf/skill.md",
		"https://github.com/owner/repo/blob/main/x/SKILL.md#dev", "https://gitlab.com/group/repo/-/blob/main/x/README.md",
		"https://github.com/owner/repo/tree/main/skills#L10", "https://github.com/owner/repo/blob/main/x/SKILL.md#L10x",
		"https://github.com/owner/repo/blob/main/x/README.md#L10",
		"https://github.com/owner/repo/pull/1", "https://github.com/owner/repo/tree",
		"https://github.com/owner/repo/tree/main/x#dev", "https://gitlab.com/group/repo/-/blob/main/x",
		"https://gitlab.com/-/tree/main", "ftp://example.com/repo", "https:///repo", "https://example.com/",
		"https://github.com/owner/../repo", "file://", "file:///", "not a url", "C:\\repo", "c:\\skills\\repo", "/", "/srv/../repo", "host:",
		"git+ssh://", "git+https://github.com/owner/repo", "user:tok@example.com/owner/repo", "user:tok@example.com:8443/owner/repo",
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
