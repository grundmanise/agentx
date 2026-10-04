package gitx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// The primitives of a publish to a branch: the tree of a commit that takes
// a skill's edits onto what the branch holds, one leased push of one
// commit to one branch, and what the remote's answer to a push means.

// ApplyChanges is the root tree of root, a tree or a commit of the branch
// a skill is published to, with what changed in the skill between the
// trees base and written, its directory as installed and as it is now,
// applied under prefix, the skill's directory from the branch's root, ""
// for the root itself. One diff-tree reads the change set, see IndexInfo,
// and a throwaway index loaded with root takes it, so that every other
// entry of root, a file agentx would not install included, stays as the
// branch holds it. The tree is written with --missing-ok, since a
// blobless fetch of a source leaves most of root's blobs on the remote,
// and nothing is fetched lazily.
func (r *Runner) ApplyChanges(ctx context.Context, gitDir, root, prefix, base, written string) (string, error) {
	noLazy := map[string]string{"GIT_NO_LAZY_FETCH": "1"}
	diff, err := r.run(ctx, call{isolated: true, env: noLazy}, isolatedArgs(gitDir, []string{"diff-tree", "-r", "-z", "--no-renames", base, written})...)
	if err != nil {
		return "", err
	}
	info, err := IndexInfo(diff, prefix)
	if err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp("", "agentx-index-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	indexed := map[string]string{"GIT_NO_LAZY_FETCH": "1", "GIT_INDEX_FILE": filepath.Join(tmp, "index")}
	if _, err := r.run(ctx, call{isolated: true, env: indexed}, isolatedArgs(gitDir, []string{"read-tree", root})...); err != nil {
		return "", err
	}
	if info != "" {
		if _, err := r.run(ctx, call{isolated: true, env: indexed, stdin: strings.NewReader(info)}, isolatedArgs(gitDir, []string{"update-index", "-z", "--index-info"})...); err != nil {
			return "", err
		}
	}
	tree, err := r.run(ctx, call{isolated: true, env: indexed}, isolatedArgs(gitDir, []string{"write-tree", "--missing-ok"})...)
	return strings.TrimSpace(tree), err
}

// ErrUnpublishable is a change set ApplyChanges cannot take onto a branch:
// a symlink or a submodule link in it, which a skill agentx installs
// never holds as its own edit.
var ErrUnpublishable = errors.New("cannot be published")

// IndexInfo translates the change set diff-tree -r -z prints between two
// trees of a skill directory into the records update-index -z
// --index-info reads, each path under prefix: a deleted path is mode 0,
// and an added or changed one takes the mode and blob the newer tree
// holds, which is the mode the older one had when the two agree on it. A
// symlink or a submodule link on either side is ErrUnpublishable naming
// the path. Pure.
func IndexInfo(diffTreeZ, prefix string) (string, error) {
	fields := strings.Split(diffTreeZ, "\x00")
	var b strings.Builder
	for i := 0; i < len(fields); i++ {
		meta := fields[i]
		if meta == "" {
			continue
		}
		if i+1 >= len(fields) {
			return "", fmt.Errorf("diff-tree printed %q with no path", meta)
		}
		i++
		name := fields[i]
		parts := strings.Fields(strings.TrimPrefix(meta, ":"))
		if !strings.HasPrefix(meta, ":") || len(parts) != 5 {
			return "", fmt.Errorf("diff-tree printed %q, which is not a change", meta)
		}
		srcMode, dstMode, dstOID, status := parts[0], parts[1], parts[3], parts[4]
		full := name
		if prefix != "" {
			full = path.Join(prefix, name)
		}
		for _, mode := range []string{srcMode, dstMode} {
			switch mode {
			case "120000":
				return "", fmt.Errorf("%w: %s is a symlink", ErrUnpublishable, full)
			case "160000":
				return "", fmt.Errorf("%w: %s is a submodule link", ErrUnpublishable, full)
			}
		}
		switch status[0] {
		case 'D':
			b.WriteString("0 " + strings.Repeat("0", len(parts[2])) + "\t" + full + "\x00")
		case 'A', 'M', 'T':
			b.WriteString(dstMode + " " + dstOID + "\t" + full + "\x00")
		default:
			return "", fmt.Errorf("diff-tree printed %q for %s, which is not an addition, a change or a deletion", status, full)
		}
	}
	return b.String(), nil
}

// PushBranch pushes commit to the branch of the remote called remote,
// short name branch, leased on lease, the commit the branch was last read
// at ("" for a branch that must not exist yet): a branch that moved since
// is not pushed over, and nothing else is pushed, no tag, no submodule and
// no other ref. It runs in the user's environment, so their credentials,
// SSH configuration and URL rewrites apply, with no hook of theirs, no
// push certificate or push option, no negotiation, no thin pack and no lazy fetch of an
// object the account repo lacks. The remote's answer is the porcelain
// status of the branch's ref, found false when git printed none, git's
// stderr and its exit status, for ClassifyPush to read; err is a git that
// could not run or was stopped.
func (r *Runner) PushBranch(ctx context.Context, gitDir, remote, commit, branch, lease string) (s PushStatus, found bool, stderr string, exit int, err error) {
	ref := "refs/heads/" + branch
	args := append(pushConfig(), "--git-dir="+gitDir, "push", "--porcelain",
		"--no-signed", "--no-verify", "--no-recurse-submodules", "--no-follow-tags", "--no-thin",
		"--force-with-lease="+ref+":"+lease, remote, commit+":"+ref)
	out, exit, err := r.runStatus(ctx, call{stderr: &stderr, env: map[string]string{"GIT_NO_LAZY_FETCH": "1"}}, 255, args...)
	if err != nil {
		return PushStatus{}, false, stderr, exit, err
	}
	list, err := ParsePushPorcelain(out)
	if err != nil {
		return PushStatus{}, false, stderr, exit, err
	}
	for _, st := range list {
		if st.To == ref {
			return st, true, stderr, exit, nil
		}
	}
	return PushStatus{}, false, stderr, exit, nil
}

// PushClass is what the remote's answer to the push of one ref means.
type PushClass string

// The classes of a push's answer, see ClassifyPush.
const (
	PushPushed      PushClass = "pushed"      // the remote's branch holds the commit now
	PushUpToDate    PushClass = "up to date"  // it held it already
	PushMoved       PushClass = "moved"       // the branch holds commits the push lacks
	PushDeclined    PushClass = "declined"    // the host refused it: a hook, a protected branch, a rule
	PushDenied      PushClass = "denied"      // this user may not push there, or no credential answered
	PushUnreachable PushClass = "unreachable" // git could not make the push, or said nothing of the ref
)

// remoteLines is how many lines the host wrote, "remote: ...", a declined
// push's reason carries.
const remoteLines = 3

// ClassifyPush reads the remote's answer to the push of one ref: its
// porcelain status s, found false when git printed none, git's stderr and
// its exit status. A status line decides when there is one: ' ', '*' and
// '+' pushed, '=' up to date, and a rejection moved when git says the
// branch holds commits the push lacks ("fetch first", "non-fast-forward",
// "stale info" for a lease that no longer holds), and otherwise declined,
// as "[remote rejected]" is, with git's reason and the first lines the
// host wrote. With none, stderr decides, as for an access check: a host
// that wants a credential authorised, a known denial, a credential
// refused or not available without asking is denied; an HTTP 403 that
// names no denial is declined; anything else unreachable. A "Done" line,
// which git prints after a rejection too, is never read as success, and
// neither is an exit status of 0 with no status line. reason is what the
// user is told, "" for pushed and up to date. Pure.
func ClassifyPush(s PushStatus, found bool, stderr string, exit int) (PushClass, string) {
	var lines, said []string
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if strings.TrimSpace(strings.TrimPrefix(line, "remote:")) == "" {
			continue // a blank line, or one the host left blank
		}
		lines = append(lines, line)
		if text, ok := strings.CutPrefix(line, "remote:"); ok && len(said) < remoteLines {
			said = append(said, strings.TrimSpace(text))
		}
	}
	withHost := func(reason string) string {
		if len(said) == 0 {
			return reason
		}
		return reason + ": " + strings.Join(said, "; ")
	}
	if found {
		switch s.Flag {
		case ' ', '*', '+':
			return PushPushed, ""
		case '=':
			return PushUpToDate, ""
		case '!':
			switch s.Reason {
			case "fetch first", "non-fast-forward", "stale info":
				return PushMoved, s.Reason
			}
			if s.Summary == "[remote failure]" {
				return PushUnreachable, s.Why()
			}
			return PushDeclined, withHost(s.Why())
		}
		return PushUnreachable, "git push reported " + s.Why() + " for " + s.To
	}
	for _, rule := range []struct {
		match  func(string) bool
		class  PushClass
		reason func(string) string
	}{
		{match: anyCase("SAML SSO", "IP allow list"), class: PushDenied},
		{match: denials, class: PushDenied},
		{match: has("returned error: 403"), class: PushDeclined, reason: func(line string) string {
			if len(said) > 0 {
				return strings.Join(said, "; ")
			}
			return line
		}},
		{match: untrusted, class: PushDenied},
		{match: noPrompt, class: PushDenied, reason: func(string) string { return noCredentials }},
	} {
		for _, line := range lines {
			if !rule.match(line) {
				continue
			}
			if rule.reason != nil {
				return rule.class, rule.reason(line)
			}
			return rule.class, hostText(line)
		}
	}
	if len(lines) > 0 {
		return PushUnreachable, hostText(lines[0])
	}
	return PushUnreachable, fmt.Sprintf("git push exited %d and said nothing of the branch", exit)
}

// DenialFix is what the user does about a push ClassifyPush calls denied.
type DenialFix int

// The fixes of a denied push, see DeniedFix.
const (
	FixCredentials DenialFix = iota // a credential that may push there, or one renewed
	FixHostKey                      // the host's SSH key, trusted once
	FixAuthorise                    // the token or key authorised for the organisation, or the IP allowed
)

// DeniedFix is what the user does about a push ClassifyPush called denied
// with reason: a host that wants the token or key authorised for an
// organisation, single sign-on or an IP allow list, is FixAuthorise; ssh
// not trusting the host yet is FixHostKey; anything else, a denial or a
// credential that was refused or not available, FixCredentials. Pure.
func DeniedFix(reason string) DenialFix {
	switch {
	case anyCase("SAML SSO", "IP allow list")(reason):
		return FixAuthorise
	case has("Host key verification failed")(reason):
		return FixHostKey
	}
	return FixCredentials
}

// hostText is a line of a push's stderr as the user is told it, with the
// "remote: " git puts before what the host wrote taken off.
func hostText(line string) string {
	return strings.TrimSpace(strings.TrimPrefix(line, "remote:"))
}

// StderrFor is the stderr of a push of the branches, short names such as
// "skills/pdf", as ClassifyPush reads it for branch: without the lines
// that name another of the branches and not branch itself, such as the
// link a host writes to open a pull request for each branch it took, so
// that the host's lines a declined branch is told of are its own or the
// push's. Pure.
func StderrFor(stderr, branch string, branches []string) string {
	var kept []string
	for _, line := range strings.Split(stderr, "\n") {
		other := false
		for _, b := range branches {
			if b != branch && namesBranch(line, b) {
				other = true
				break
			}
		}
		if !other || namesBranch(line, branch) {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// namesBranch reports whether line names the branch, its short name or a
// ref ending in it, as a whole name: "skills/pdf" in "refs/heads/skills/pdf"
// or "'skills/pdf'", and not in "skills/pdf-2" or "myskills/pdf".
func namesBranch(line, branch string) bool {
	nameByte := func(c byte) bool {
		return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-'
	}
	for from := 0; ; {
		i := strings.Index(line[from:], branch)
		if i < 0 {
			return false
		}
		start, end := from+i, from+i+len(branch)
		if (start == 0 || !nameByte(line[start-1])) && (end == len(line) || !nameByte(line[end]) && line[end] != '/') {
			return true
		}
		from = start + 1
	}
}
