package source_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// TestAFetchWritesNoConfig: a fetch runs outside agentx's lock, so it never
// writes the account repo's config, where it could fail another command's
// git config and leave a remote behind. git's fetch with --filter writes a
// remote's promisor settings, and moves repositoryformatversion to 1, when
// the config lacks them: a remote source remove deleted while the fetch
// ran, or one a killed add had begun to write.
func TestAFetchWritesNoConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, c := range []struct {
		name    string
		remote  func(t *testing.T, git *gitx.Runner, gitDir string, src source.Source)
		fetched bool
	}{
		{"a remote removed while it fetched", func(*testing.T, *gitx.Runner, string, source.Source) {}, false},
		{"a remote with only its URL", func(t *testing.T, git *gitx.Runner, gitDir string, src source.Source) {
			if _, err := git.Isolated(ctx, gitDir, "config", "remote."+source.RemoteName(src.ID())+".url", src.URL); err != nil {
				t.Fatal(err)
			}
		}, true},
		{"a whole remote", func(t *testing.T, git *gitx.Runner, gitDir string, src source.Source) {
			if err := source.Configure(ctx, git, gitDir, home.Source{URL: src.URL, Pin: src.Ref}); err != nil {
				t.Fatal(err)
			}
		}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			up := newRepo(t, t.TempDir(), "skills")
			up.run("config", "uploadpack.allowFilter", "true")
			up.skill("skills/alpha", "alpha", "The first skill")
			up.commit()
			git, gitDir := accountRepo(t)
			src := source.Source{URL: up.url}
			c.remote(t, git, gitDir, src)
			config := filepath.Join(gitDir, "config")
			before, err := os.ReadFile(config)
			if err != nil {
				t.Fatal(err)
			}
			_, err = source.Fetch(ctx, git, gitDir, src)
			if (err == nil) != c.fetched {
				t.Errorf("fetch error = %v, want fetched %v", err, c.fetched)
			}
			after, err := os.ReadFile(config)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Errorf("the fetch wrote the account repo's config:\n--- before\n%s--- after\n%s", before, after)
			}
		})
	}
}

// TestConfigureWritesTheRemoteOfEachLayout configures one source's remote
// as a tree source with a push URL, as one without, and as a fork source,
// in turn, and reads each back as the entry wants it: a key the layout or
// the entry does not have goes, and a key a user gave a second value is set
// to one again.
func TestConfigureWritesTheRemoteOfEachLayout(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	git, gitDir := accountRepo(t)
	const url, push = "https://github.com/acme/skills", "git@github.com:acme/skills.git"
	id := source.ID(url)
	key := "remote." + source.RemoteName(id) + "."
	tree := home.Source{URL: url, Pin: "v1", PushURL: push}
	for _, tc := range []struct {
		name  string
		entry home.Source
		want  source.Remote
	}{
		{"a tree source with a push URL", tree, source.Remote{URL: url, Refspec: "+v1:" + source.StagingRef(id), PushURL: push, Promisor: "true", Filter: "blob:none"}},
		{"a tree source without one", home.Source{URL: url, Pin: "v1"}, source.Remote{URL: url, Refspec: "+v1:" + source.StagingRef(id), Promisor: "true", Filter: "blob:none"}},
		{"a fork source", home.Source{URL: url, Layout: home.LayoutFork, Account: true, PushURL: push}, source.Remote{URL: push, Refspec: gitx.ForkRefspec(source.RemoteName(id))}},
	} {
		if got := source.RemoteOf(tc.entry); got != tc.want {
			t.Errorf("%s: RemoteOf = %+v, want %+v", tc.name, got, tc.want)
		}
		if _, err := git.Isolated(ctx, gitDir, "config", "--add", key+"fetch", "+refs/heads/x:refs/x"); err != nil {
			t.Fatal(err)
		}
		if err := source.Configure(ctx, git, gitDir, tc.entry); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := source.Remotes(ctx, git, gitDir)[id]; got != tc.want {
			t.Errorf("%s: the remote reads back as %+v, want %+v", tc.name, got, tc.want)
		}
		if tag, err := git.Isolated(ctx, gitDir, "config", "--get", key+"tagOpt"); err != nil || tag != "--no-tags" {
			t.Errorf("%s: tagOpt = %q, %v", tc.name, tag, err)
		}
	}
}
