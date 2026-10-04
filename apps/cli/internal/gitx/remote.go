package gitx

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// ForkRefspec is the one fetch refspec of the remote of fork branches
// called remote: every fork branch it holds, onto the remote-tracking
// branch of the same name. Nothing else travels: import branches, update
// candidates, upstream-removed markers and source refs are this machine's
// own, and tags are never fetched.
func ForkRefspec(remote string) string {
	return "+refs/heads/skills/*:" + TrackingPrefix(remote) + "skills/*"
}

// TrackingPrefix is where a fetch of the remote called remote writes what
// it holds, every ref under it this remote's.
func TrackingPrefix(remote string) string { return "refs/remotes/" + remote + "/" }

// RemoteURL is the URL of the remote called remote as the account repo
// records it, "" when no such remote is set.
func (r *Runner) RemoteURL(ctx context.Context, gitDir, remote string) (string, error) {
	out, _, err := r.IsolatedStatus(ctx, gitDir, 1, "config", "--get", "remote."+remote+".url")
	return strings.TrimRight(out, "\n"), err
}

// SetRemote records url as the remote of fork branches called remote, with
// its ForkRefspec as its one fetch refspec and tags off, in the account
// repo's own configuration. It is written key by key with git config rather
// than git remote add, which would refuse a remote that is there already
// and write the default refspec, which fetches every branch. It runs under
// the lock, since git config fails rather than waits for its own lock file.
func (r *Runner) SetRemote(ctx context.Context, gitDir, remote, url string) error {
	section := "remote." + remote + "."
	for _, args := range [][]string{
		{"config", section + "url", url},
		{"config", "--replace-all", section + "fetch", ForkRefspec(remote)},
		{"config", section + "tagOpt", "--no-tags"},
	} {
		if _, err := r.Isolated(ctx, gitDir, args...); err != nil {
			return err
		}
	}
	return nil
}

// SetTracking records that the local branch, a short name such as
// skills/pdf, tracks the branch of the same name on the remote called
// remote, as git branch --track records it, so that git status in the
// fork's worktree says how the two stand. Writing it again is harmless. It
// is no lineage: agentx itself reads the remote-tracking branch of the same
// name, whatever the configuration says. Under the lock, as SetRemote.
func (r *Runner) SetTracking(ctx context.Context, gitDir, remote, branch string) error {
	section := "branch." + branch + "."
	for _, args := range [][]string{
		{"config", section + "remote", remote},
		{"config", section + "merge", "refs/heads/" + branch},
	} {
		if _, err := r.Isolated(ctx, gitDir, args...); err != nil {
			return err
		}
	}
	return nil
}

// UnsetTracking takes the tracking configuration of branch out of the
// account repo, the section SetTracking wrote, for a removal that deletes
// the branch: a later branch of the name would otherwise track the
// remote branch of another. A branch with none has nothing to take out.
func (r *Runner) UnsetTracking(ctx context.Context, gitDir, branch string) error {
	section := "branch." + branch
	_, status, err := r.IsolatedStatus(ctx, gitDir, 1, "config", "--get-regexp", "^"+regexp.QuoteMeta(section)+`\.`)
	if err != nil || status != 0 {
		return err
	}
	_, err = r.Isolated(ctx, gitDir, "config", "--remove-section", section)
	return err
}

// UnsetRemote takes the remote called remote out of the account repo: its
// configuration section, the tracking configuration of every fork branch
// that names it, and every remote-tracking branch a fetch of it wrote. No
// local branch is touched. Under the lock, as SetRemote. The remote-tracking
// branches go first: one stopped part way leaves the remote set, which a
// second unset takes out in turn, never branches of a remote no longer set.
func (r *Runner) UnsetRemote(ctx context.Context, gitDir, remote string) error {
	if err := r.dropRemoteRefs(ctx, gitDir, remote); err != nil {
		return err
	}
	out, _, err := r.IsolatedStatus(ctx, gitDir, 1, "config", "-z", "--get-regexp", `^(remote\.`+regexp.QuoteMeta(remote)+`|branch\.skills/.*)\.`)
	if err != nil {
		return err
	}
	sections := map[string]bool{}
	for _, record := range strings.Split(out, "\x00") {
		key, value, _ := strings.Cut(record, "\n")
		section := key[:max(strings.LastIndex(key, "."), 0)]
		switch {
		case strings.HasPrefix(key, "remote."+remote+"."):
			sections[section] = true
		case strings.HasPrefix(key, "branch.") && strings.HasSuffix(key, ".remote") && value == remote:
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

// dropRemoteRefs deletes every remote-tracking ref of the remote called
// remote, in one transaction: what a fetch of a remote no longer set, or of
// another URL, wrote says nothing about the remote set now.
func (r *Runner) dropRemoteRefs(ctx context.Context, gitDir, remote string) error {
	out, err := r.Isolated(ctx, gitDir, "for-each-ref", "--format=%(refname)", TrackingPrefix(remote))
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

// pushConfig is networkConfig for a push of the user's environment, with
// no negotiation and none of the push options the user sets for their own
// pushes, which a server that takes none refuses. The push itself passes
// --no-signed, so that a user's push.gpgSign never asks a host that signs
// nothing for a push certificate.
func pushConfig() []string {
	return append(networkConfig(), "-c", "push.negotiate=false", "-c", "push.pushOption=")
}

// ProbeRemote asks the repository at url which fork branches it holds, in
// the user's environment, so that a URL git cannot reach, or a repository
// it cannot read, is found before anything records it, and reports whether
// it holds any. It reads no repository of agentx's, and runs where no
// repository is, so that no configuration but the user's applies.
func (r *Runner) ProbeRemote(ctx context.Context, url string) (forks bool, err error) {
	args := append(networkConfig(), "ls-remote", "--heads", url, "refs/heads/skills/*")
	out, err := r.run(ctx, call{dir: os.TempDir()}, args...)
	return strings.TrimSpace(out) != "", err
}

// FetchRemote fetches the fork branches of the remote called remote into
// their remote-tracking branches, in the user's environment: quiet, no
// tags, no FETCH_HEAD, no submodules, and a remote-tracking branch whose
// fork branch the remote no longer holds is deleted. An object the account
// repo lacks is never fetched lazily from a source along the way. The
// remote's ForkRefspec is given on the command line, with --refmap=, so
// that it is the only refspec the fetch follows and prunes: the user's
// environment merges every remote.<name>.fetch their configuration holds,
// a global one meant for their projects included. refetch fetches every
// object the fork branches reach again, as a fresh clone would, telling
// the remote of nothing this repository holds. Only the first fetch of an
// account remote on this machine asks for it: a shared source of the same
// repository, removed before, may have left commits fetched without their
// blobs, which would otherwise let the remote leave out blobs the account
// repo never received. A refetch with no fork branch to fetch waits on the
// remote for good, git asking for nothing and still waiting for a pack, so
// it is asked for only when the remote was just seen holding one.
func (r *Runner) FetchRemote(ctx context.Context, gitDir, remote string, refetch bool) error {
	args := append(networkConfig(), "--git-dir="+gitDir,
		"fetch", "--quiet", "--no-tags", "--no-write-fetch-head", "--prune", "--recurse-submodules=no", "--refmap=")
	if refetch {
		args = append(args, "--refetch")
	}
	args = append(args, remote, ForkRefspec(remote))
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

// Why is what git said of the ref: its reason, or its summary when it gave
// none, as a rejection is explained to the user.
func (s PushStatus) Why() string {
	if s.Reason != "" {
		return s.Reason
	}
	return s.Summary
}

// Push pushes each fork branch named by its short name, such as
// "skills/pdf", to the branch of the same name on the remote called
// remote, never forced, in one git push of the user's environment with no
// hook, push certificate, push option or tag of theirs, see pushConfig.
// The remote's answer is the status of each ref read from --porcelain,
// git's stderr and its exit status, which ClassifyPush reads ref by ref: a
// ref the remote rejects is a status, not an error, and the push of the
// others stands, and so is a push git could not make at all, which leaves
// no status. err is a git that could not run or was stopped. git moves the
// remote-tracking branch of every ref it pushed itself.
func (r *Runner) Push(ctx context.Context, gitDir, remote string, branches []string) (statuses []PushStatus, stderr string, exit int, err error) {
	args := append(pushConfig(), "--git-dir="+gitDir, "push", "--porcelain",
		"--no-signed", "--no-verify", "--no-recurse-submodules", "--no-follow-tags", remote)
	for _, b := range branches {
		args = append(args, "refs/heads/"+b+":refs/heads/"+b)
	}
	out, exit, err := r.runStatus(ctx, call{stderr: &stderr}, 255, args...)
	if err != nil {
		return nil, stderr, exit, err
	}
	statuses, err = ParsePushPorcelain(out)
	return statuses, stderr, exit, err
}

// DeleteRemoteBranch deletes the branch of the remote called remote, named
// by its short name such as "skills/pdf", in one git push of the user's
// environment with no hook, push certificate or push option of theirs, see
// pushConfig, and returns the remote's answer for it. The deletion is
// leased on expect, the commit the last fetch read the branch at: a branch
// another machine moved since is not deleted, and git reports it rejected
// as stale. A branch the remote
// refuses to delete, as a hosting service refuses its default branch, is
// a rejected status too, not an error. git drops the remote-tracking
// branch of a branch it deleted itself.
func (r *Runner) DeleteRemoteBranch(ctx context.Context, gitDir, remote, branch, expect string) (PushStatus, error) {
	ref := "refs/heads/" + branch
	args := append(pushConfig(), "--git-dir="+gitDir, "push", "--porcelain",
		"--no-signed", "--no-verify", "--no-recurse-submodules", "--no-follow-tags",
		"--force-with-lease="+ref+":"+expect, remote, ":"+ref)
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
