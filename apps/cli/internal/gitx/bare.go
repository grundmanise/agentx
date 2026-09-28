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
// opens, that is, whether git rev-parse --is-bare-repository, run on it in
// the isolated environment, would answer true, told from its files without
// running git. Nearly every command checks the account repo and nearly
// always finds it as git and agentx left it, so this spares each of them a
// git process. It asks, the same way git does or more strictly:
//
//   - that the process has a working directory, which git reads first;
//   - that HEAD is a file naming a ref, as git init writes it;
//   - that objects and refs can be searched, which is what git asks of them;
//   - that there is no commondir, which would send git elsewhere for both;
//   - that config is exactly what git and agentx write, with
//     core.repositoryformatversion 0, without which git ignores core.bare,
//     and core.bare true: see plainConfig.
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

// plainConfig reports whether config is, line for line, what git init,
// git config and agentx write into the account repo: the header lines
// [core], [gc], [merge] and [remote "<name>"], each followed by tab-indented
// key = value lines, every key one of those agentx or git init writes, in
// the case they write it, and every value one git config writes as it is.
// core.repositoryformatversion must be 0 and core.bare true. No comment,
// blank line, include, extension or other key is plain, whatever git would
// make of it; nor is a value git would unquote or unescape.
func plainConfig(config string) bool {
	body, ok := strings.CutSuffix(config, "\n")
	if !ok {
		return false
	}
	var section string
	version, bare := false, false
	for _, line := range strings.Split(body, "\n") {
		if pair, ok := strings.CutPrefix(line, "\t"); ok && section != "" {
			key, value, ok := strings.Cut(pair, " = ")
			if !ok || !plainValue(value) {
				return false
			}
			switch section + "." + key {
			case "core.repositoryformatversion":
				if value != "0" {
					return false
				}
				version = true
			case "core.bare":
				if value != "true" {
					return false
				}
				bare = true
			case "core.filemode", "core.symlinks", "core.ignorecase", "core.precomposeunicode", "core.logAllRefUpdates":
				if value != "true" && value != "false" {
					return false
				}
			case "gc.auto", "merge.conflictStyle", // git reads none of these to open a repository
				"remote.url", "remote.fetch", "remote.tagOpt", "remote.promisor", "remote.partialclonefilter":
			default:
				return false
			}
			continue
		}
		switch {
		case line == "[core]", line == "[gc]", line == "[merge]":
			section = line[1 : len(line)-1]
		case plainRemoteHeader(line):
			section = "remote"
		default:
			return false
		}
	}
	return version && bare
}

// plainRemoteHeader reports whether line is [remote "<name>"] with a name
// of lowercase letters, digits and hyphens, as agentx names a source's
// remote.
func plainRemoteHeader(line string) bool {
	name, ok := strings.CutPrefix(line, `[remote "`)
	if !ok {
		return false
	}
	if name, ok = strings.CutSuffix(name, `"]`); !ok || name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		if c := name[i]; (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

// plainValue reports whether git config writes value as it is and reads it
// back the same: printable ASCII, no leading or trailing space, and nothing
// it would quote or escape.
func plainValue(value string) bool {
	if value == "" || value[0] == ' ' || value[len(value)-1] == ' ' {
		return false
	}
	for i := 0; i < len(value); i++ {
		if c := value[i]; c < ' ' || c > '~' || c == '"' || c == '\\' || c == ';' || c == '#' {
			return false
		}
	}
	return true
}
