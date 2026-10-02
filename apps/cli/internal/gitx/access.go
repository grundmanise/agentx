package gitx

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// AccessCheckRef is the branch the access check asks to delete. The check is
// a dry run, so nothing is deleted; and no source holds a branch of this name,
// so even a push that lost its --dry-run would change nothing.
const AccessCheckRef = "refs/heads/agentx-access-check"

// AccessBudget bounds one access check, and one read of a remote's default
// branch: a server that does not answer leaves the access unknown rather
// than the command waiting on it.
const AccessBudget = 30 * time.Second

// Access is what an access check found out about a source: home.AccessWritable,
// home.AccessReadOnly or home.AccessUnknown, why when it is not writable, and
// whether the host wants the user's token or key authorised for an
// organisation, single sign-on or an IP allow list, before it says more.
type Access struct {
	Access    string
	Reason    string // what git or the host said, one line as they wrote it; "" for writable
	Authorise bool
}

// Reasons an access check gives in its own words.
const (
	noCredentials  = "no credentials for pushing were available without asking"
	checkTimedOut  = "the check did not finish in 30s"
	checkStopped   = "the check was stopped"
	pathNotWritten = "this machine cannot write to "
)

// CheckRemote is the remote an access check and a read of a source's
// default branch name: one given on the command line in a throwaway
// repository, see targetRemote.
const CheckRemote = "agentx"

// checkPushRemote is a second remote of the throwaway repository whose URL
// is the source's push URL. Git keys includeIf "hasconfig:remote.*.url:..."
// on remote.<name>.url alone, never on a push URL, so it is the one through
// which a setting the user keys on the push URL applies.
const checkPushRemote = "agentx-push"

// targetRemote is the configuration, as -c options, of CheckRemote for a
// source fetched from url and pushed to at pushURL, "" for none.
func targetRemote(url, pushURL string) []string {
	args := []string{"-c", "remote." + CheckRemote + ".url=" + url}
	if pushURL != "" {
		args = append(args, "-c", "remote."+CheckRemote+".pushurl="+pushURL, "-c", "remote."+checkPushRemote+".url="+pushURL)
	}
	return args
}

// throwawayRepo makes an empty git directory in the temporary directory,
// for a git run that must see the user's configuration as it applies to
// one source and nothing of the account repo's: a HEAD, objects and refs,
// all git asks of a repository, and no config file. Users key settings on a
// remote's URL with includeIf "hasconfig:remote.*.url:...", and inside the
// account repo every source's URL matches at once, so that a setting
// included for one source, an SSH command, a credential or a proxy, would
// apply to every other. remove deletes the directory.
func throwawayRepo() (gitDir string, remove func(), err error) {
	dir, err := os.MkdirTemp("", "agentx-target-*")
	if err != nil {
		return "", nil, err
	}
	remove = func() { os.RemoveAll(dir) }
	for _, sub := range []string{"objects", "refs"} {
		if err := os.Mkdir(filepath.Join(dir, sub), 0o755); err != nil {
			remove()
			return "", nil, err
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		remove()
		return "", nil, err
	}
	return dir, remove, nil
}

// ProbeAccess asks the source fetched from url and pushed to at pushURL,
// "" when pushes go to url, whether this machine may push to it, without
// pushing anything: a dry run of deleting AccessCheckRef, through a remote
// of those URLs so that the user's URL rewrites apply as they would to a
// real push. It needs no object and sends no pack, so it runs from an empty
// throwaway repository (see throwawayRepo), where the user's configuration
// applies as it does to this source alone: an SSH command, a credential or
// any other setting the user keys on another source's URL does not.
//
// The check is a side step of the command, never what the user asked for,
// so it runs unattended (see call.unattended) and within AccessBudget, with
// no hook, no push certificate, no negotiation, no push option and no lazy
// fetch: a push option the user sets for their own pushes makes git give
// up on any server that does not take push options, before it answers.
//
// A dry run over a path proves nothing, since git checks no permission
// before it would write: a writable answer over a path is refined by
// whether this machine may write the repository's directories.
func (r *Runner) ProbeAccess(ctx context.Context, url, pushURL string) Access {
	gitDir, remove, err := throwawayRepo()
	if err != nil {
		return Access{Access: home.AccessUnknown, Reason: firstLine(err.Error())}
	}
	defer remove()
	args := append(networkConfig(), "-c", "push.negotiate=false", "-c", "push.pushOption=")
	args = append(args, targetRemote(url, pushURL)...)
	args = append(args, "--git-dir="+gitDir)
	args = append(args, ProbeArgs(CheckRemote)...)
	budget, cancel := context.WithTimeout(ctx, AccessBudget)
	defer cancel()
	var stderr string
	out, status, err := r.runStatus(budget, call{unattended: true, stderr: &stderr, env: map[string]string{"GIT_NO_LAZY_FETCH": "1"}}, 255, args...)
	switch {
	case ctx.Err() != nil:
		return Access{Access: home.AccessUnknown, Reason: checkStopped}
	case budget.Err() != nil:
		return Access{Access: home.AccessUnknown, Reason: checkTimedOut}
	case err != nil:
		return Access{Access: home.AccessUnknown, Reason: firstLine(err.Error())}
	}
	answer := ClassifyAccess(status, out, stderr)
	if answer.Access == home.AccessWritable {
		if path, ok := LocalPath(pushedTo(out)); ok {
			if denied := unwritable(path); denied != "" {
				return Access{Access: home.AccessReadOnly, Reason: pathNotWritten + denied}
			}
		}
	}
	return answer
}

// ProbeArgs is the push of the access check of the remote called remote,
// after the global options: a dry run of deleting AccessCheckRef that
// signs nothing, runs no hook of the user's and takes no tag or submodule
// along.
func ProbeArgs(remote string) []string {
	return []string{"push", "--dry-run", "--porcelain", "--no-signed", "--no-verify",
		"--no-recurse-submodules", "--no-follow-tags", remote, ":" + AccessCheckRef}
}

// accessRule is one line of git's or a host's answer that decides an
// access check.
type accessRule struct {
	match  func(line string) bool
	access string
	reason func(line string) string // "" keeps the line
}

// has matches a line holding every one of parts, in that order.
func has(parts ...string) func(string) bool {
	return func(line string) bool {
		rest := line
		for _, p := range parts {
			i := strings.Index(rest, p)
			if i < 0 {
				return false
			}
			rest = rest[i+len(p):]
		}
		return true
	}
}

// remoteHas matches a line the host sent, "remote: ...", holding any of parts
// in any case.
func remoteHas(parts ...string) func(string) bool {
	inAnyCase := anyCase(parts...)
	return func(line string) bool {
		return strings.HasPrefix(line, "remote:") && inAnyCase(line)
	}
}

// anyCase matches a line holding any of parts in any case.
func anyCase(parts ...string) func(string) bool {
	return func(line string) bool {
		lower := strings.ToLower(line)
		for _, p := range parts {
			if strings.Contains(lower, strings.ToLower(p)) {
				return true
			}
		}
		return false
	}
}

// anyOf matches a line any of matches matches.
func anyOf(matches ...func(string) bool) func(string) bool {
	return func(line string) bool {
		for _, m := range matches {
			if m(line) {
				return true
			}
		}
		return false
	}
}

// denials are the lines by which git and the hosts agentx knows say that
// this user may read a repository and not write it. Only these make a
// source read-only: a refusal of any other kind may be fixed by the user,
// and calling it a lack of rights would hide that.
var denials = anyOf(
	has("Permission to ", " denied"),        // GitHub over HTTPS and SSH
	has("denied to "),                       // GitHub, naming the user or key
	has("not allowed to push"),              // GitLab
	has("You are not allowed to push code"), // GitLab
	has("does not have write access"),       // a GitLab deploy key
	has("Write access to repository not granted"),
	has("marked as read only"), // a GitHub deploy key
	has("insufficient permission"),
	has("TF401027"),  // Azure DevOps
	has("DENIED by"), // gitolite
	has("was archived so it is read-only"),
	has("access denied or repository not exported"), // git daemon
	has("deployment key", "read-only"),              // Bitbucket
	remoteHas("read-only", "read only"),
)

// noPrompt are the lines git and ssh write when a credential was needed and
// asking for one was not allowed.
var noPrompt = anyOf(
	has("could not read Username"),
	has("could not read Password"),
	has("terminal prompts disabled"),
	has("returned error: 401"),
	has("Permission denied (publickey"),
)

// untrusted are the lines git and ssh write when a credential was offered and
// refused, an expired token, or when ssh does not trust the host yet: the
// user has something to renew or a host to trust, and not a credential to
// find, so the line itself is the reason.
var untrusted = anyOf(
	has("Authentication failed"),
	has("Host key verification failed"),
)

// ClassifyAccess reads the answer of the access check's dry run: git's exit
// status, its porcelain output and its stderr. The status line of the
// check's ref is read first, then stderr line by line; the exit status
// alone decides nothing. Writable is exit 0 with a status line for the ref
// that is not a rejection. Read-only needs a known denial, see denials.
// Everything else is unknown, with a reason: a host that wants a token or
// key authorised for an organisation, or an IP allow list, on any line,
// since over SSH GitHub writes it with no "remote:", is unknown with that
// line as the reason and says so (Authorise), since the user can fix it;
// so is a 403 that names no denial, a credential that was refused or not
// available without asking, a host not trusted yet, and a server that
// cannot be reached. Pure.
func ClassifyAccess(status int, stdout, stderr string) Access {
	if status == 0 {
		for _, line := range strings.Split(stdout, "\n") {
			if fields := strings.SplitN(line, "\t", 3); len(fields) == 3 && strings.HasSuffix(fields[1], ":"+AccessCheckRef) && fields[0] != "!" {
				return Access{Access: home.AccessWritable}
			}
		}
	}
	var lines []string
	for _, line := range strings.Split(stderr, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	firstRemote := ""
	for _, line := range lines {
		if strings.HasPrefix(line, "remote:") && strings.TrimSpace(strings.TrimPrefix(line, "remote:")) != "" {
			firstRemote = line
			break
		}
	}
	rules := []accessRule{
		{match: anyCase("SAML SSO", "IP allow list"), access: home.AccessUnknown},
		{match: denials, access: home.AccessReadOnly},
		{match: has("returned error: 403"), access: home.AccessUnknown, reason: func(line string) string {
			if firstRemote != "" {
				return firstRemote
			}
			return line
		}},
		{match: untrusted, access: home.AccessUnknown},
		{match: noPrompt, access: home.AccessUnknown, reason: func(string) string { return noCredentials }},
	}
	for i, rule := range rules {
		for _, line := range lines {
			if !rule.match(line) {
				continue
			}
			reason := line
			if rule.reason != nil {
				reason = rule.reason(line)
			}
			return Access{Access: rule.access, Reason: reason, Authorise: i == 0}
		}
	}
	for _, line := range strings.Split(stdout, "\n") {
		if fields := strings.SplitN(line, "\t", 3); len(fields) == 3 && fields[0] == "!" {
			return Access{Access: home.AccessUnknown, Reason: strings.TrimSpace(fields[2])}
		}
	}
	if len(lines) > 0 {
		return Access{Access: home.AccessUnknown, Reason: lines[0]}
	}
	return Access{Access: home.AccessUnknown, Reason: "git push exited " + strconv.Itoa(status) + " and said nothing"}
}

// pushedTo is the URL git push --porcelain names on its "To" line, which is
// where the push went once every URL rewrite was applied; "" when it printed
// none.
func pushedTo(stdout string) string {
	for _, line := range strings.Split(stdout, "\n") {
		if rest, ok := strings.CutPrefix(line, "To "); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// scpLike is git's scp form, [user@]host:path, which is not a path: a colon
// before the first slash.
var scpLike = regexp.MustCompile(`^[^/]*:`)

// LocalPath is the directory a URL git pushes to over a path transport
// names: a file URL, whose host git ignores and whose path it decodes, or a
// path. ok is false for every other transport. Pure.
func LocalPath(raw string) (path string, ok bool) {
	if raw == "" {
		return "", false
	}
	if rest, isFile := strings.CutPrefix(raw, "file://"); isFile {
		if i := strings.Index(rest, "/"); i > 0 {
			rest = rest[i:] // file://host/path: the host is ignored
		}
		if decoded, err := url.PathUnescape(rest); err == nil {
			rest = decoded
		}
		return rest, rest != ""
	}
	if strings.Contains(raw, "://") || scpLike.MatchString(raw) {
		return "", false
	}
	return raw, true
}

// RepoDirs are the directories a push to the repository at a path writes:
// its git directory, its objects and where its refs live, which is
// refs/heads for files and reftable for a reftable repository. Git finds the
// repository as it does for a push, trying path/.git, path, path.git/.git
// and path.git in that order; a .git file names the git directory, and a
// linked worktree's git directory keeps objects and refs in its common
// directory. ok is false when none of them is a repository. It reads the
// file system and nothing else.
func RepoDirs(path string) (dirs []string, ok bool) {
	for _, candidate := range []string{path + "/.git", path, path + ".git/.git", path + ".git"} {
		gitDir, isRepo := gitDirAt(candidate)
		if !isRepo {
			continue
		}
		common := gitDir
		if b, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
			c := strings.TrimSpace(string(b))
			if !filepath.IsAbs(c) {
				c = filepath.Join(gitDir, c)
			}
			common = filepath.Clean(c)
		}
		refs := filepath.Join(common, "refs", "heads")
		if info, err := os.Stat(filepath.Join(common, "reftable")); err == nil && info.IsDir() {
			refs = filepath.Join(common, "reftable")
		}
		return []string{gitDir, filepath.Join(common, "objects"), refs}, true
	}
	return nil, false
}

// gitDirAt is the git directory at candidate: candidate itself when it is a
// directory holding HEAD and objects (or a commondir), or the directory a
// .git file there names.
func gitDirAt(candidate string) (string, bool) {
	info, err := os.Stat(candidate)
	if err != nil {
		return "", false
	}
	if !info.IsDir() {
		b, err := os.ReadFile(candidate)
		if err != nil {
			return "", false
		}
		named, found := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir:")
		if !found {
			return "", false
		}
		named = strings.TrimSpace(named)
		if !filepath.IsAbs(named) {
			named = filepath.Join(filepath.Dir(candidate), named)
		}
		candidate = filepath.Clean(named)
	}
	if _, err := os.Stat(filepath.Join(candidate, "HEAD")); err != nil {
		return "", false
	}
	for _, marker := range []string{"objects", "commondir"} {
		if _, err := os.Stat(filepath.Join(candidate, marker)); err == nil {
			return candidate, true
		}
	}
	return "", false
}

// unwritable is the first directory of the repository at path that this
// machine may not write, as access(2) answers for the real user, or "" when
// it may write them all or path holds no repository it can find.
func unwritable(path string) string {
	dirs, ok := RepoDirs(path)
	if !ok {
		return ""
	}
	for _, d := range dirs {
		if err := unix.Access(d, unix.W_OK); err != nil && !errors.Is(err, unix.ENOENT) {
			return d
		}
	}
	return ""
}

// firstLine is the first line of text.
func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return line
}

// DefaultBranch is the branch the HEAD of the repository at url names,
// read with ls-remote --symref in the user's environment, from a throwaway
// repository, unattended and within AccessBudget as the access check is:
// "" when the remote's HEAD is no branch, or names none. It is shown to the
// user and decides nothing.
func (r *Runner) DefaultBranch(ctx context.Context, url string) (string, error) {
	gitDir, remove, err := throwawayRepo()
	if err != nil {
		return "", err
	}
	defer remove()
	budget, cancel := context.WithTimeout(ctx, AccessBudget)
	defer cancel()
	args := append(networkConfig(), targetRemote(url, "")...)
	args = append(args, "--git-dir="+gitDir, "ls-remote", "--symref", CheckRemote, "HEAD")
	out, err := r.run(budget, call{unattended: true}, args...)
	if err != nil {
		if budget.Err() != nil && ctx.Err() == nil {
			return "", fmt.Errorf("git ls-remote: %s", checkTimedOut)
		}
		return "", err
	}
	return ParseSymref(out), nil
}

// ParseSymref reads the branch ls-remote --symref names for HEAD, from its
// "ref: refs/heads/<branch>\tHEAD" line; "" when there is none. Pure.
func ParseSymref(out string) string {
	for _, line := range strings.Split(out, "\n") {
		rest, ok := strings.CutPrefix(line, "ref: ")
		if !ok {
			continue
		}
		target, name, ok := strings.Cut(rest, "\t")
		if !ok || strings.TrimSpace(name) != "HEAD" {
			continue
		}
		if branch, ok := strings.CutPrefix(target, "refs/heads/"); ok {
			return branch
		}
	}
	return ""
}
