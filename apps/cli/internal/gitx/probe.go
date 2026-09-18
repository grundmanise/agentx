package gitx

import (
	"context"
	"os"
	"path/filepath"
)

// FixedCommit is the id the isolated environment gives the fixed input Probe
// commits: the same on every machine, whatever the user's git configuration.
const FixedCommit = "5d75017e77f5413f4337ef776244b8d8dc77ca90"

// Probe exercises git on a throwaway repository in the operating system's
// temporary directory: it commits a fixed tree with the isolated environment,
// then merges two branches of it with merge-tree --write-tree --merge-base.
// commit is the id of the fixed commit, to compare with FixedCommit; mergeErr
// is why the merge failed, nil when it worked; err means the throwaway
// repository could not be set up at all.
func Probe(ctx context.Context, r *Runner) (commit string, mergeErr, err error) {
	dir, err := os.MkdirTemp("", "agentx-doctor-*")
	if err != nil {
		return "", nil, err
	}
	defer os.RemoveAll(dir)
	git := func(args ...string) (string, error) {
		return r.Isolated(ctx, filepath.Join(dir, ".git"), append([]string{"--work-tree=" + dir}, args...)...)
	}
	commitAll := func(message string) (string, error) {
		if _, err := git("add", "--all"); err != nil {
			return "", err
		}
		if _, err := git("commit", "--quiet", "--message", message); err != nil {
			return "", err
		}
		return git("rev-parse", "HEAD")
	}
	write := func(name, content string) error {
		return os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)
	}

	if _, err := git("init", "--quiet"); err != nil {
		return "", nil, err
	}
	if err := write("SKILL.md", "---\nname: probe\ndescription: doctor probe\n---\n"); err != nil {
		return "", nil, err
	}
	if err := write("crlf.txt", "line one\r\nline two\r\n"); err != nil {
		return "", nil, err
	}
	base, err := commitAll("base")
	if err != nil {
		return "", nil, err
	}

	if err := write("SKILL.md", "---\nname: probe\ndescription: doctor probe, ours\n---\n"); err != nil {
		return base, nil, err
	}
	ours, err := commitAll("ours")
	if err != nil {
		return base, nil, err
	}
	if _, err := git("reset", "--quiet", "--hard", base); err != nil {
		return base, nil, err
	}
	if err := write("notes.md", "theirs\n"); err != nil {
		return base, nil, err
	}
	theirs, err := commitAll("theirs")
	if err != nil {
		return base, nil, err
	}
	_, mergeErr = git("merge-tree", "--write-tree", "--merge-base="+base, ours, theirs)
	return base, mergeErr, nil
}
