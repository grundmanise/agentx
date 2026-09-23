package cli

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
)

// TestLastChangeAgreesWithGitLog builds histories full of merges, taken
// back changes and equal committer times, and checks that the one walk
// gives every subpath, from several tips, the commit
// "git log -1 --no-renames <tip> -- :(literal)<subpath>" names for it.
func TestLastChangeAgreesWithGitLog(t *testing.T) {
	t.Parallel()
	for seed := range uint64(3) {
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
	// A skill is absent (-1) or one of a few versions, so that a change is
	// often taken back, or made the same way on two lines.
	trees := make([]string, variants)
	for v := range trees {
		blob := s.hashObject(fmt.Sprintf("---\nname: s\ndescription: version %d\n---\n", v))
		trees[v] = s.mktree("100644 blob " + blob + "\tSKILL.md")
	}
	type made struct {
		id    string
		state []int
	}
	build := func(state []int, when int, message string, parents []made) made {
		var entries []string
		for i, v := range state {
			if v >= 0 {
				entries = append(entries, fmt.Sprintf("040000 tree %s\ts%d", trees[v], i))
			}
		}
		root := []string{}
		if len(entries) > 0 {
			root = append(root, "040000 tree "+s.mktree(entries...)+"\tskills")
		}
		root = append(root, "100644 blob "+s.hashObject(message)+"\tREADME.md")
		args := []string{"commit-tree", s.mktree(root...), "-m", message}
		for _, p := range parents {
			args = append(args, "-p", p.id)
		}
		out, err := s.git.IsolatedAt(context.Background(), s.gitDir, fmt.Sprintf("%d +0000", 1700000000+when), args...)
		if err != nil {
			t.Fatalf("git commit-tree: %v", err)
		}
		return made{id: strings.TrimSpace(out), state: state}
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

	for _, tip := range history[len(history)-5:] {
		out, err := s.git.Isolated(context.Background(), s.gitDir, historyRead(tip.id, subpaths)...)
		if err != nil {
			t.Fatalf("the walk: %v", err)
		}
		walked, err := parseHistory(out, subpaths)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range subpaths {
			want := s.bare("log", "-1", "--no-renames", "--format=%H", tip.id, "--", ":(literal)"+p)
			got, _ := walked.lastChange(tip.id, p)
			if got != want {
				t.Errorf("from %s, %s: the walk names %q, git log -1 %q", short(tip.id), p, got, want)
			}
		}
	}
}

// hashObject writes content into the source as a blob and returns its id.
func (s *sourceRepo) hashObject(content string) string {
	s.t.Helper()
	out, err := s.git.IsolatedInput(context.Background(), s.gitDir, strings.NewReader(content), "hash-object", "-w", "--stdin")
	if err != nil {
		s.t.Fatalf("git hash-object: %v", err)
	}
	return strings.TrimSpace(out)
}
