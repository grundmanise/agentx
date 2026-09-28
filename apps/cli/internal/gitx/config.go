package gitx

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// AddRemoteConfig sets pairs, in order, in the section [remote "name"] of
// the config of the repository at gitDir when the config has no such
// section yet, and leaves the file exactly as git config leaves it after
// setting each pair in turn: the section at the end, its keys in the order
// given. git config sets one key per process; this writes them all at once,
// the way git writes its config: into config.lock, created only if no
// other writer holds it, then renamed over config.
//
// It writes only a config that is plainly what git and agentx write, and
// still is once the section is added (see plainConfig), so that the file
// holds no such section under any spelling, every key and value is one git
// writes as it is, and nothing in it changes how git would write the file.
// It reports whether it wrote. When it did not, nothing changed, and the
// caller sets the keys with git config, which answers for whatever stood in
// the way, a config.lock another writer holds included.
func AddRemoteConfig(gitDir, name string, pairs [][2]string) bool {
	header := `[remote "` + name + `"]`
	var add strings.Builder
	add.WriteString(header + "\n")
	for _, kv := range pairs {
		add.WriteString("\t" + kv[0] + " = " + kv[1] + "\n")
	}
	path := filepath.Join(gitDir, "config")
	lock, err := os.OpenFile(path+".lock", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	if err != nil {
		return false
	}
	b, err := os.ReadFile(path)
	text := string(b)
	ok := err == nil && plainConfig(text) && !slices.Contains(strings.Split(text, "\n"), header) && plainConfig(text+add.String())
	if ok {
		_, err = lock.WriteString(text + add.String())
		ok = err == nil
	}
	if err := lock.Close(); err != nil {
		ok = false
	}
	if ok && os.Rename(lock.Name(), path) == nil {
		return true
	}
	os.Remove(lock.Name())
	return false
}
