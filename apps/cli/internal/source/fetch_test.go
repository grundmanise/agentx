package source_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
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
			if err := source.Configure(ctx, git, gitDir, src); err != nil {
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
