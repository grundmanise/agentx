package gitx

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// plainlyBare reports whether gitDir is plainly a bare repository git
// opens: whether git rev-parse --is-bare-repository, run on it in the
// isolated environment, would answer true, told from its files without
// running git. Nearly every command opens the account repo and nearly
// always finds it as git and agentx left it, so this spares each of them a
// git process. It checks what git checks before that answer, the same way
// or more strictly:
//
//   - the process has a working directory, which git reads first of all;
//   - HEAD is a file naming a ref, as git init writes it;
//   - objects and refs can be searched, which is what git asks of them;
//   - there is no commondir, which would make the directory a linked
//     worktree's reading its objects and refs elsewhere;
//   - config parses as the plain lines git config writes, and holds
//     core.repositoryformatversion 0, without which git ignores what
//     core.bare says, and core.bare true, and nothing git would have to
//     judge before it answers: see plainConfig.
//
// Anything else is not plain, and git answers instead, so a false changes
// nothing but the cost: a repository git cannot read, or reads as not bare,
// is reported with git's own words, as it always was.
func plainlyBare(gitDir string) bool {
	if _, err := os.Getwd(); err != nil {
		return false
	}
	head, err := os.Lstat(filepath.Join(gitDir, "HEAD"))
	if err != nil || !head.Mode().IsRegular() {
		return false
	}
	if b, err := os.ReadFile(filepath.Join(gitDir, "HEAD")); err != nil || !strings.HasPrefix(string(b), "ref: refs/") {
		return false
	}
	for _, dir := range []string{"objects", "refs"} {
		if unix.Access(filepath.Join(gitDir, dir), unix.X_OK) != nil {
			return false
		}
	}
	if _, err := os.Lstat(filepath.Join(gitDir, "commondir")); !errors.Is(err, fs.ErrNotExist) {
		return false
	}
	config, err := os.ReadFile(filepath.Join(gitDir, "config"))
	return err == nil && plainConfig(string(config))
}

// plainConfig reports whether config is the configuration of a bare
// repository that git reads without judging anything in it: every line is
// blank, a comment, a section header or a key = value pair with no
// quoting, escaping or continuation; the only sections are core and the
// ones git does not read before it answers, gc, merge and a remote's; core
// holds only the keys git init and agentx write, each with a value git
// reads as it is, repositoryformatversion 0 among them; and core.bare is
// true wherever it is said. An include, an extension, a work tree, a value
// git would parse and a section it does not know all make it not plain.
func plainConfig(config string) bool {
	var section string
	version, bare := false, false
	for _, line := range strings.Split(config, "\n") {
		line = strings.Trim(line, " \t")
		switch {
		case !printable(line):
			return false
		case line == "", line[0] == '#', line[0] == ';':
		case line[0] == '[':
			name, sub, ok := plainHeader(line)
			if !ok {
				return false
			}
			switch {
			case (name == "core" || name == "gc" || name == "merge") && sub == "":
			case name == "remote" && sub != "":
			default:
				return false
			}
			section = name
		default:
			key, value, ok := plainPair(line)
			if !ok || section == "" {
				return false
			}
			if section != "core" {
				continue
			}
			switch key {
			case "repositoryformatversion":
				if value != "0" {
					return false
				}
				version = true
			case "bare":
				if value != "true" {
					return false
				}
				bare = true
			case "filemode", "symlinks", "ignorecase", "precomposeunicode", "logallrefupdates":
				if value != "true" && value != "false" {
					return false
				}
			default:
				return false
			}
		}
	}
	return version && bare
}

// printable reports whether line holds no control character but a tab:
// no carriage return, no NUL, nothing git might read otherwise.
func printable(line string) bool {
	for i := 0; i < len(line); i++ {
		if c := line[i]; (c < ' ' && c != '\t') || c == 0x7f {
			return false
		}
	}
	return true
}

// plainHeader reads a section header, [name] or [name "sub"], the name of
// letters, digits and hyphens and the subsection of those, dots and
// underscores, with nothing after it. The name is lowercased, as git
// compares it.
func plainHeader(line string) (name, sub string, ok bool) {
	inner, ok := strings.CutPrefix(line, "[")
	if !ok {
		return "", "", false
	}
	if inner, ok = strings.CutSuffix(inner, "]"); !ok {
		return "", "", false
	}
	name, quoted, hasSub := strings.Cut(inner, " ")
	if !word(name, "-") {
		return "", "", false
	}
	if hasSub {
		sub, ok = strings.CutPrefix(quoted, `"`)
		if !ok {
			return "", "", false
		}
		if sub, ok = strings.CutSuffix(sub, `"`); !ok || !word(sub, "-._") {
			return "", "", false
		}
	}
	return strings.ToLower(name), sub, true
}

// plainPair reads key = value, the key starting with a letter and made of
// letters, digits and hyphens, the value free of what git would unquote,
// unescape or cut off as a comment. A key with no value, which git reads as
// true, is not plain. The key is lowercased, as git compares it.
func plainPair(line string) (key, value string, ok bool) {
	key, value, ok = strings.Cut(line, "=")
	if !ok {
		return "", "", false
	}
	key = strings.TrimRight(key, " \t")
	value = strings.Trim(value, " \t")
	if !word(key, "-") || !isLetter(key[0]) || strings.ContainsAny(value, "\"\\#;") {
		return "", "", false
	}
	return strings.ToLower(key), value, true
}
