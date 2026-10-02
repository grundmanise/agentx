package gitx

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
)

// RemoteName is the account remote's name in the account repo: the one
// remote agentx gives it, which the user attaches with agentx remote set.
const RemoteName = "origin"

// ForkRefspec is the account remote's one fetch refspec: every fork branch
// it holds, onto the remote-tracking branch of the same name. Nothing else
// travels: import branches, update candidates, upstream-removed markers and
// source refs are this machine's own, and tags are never fetched.
const ForkRefspec = "+refs/heads/skills/*:refs/remotes/origin/skills/*"

// remoteTrackingPrefix is where a fetch of the account remote writes what it
// holds, every ref under it this remote's.
const remoteTrackingPrefix = "refs/remotes/" + RemoteName + "/"

// RemoteURL is the URL of the account remote as the account repo records
// it, "" when no remote is set.
func (r *Runner) RemoteURL(ctx context.Context, gitDir string) (string, error) {
	out, _, err := r.IsolatedStatus(ctx, gitDir, 1, "config", "--get", "remote."+RemoteName+".url")
	return strings.TrimRight(out, "\n"), err
}

// SetRemote records url as the account remote, with ForkRefspec as its one
// fetch refspec and tags off, in the account repo's own configuration. It
// is written key by key with git config rather than git remote add, which
// would refuse a remote that is there already and write the default
// refspec, which fetches every branch. It runs under the lock, since git
// config fails rather than waits for its own lock file.
func (r *Runner) SetRemote(ctx context.Context, gitDir, url string) error {
	section := "remote." + RemoteName + "."
	for _, args := range [][]string{
		{"config", section + "url", url},
		{"config", "--replace-all", section + "fetch", ForkRefspec},
		{"config", section + "tagOpt", "--no-tags"},
	} {
		if _, err := r.Isolated(ctx, gitDir, args...); err != nil {
			return err
		}
	}
	return nil
}

// SetTracking records that the local branch, a short name such as
// skills/pdf, tracks the branch of the same name on the account remote, as
// git branch --track records it, so that git status in the fork's worktree
// says how the two stand. Writing it again is harmless. It is no lineage:
// agentx itself reads the remote-tracking branch of the same name, whatever
// the configuration says. Under the lock, as SetRemote.
func (r *Runner) SetTracking(ctx context.Context, gitDir, branch string) error {
	section := "branch." + branch + "."
	for _, args := range [][]string{
		{"config", section + "remote", RemoteName},
		{"config", section + "merge", "refs/heads/" + branch},
	} {
		if _, err := r.Isolated(ctx, gitDir, args...); err != nil {
			return err
		}
	}
	return nil
}

// UnsetRemote takes the account remote out of the account repo: its
// configuration section, the tracking configuration of every fork branch
// that names it, and every remote-tracking branch a fetch of it wrote. No
// local branch is touched. Under the lock, as SetRemote. The remote-tracking
// branches go first: one stopped part way leaves the remote set, which a
// second unset takes out in turn, never branches of a remote no longer set.
func (r *Runner) UnsetRemote(ctx context.Context, gitDir string) error {
	if err := r.DropRemoteRefs(ctx, gitDir); err != nil {
		return err
	}
	out, _, err := r.IsolatedStatus(ctx, gitDir, 1, "config", "-z", "--get-regexp", `^(remote\.`+RemoteName+`|branch\.skills/.*)\.`)
	if err != nil {
		return err
	}
	sections := map[string]bool{}
	for _, record := range strings.Split(out, "\x00") {
		key, value, _ := strings.Cut(record, "\n")
		section := key[:max(strings.LastIndex(key, "."), 0)]
		switch {
		case strings.HasPrefix(key, "remote."+RemoteName+"."):
			sections[section] = true
		case strings.HasPrefix(key, "branch.") && strings.HasSuffix(key, ".remote") && value == RemoteName:
			sections[section] = true
		}
	}
	names := make([]string, 0, len(sections))
	for s := range sections {
		names = append(names, s)
	}
	sort.Strings(names)
	for _, s := range names {
		if _, err := r.Isolated(ctx, gitDir, "config", "--remove-section", s); err != nil {
			return err
		}
	}
	return nil
}

// DropRemoteRefs deletes every remote-tracking ref of the account remote,
// in one transaction: what a fetch of a remote no longer set, or of another
// one, wrote says nothing about the remote set now.
func (r *Runner) DropRemoteRefs(ctx context.Context, gitDir string) error {
	out, err := r.Isolated(ctx, gitDir, "for-each-ref", "--format=%(refname)", remoteTrackingPrefix)
	if err != nil || strings.TrimSpace(out) == "" {
		return err
	}
	var b strings.Builder
	b.WriteString("start\n")
	for _, ref := range strings.Split(out, "\n") {
		if ref != "" {
			b.WriteString("delete " + ref + "\n")
		}
	}
	b.WriteString("commit\n")
	_, err = r.IsolatedInput(ctx, gitDir, strings.NewReader(b.String()), "update-ref", "--stdin")
	return err
}

// networkConfig is what every git agentx runs against the account remote
// sets on top of the user's environment, whose credential helpers, SSH
// configuration and URL rewrites apply as for any git of theirs: no hook of
// theirs runs. A hooks path set in the user's global configuration belongs
// to their projects, not to the account repo, and a hook that waits for
// input would hang the serve child.
func networkConfig() []string { return []string{"-c", "core.hooksPath=" + os.DevNull} }

// ProbeRemote asks the repository at url which fork branches it holds, in
// the user's environment, so that a URL git cannot reach, or a repository
// it cannot read, is found before anything records it. It reads no
// repository of agentx's, and runs where no repository is, so that no
// configuration but the user's applies.
func (r *Runner) ProbeRemote(ctx context.Context, url string) error {
	args := append(networkConfig(), "ls-remote", "--heads", url, "refs/heads/skills/*")
	_, err := r.run(ctx, call{dir: os.TempDir()}, args...)
	return err
}

// FetchRemote fetches the account remote's fork branches into their
// remote-tracking branches, in the user's environment: quiet, no tags, no
// FETCH_HEAD, no submodules, and a remote-tracking branch whose fork branch
// the remote no longer holds is deleted. An object the account repo lacks
// is never fetched lazily from a source along the way. ForkRefspec is given
// on the command line, with --refmap=, so that it is the only refspec the
// fetch follows and prunes: the user's environment merges every
// remote.origin.fetch their configuration holds, a global one meant for
// their projects included.
func (r *Runner) FetchRemote(ctx context.Context, gitDir string) error {
	args := append(networkConfig(), "--git-dir="+gitDir,
		"fetch", "--quiet", "--no-tags", "--no-write-fetch-head", "--prune", "--recurse-submodules=no", "--refmap=", RemoteName, ForkRefspec)
	_, err := r.run(ctx, call{env: map[string]string{"GIT_NO_LAZY_FETCH": "1"}}, args...)
	return err
}

// PushStatus is what git push --porcelain reports of one ref it was asked
// to push: its flag, the two sides of the refspec, the summary and, in
// parentheses after it, the reason git gives, such as "fetch first" for a
// branch the remote moved on.
type PushStatus struct {
	Flag    byte   // ' ' pushed, '+' forced, '-' deleted, '*' new, '!' rejected, '=' up to date
	From    string // the local ref
	To      string // the remote ref
	Summary string // "1a2b3c4..5d6e7f8", "[new branch]", "[rejected]", "[up to date]"
	Reason  string // what git said in parentheses, "" when nothing
}

// Rejected reports whether the remote, or git on its behalf, refused the
// ref: the remote holds commits the push would drop, or a hook of the
// remote's said no.
func (s PushStatus) Rejected() bool { return s.Flag == '!' }

// Push pushes each fork branch named by its short name, such as
// "skills/pdf", to the account remote's branch of the same name, never
// forced, in one git push of the user's environment with no hook of theirs,
// see networkConfig, and the remote's answer for each ref read from
// --porcelain. A ref the remote rejects is a status, not an error: git
// exits 1 for it, and the push of the others stands. git moves the
// remote-tracking branch of every ref it pushed itself.
func (r *Runner) Push(ctx context.Context, gitDir string, branches []string) ([]PushStatus, error) {
	args := append(networkConfig(), "--git-dir="+gitDir, "push", "--porcelain", "--no-verify", "--no-recurse-submodules", RemoteName)
	for _, b := range branches {
		args = append(args, "refs/heads/"+b+":refs/heads/"+b)
	}
	out, _, err := r.runStatus(ctx, call{}, 1, args...)
	if err != nil {
		return nil, err
	}
	return ParsePushPorcelain(out)
}

// DeleteRemoteBranch deletes the account remote's branch, named by its
// short name such as "skills/pdf", in one git push of the user's
// environment with no hook of theirs, see networkConfig, and returns the
// remote's answer for it. The deletion is leased on expect, the commit the
// last fetch read the branch at: a branch another machine moved since is
// not deleted, and git reports it rejected as stale. A branch the remote
// refuses to delete, as a hosting service refuses its default branch, is
// a rejected status too, not an error. git drops the remote-tracking
// branch of a branch it deleted itself.
func (r *Runner) DeleteRemoteBranch(ctx context.Context, gitDir, branch, expect string) (PushStatus, error) {
	ref := "refs/heads/" + branch
	args := append(networkConfig(), "--git-dir="+gitDir, "push", "--porcelain", "--no-verify", "--no-recurse-submodules",
		"--force-with-lease="+ref+":"+expect, RemoteName, ":"+ref)
	out, _, err := r.runStatus(ctx, call{}, 1, args...)
	if err != nil {
		return PushStatus{}, err
	}
	list, err := ParsePushPorcelain(out)
	if err != nil {
		return PushStatus{}, err
	}
	for _, s := range list {
		if s.To == ref {
			return s, nil
		}
	}
	return PushStatus{}, fmt.Errorf("git push said nothing of %s", ref)
}

// ParsePushPorcelain reads what git push --porcelain prints: a "To <url>"
// line, then one line per ref, "<flag>\t<from>:<to>\t<summary>", the
// summary followed by " (<reason>)" when git gives one, and a "Done" line
// once the remote accepted the update. Pure.
func ParsePushPorcelain(out string) ([]PushStatus, error) {
	var list []PushStatus
	for _, line := range strings.Split(out, "\n") {
		if line == "" || line == "Done" || strings.HasPrefix(line, "To ") {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 || len(fields[0]) != 1 {
			return nil, fmt.Errorf("git push printed %q, which is not a ref's status", line)
		}
		from, to, ok := strings.Cut(fields[1], ":")
		if !ok {
			return nil, fmt.Errorf("git push printed %q, which names no refspec", line)
		}
		s := PushStatus{Flag: fields[0][0], From: from, To: to, Summary: fields[2]}
		if i := strings.Index(s.Summary, " ("); i >= 0 && strings.HasSuffix(s.Summary, ")") {
			s.Summary, s.Reason = s.Summary[:i], s.Summary[i+2:len(s.Summary)-1]
		}
		list = append(list, s)
	}
	return list, nil
}
