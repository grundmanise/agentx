package cli

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// lastChangeSeeds names the environment variable that sets how many
// random histories TestLastChangeAgreesWithGitLog checks. Each one costs
// some thirty git processes, most of them the git log it is checked
// against, so a run checks one unless it asks for more.
const lastChangeSeeds = "AGENTX_TEST_LASTCHANGE_SEEDS"

// TestLastChangeAgreesWithGitLog builds histories full of merges, taken
// back changes and equal committer times, and checks that the one walk
// gives every subpath, from several tips, the commit
// "git log -1 --no-renames <tip> -- :(literal)<subpath>" names for it.
func TestLastChangeAgreesWithGitLog(t *testing.T) {
	t.Parallel()
	seeds := uint64(1)
	if n, err := strconv.ParseUint(os.Getenv(lastChangeSeeds), 10, 64); err == nil && n > 0 {
		seeds = n
	}
	for seed := range seeds {
		t.Run(fmt.Sprint("seed ", seed), func(t *testing.T) {
			t.Parallel()
			checkLastChangeAgainstGit(t, seed)
		})
	}
}

func checkLastChangeAgainstGit(t *testing.T, seed uint64) {
	h := newHarness(t)
	s := h.newSourceRepo("random", false)
	rng := rand.New(rand.NewPCG(seed, 42))
	const skills, variants, commits = 5, 3, 60
	subpaths := make([]string, skills)
	for i := range subpaths {
		subpaths[i] = fmt.Sprintf("skills/s%d", i)
	}
	// The whole history is one fast-import stream, one git process rather
	// than four per commit. A skill is absent (-1) or one of a few
	// versions, so that a change is often taken back, or made the same way
	// on two lines.
	var stream strings.Builder
	data := func(content string) { fmt.Fprintf(&stream, "data %d\n%s\n", len(content), content) }
	type made struct {
		mark  int
		state []int
	}
	marks := 0
	build := func(state []int, when int, message string, parents []made) made {
		marks++
		fmt.Fprintf(&stream, "commit refs/heads/random\nmark :%d\ncommitter A U Thor <author@example.com> %d +0000\n", marks, 1700000000+when)
		data(message)
		for i, p := range parents {
			verb := "merge"
			if i == 0 {
				verb = "from"
			}
			fmt.Fprintf(&stream, "%s :%d\n", verb, p.mark)
		}
		stream.WriteString("deleteall\n")
		for i, v := range state {
			if v >= 0 {
				fmt.Fprintf(&stream, "M 100644 inline skills/s%d/SKILL.md\n", i)
				data(fmt.Sprintf("---\nname: s\ndescription: version %d\n---\n", v))
			}
		}
		stream.WriteString("M 100644 inline README.md\n")
		data(message)
		return made{mark: marks, state: state}
	}
	start := make([]int, skills)
	for i := range start {
		start[i] = rng.IntN(variants+1) - 1
	}
	history := []made{build(start, 0, "start", nil)}
	for n := 1; n < commits; n++ {
		var parents []made
		for len(parents) == 0 || (rng.IntN(3) == 0 && len(parents) < 3) {
			parents = append(parents, history[len(history)-1-rng.IntN(min(len(history), 8))])
		}
		state := make([]int, skills)
		for i := range state {
			state[i] = parents[rng.IntN(len(parents))].state[i]
			if rng.IntN(4) == 0 {
				state[i] = rng.IntN(variants+1) - 1
			}
		}
		// Committer times that repeat and go back as often as they go on.
		history = append(history, build(state, rng.IntN(n/2+1), fmt.Sprint("commit ", n), parents))
	}
	exported := filepath.Join(t.TempDir(), "marks")
	if _, err := s.git.IsolatedInput(context.Background(), s.gitDir, strings.NewReader(stream.String()),
		"fast-import", "--quiet", "--export-marks="+exported); err != nil {
		t.Fatalf("git fast-import: %v", err)
	}
	ids := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(readText(t, exported)), "\n") {
		mark, id, _ := strings.Cut(line, " ")
		ids[mark] = id
	}

	for _, tip := range history[len(history)-5:] {
		id := ids[fmt.Sprint(":", tip.mark)]
		out, err := s.git.Isolated(context.Background(), s.gitDir, historyRead(id, subpaths)...)
		if err != nil {
			t.Fatalf("the walk: %v", err)
		}
		walked, err := parseHistory(out, subpaths)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range subpaths {
			want := s.bare("log", "-1", "--no-renames", "--format=%H", id, "--", ":(literal)"+p)
			got, _ := walked.lastChange(id, p)
			if got != want {
				t.Errorf("from %s, %s: the walk names %q, git log -1 %q", short(id), p, got, want)
			}
		}
	}
}
