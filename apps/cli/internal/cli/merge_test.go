package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
)

// TestHunksOfReadsWhatMergeFileWrites holds the hunks of one file to the
// three versions they come from: several hunks numbered in order, lines of
// the file that are longer runs of marker characters than git's own
// markers, a file with CRLF line endings and no line ending at its end, and
// sides of which only one ends the file without a newline. Every hunk gives
// each version's lines back as that version holds them.
func TestHunksOfReadsWhatMergeFileWrites(t *testing.T) {
	t.Parallel()
	requireGit(t)
	ctx := context.Background()
	gitDir := filepath.Join(t.TempDir(), "account.git")
	r := gitx.New(map[string]string{"PATH": os.Getenv("PATH"), "HOME": t.TempDir()}, false, func(string, ...any) {})
	if _, err := r.Isolated(ctx, gitDir, "init", "--bare", "--quiet", gitDir); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name               string
		base, mine, theirs string
		want               []string // mine|base|theirs of each hunk
	}{
		{
			name: "two hunks", base: "one\ntwo\nthree\nfour\nfive\n", mine: "ONE\ntwo\nthree\nfour\nFIVE\n", theirs: "uno\ntwo\nthree\nfour\ncinco\n",
			want: []string{"ONE\n|one\n|uno\n", "FIVE\n|five\n|cinco\n"},
		},
		{
			name: "lines longer than git's markers", base: "<<<<<<<<<<<<\nx\n============\n", mine: "<<<<<<<<<<<<\nmine\n============\n", theirs: "<<<<<<<<<<<<\ntheirs\n============\n",
			want: []string{"mine\n|x\n|theirs\n"},
		},
		{
			name: "CRLF with no line ending at the end", base: "a\r\nb", mine: "a\r\nb, mine", theirs: "a\r\nb, theirs",
			want: []string{"b, mine|b|b, theirs"},
		},
		{
			name: "one side with no newline at the end", base: "a\nb\n", mine: "a\nb, mine", theirs: "a\nb, theirs\n",
			want: []string{"b, mine|b\n|b, theirs\n"},
		},
		{
			name: "a hunk followed by lines both keep", base: "a\nb", mine: "A\nb", theirs: "α\nb",
			want: []string{"A\n|a\n|α\n"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			versions := map[string]staged{
				gitStageBase: {mode: "100644", oid: "base"}, gitStageMine: {mode: "100644", oid: "mine"}, gitStageTheirs: {mode: "100644", oid: "theirs"},
			}
			bodies := map[string]string{"base": c.base, "mine": c.mine, "theirs": c.theirs}
			hunks, err := hunksOf(ctx, r, gitDir, t.TempDir(), versions, bodies)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for i, h := range hunks {
				if h.Index != i+1 {
					t.Errorf("hunk %d is numbered %d", i+1, h.Index)
				}
				got = append(got, fmt.Sprintf("%s|%s|%s", h.Mine, h.Base, h.Theirs))
			}
			equal(t, "hunks", strings.Join(got, " / "), strings.Join(c.want, " / "))
		})
	}
}
