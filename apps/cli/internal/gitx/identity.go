package gitx

import (
	"context"
	"io"
	"strings"
)

// Ident is the author and committer of a commit agentx writes on a fork's
// branch: the user's own identity, or the machine's when they have none.
type Ident struct {
	Name  string
	Email string
}

// Sanitised is the identity with what git would drop or refuse taken out:
// angle brackets, which git strips from a name without a word, and NUL and
// line breaks, which no identity line can carry, with the surrounding
// whitespace trimmed. A part that is empty afterwards is no identity.
func (i Ident) Sanitised() Ident {
	clean := func(s string) string {
		return strings.TrimSpace(strings.Map(func(r rune) rune {
			switch r {
			case '<', '>', 0, '\r', '\n':
				return -1
			}
			return r
		}, s))
	}
	return Ident{Name: clean(i.Name), Email: clean(i.Email)}
}

// UserConfig is what agentx reads of the user's own git configuration: the
// identity their commits carry and the global ignore file they named with
// core.excludesFile, "" for each they did not set.
type UserConfig struct {
	Ident        Ident
	ExcludesFile string
}

// ReadUserConfig reads the user's identity and core.excludesFile in one git
// process of the user's environment, against the account repo so that
// every file git reads configuration from, the system's, the user's, the
// files they include and the repository's own, is read as git reads it for
// any command: git config --get-regexp, whose last value of a key wins.
// --path is not given to that read, since it would apply to the name too and
// fail the whole read on one git cannot expand; an excludes file that needs
// expanding, one starting with ~ or naming %(prefix), is read again with
// UserExcludesFile, the read every other command makes of it, so that every
// git agentx runs over a skill directory is given the same path. A
// configuration git cannot read is no identity and no excludes file.
func (r *Runner) ReadUserConfig(ctx context.Context, gitDir string) UserConfig {
	out, _, err := r.UserStatus(ctx, gitDir, 1, "config", "-z", "--get-regexp", `^(user\.(name|email)|core\.excludesfile)$`)
	if err != nil {
		return UserConfig{}
	}
	var c UserConfig
	for _, record := range strings.Split(out, "\x00") {
		key, value, _ := strings.Cut(record, "\n")
		switch strings.ToLower(key) {
		case "user.name":
			c.Ident.Name = value
		case "user.email":
			c.Ident.Email = value
		case "core.excludesfile":
			c.ExcludesFile = value
		}
	}
	if strings.HasPrefix(c.ExcludesFile, "~") || strings.Contains(c.ExcludesFile, "%(") {
		c.ExcludesFile = r.UserExcludesFile(ctx, gitDir)
	}
	return c
}

// UserStatus is User for a git whose exit status is part of its answer, as
// IsolatedStatus is for the isolated environment: an exit status from 1 to
// upTo is returned with stdout as is and no error.
func (r *Runner) UserStatus(ctx context.Context, gitDir string, upTo int, args ...string) (string, int, error) {
	return r.runStatus(ctx, call{}, upTo, append([]string{"--git-dir=" + gitDir}, args...)...)
}

// CommitTreeAs writes a commit of tree with parents and message in the
// isolated environment, as every commit agentx writes is written, with id
// as its author and committer and when, "<epoch> <+hhmm>", as both dates.
// Nothing of the user's configuration applies besides the identity it
// names: no hook runs and nothing signs it. It returns the commit's id.
func (r *Runner) CommitTreeAs(ctx context.Context, gitDir string, id Ident, when, tree string, parents []string, message string) (string, error) {
	args := []string{"commit-tree", tree}
	for _, p := range parents {
		args = append(args, "-p", p)
	}
	out, err := r.run(ctx, identityCall(id, when, strings.NewReader(message)), isolatedArgs(gitDir, args)...)
	return strings.TrimSpace(out), err
}

// identityCall is an isolated call whose author and committer are id at
// when, in place of agentx's fixed identity and date.
func identityCall(id Ident, when string, stdin io.Reader) call {
	return call{isolated: true, dates: when, stdin: stdin, env: map[string]string{
		"GIT_AUTHOR_NAME": id.Name, "GIT_AUTHOR_EMAIL": id.Email,
		"GIT_COMMITTER_NAME": id.Name, "GIT_COMMITTER_EMAIL": id.Email,
	}}
}
