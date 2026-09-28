package gitx

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/interrupt"
)

// AccountRepoPath is the git directory of the account repo in agentx home.
func AccountRepoPath(homeDir string) string { return filepath.Join(homeDir, "account.git") }

// CheckAccountRepo reports whether the account repo exists in agentx home
// and, when it does, that git reads it as a bare repository. A repo that
// is plainly one needs no git to say so; any other is left to git, whose
// answer is the error. It changes nothing.
func CheckAccountRepo(ctx context.Context, r *Runner, homeDir string) (gitDir string, exists bool, err error) {
	gitDir = AccountRepoPath(homeDir)
	info, err := os.Stat(gitDir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return gitDir, false, nil
	case err != nil:
		return gitDir, false, fmt.Errorf("account repo %s: %w", gitDir, err)
	}
	if info.IsDir() && plainlyBare(gitDir) {
		return gitDir, true, nil
	}
	if out, err := r.Isolated(ctx, gitDir, "rev-parse", "--is-bare-repository"); err != nil || out != "true" {
		if err == nil {
			err = errors.New("not a bare repository")
		}
		return gitDir, true, fmt.Errorf("account repo %s: %w", gitDir, err)
	}
	return gitDir, true, nil
}

// OpenAccountRepo returns the git directory of the account repo in agentx
// home, creating the repo under the lock when it does not exist yet. A
// present repo that git cannot read is an error; so is one that cannot be
// created. created reports whether this call made it.
func OpenAccountRepo(ctx context.Context, r *Runner, homeDir string) (gitDir string, created bool, err error) {
	gitDir, exists, err := CheckAccountRepo(ctx, r, homeDir)
	if err != nil || exists {
		return gitDir, false, err
	}
	// The recovery this Mutate runs before its own work finishes the journal
	// of an earlier command, whose ref steps a stop may not cut in half; the
	// creation itself keeps the command's context and gives up on a stop
	// like anything else, leaving the temporary directory behind it.
	err = home.Mutate(homeDir, r.Refs(interrupt.Uninterruptible(ctx)), func() error {
		if _, err := os.Stat(gitDir); err == nil {
			return nil // created meanwhile by another command
		}
		created = true
		return createAccountRepo(ctx, r, gitDir)
	})
	return gitDir, created, err
}

// accountConfig is the configuration agentx sets in the account repo when
// it creates it.
var accountConfig = [][2]string{
	{"gc.auto", "0"},                  // maintenance runs on the serve child's timer, never inside a command
	{"core.logAllRefUpdates", "true"}, // a bare repo has no reflogs by default
	{"merge.conflictStyle", "zdiff3"},
}

// createAccountRepo initialises the bare repo with the configuration agentx
// needs, in a temporary directory that is renamed into place only once every
// step succeeded, so a failure leaves no half-configured repo behind.
func createAccountRepo(ctx context.Context, r *Runner, gitDir string) error {
	tmp, err := os.MkdirTemp(filepath.Dir(gitDir), ".account.git.*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	if _, err := r.Isolated(ctx, tmp, "init", "--bare", "--quiet"); err != nil {
		return err
	}
	if err := configureNew(ctx, r, tmp, accountConfig); err != nil {
		return err
	}
	if err := os.Rename(tmp, gitDir); err != nil {
		return fmt.Errorf("account repo %s: %w", gitDir, err)
	}
	return nil
}

// configureNew sets every key of config in the repository git init has
// just made at gitDir, which nothing else can reach yet. git config sets one
// key per process, so a config that is still what git init wrote gets the
// keys in one write instead, in the very bytes git config would leave, see
// appendConfig; any other is set by git config, one key at a time.
func configureNew(ctx context.Context, r *Runner, gitDir string, config [][2]string) error {
	path := filepath.Join(gitDir, "config")
	if b, err := os.ReadFile(path); err == nil {
		if text, ok := appendConfig(string(b), config); ok {
			return os.WriteFile(path, []byte(text), 0o644) // the file is there: it keeps the mode git init gave it
		}
	}
	for _, kv := range config {
		if _, err := r.Isolated(ctx, gitDir, "config", kv[0], kv[1]); err != nil {
			return err
		}
	}
	return nil
}

// appendConfig is text, a config holding the core section alone, once
// every key of config is set in it the way git config sets a key that is
// not there yet: at the end of its section, and in a section of its own at
// the end of the file when the file has none. A key of core therefore
// follows the lines git init wrote, and every other section follows in the
// order its first key was given. ok is false, and git config is left to
// do it, unless text is a [core] line and tab-indented key = value lines,
// each ending in a newline, none of them a key of config, and every name
// and value of config needs no quoting.
func appendConfig(text string, config [][2]string) (string, bool) {
	lines := strings.Split(text, "\n")
	if len(lines) < 2 || lines[0] != "[core]" || lines[len(lines)-1] != "" {
		return "", false
	}
	set := map[string]bool{}
	for _, line := range lines[1 : len(lines)-1] {
		key, value, ok := strings.Cut(line, " = ")
		if !ok || !strings.HasPrefix(key, "\t") || !word(key[1:], "-") || !word(value, "-") {
			return "", false
		}
		set["core."+strings.ToLower(key[1:])] = true
	}
	var core strings.Builder
	var order []string
	sections := map[string]*strings.Builder{}
	for _, kv := range config {
		section, name, ok := strings.Cut(kv[0], ".")
		if !ok || !word(section, "-") || section != strings.ToLower(section) || !word(name, "-") || !word(kv[1], "-") ||
			set[strings.ToLower(kv[0])] {
			return "", false
		}
		set[strings.ToLower(kv[0])] = true
		line := "\t" + name + " = " + kv[1] + "\n"
		if section == "core" {
			core.WriteString(line)
			continue
		}
		if sections[section] == nil {
			order = append(order, section)
			sections[section] = &strings.Builder{}
		}
		sections[section].WriteString(line)
	}
	out := text + core.String()
	for _, section := range order {
		out += "[" + section + "]\n" + sections[section].String()
	}
	return out, true
}

// word reports whether s is not empty and made of ASCII letters, digits and
// the bytes of extra.
func word(s, extra string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; !isLetter(c) && (c < '0' || c > '9') && !strings.ContainsRune(extra, rune(c)) {
			return false
		}
	}
	return true
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
