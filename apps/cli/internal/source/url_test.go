package source

import (
	"errors"
	"testing"
)

func TestParse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in       string
		url      string
		subpath  string
		ref      string
		stripped bool
	}{
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
	}
	for _, tt := range tests {
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

func TestParseRejects(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"", "   ", "owner", "owner/", "/owner/repo", "owner//repo", "owner/repo#", "owner/repo#a b", "owner/repo#a..b",
		"https://github.com/owner", "https://github.com/owner/repo/blob/main/SKILL.md",
		"https://github.com/owner/repo/pull/1", "https://github.com/owner/repo/tree",
		"https://github.com/owner/repo/tree/main/x#dev", "https://gitlab.com/group/repo/-/blob/main/x",
		"https://gitlab.com/-/tree/main", "ftp://example.com/repo", "https:///repo", "https://example.com/",
		"https://github.com/owner/../repo", "file://", "file:///", "not a url", "C:\\repo",
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
	if got := RepoName("https://github.com/owner/repo"); got != "repo" {
		t.Errorf("RepoName = %q", got)
	}
}
