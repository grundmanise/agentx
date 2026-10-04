package home

import (
	"bytes"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// SystemFiles are the files an operating system or an editor leaves in a
// directory on its own: Finder and Explorer metadata, the leftovers of a
// file still open on a network or FUSE file system, and editors' swap,
// backup and lock files. While ignore_system_files is on, git ignores them
// in every skill directory, so none of them makes a skill modified, shows
// in a diff or is lost to an update. Each is a shell glob matched against a
// base name, which is how git reads a pattern with no slash in it.
var SystemFiles = []string{
	".DS_Store", "._*", ".AppleDouble", ".LSOverride",
	"Thumbs.db", "ehthumbs.db", "desktop.ini",
	".directory", ".fuse_hidden*", ".nfs*",
	"*.swp", "*.swo", "*~", ".#*",
}

// IsSystemFile reports whether a base name matches one of SystemFiles.
func IsSystemFile(name string) bool {
	for _, pattern := range SystemFiles {
		if ok, _ := path.Match(pattern, name); ok {
			return true
		}
	}
	return false
}

// SyncExclude makes the info/exclude file of the account repo at gitDir
// hold SystemFiles, one per line, while on is true, and nothing otherwise.
// It writes only when the file holds something else. Nothing else is ever
// written there: the file follows the setting, and every git run over a
// skill directory syncs it first.
func SyncExclude(gitDir string, on bool) error {
	var want []byte
	if on {
		want = []byte(strings.Join(SystemFiles, "\n") + "\n")
	}
	file := filepath.Join(gitDir, "info", "exclude")
	if have, err := os.ReadFile(file); err == nil && bytes.Equal(have, want) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	return os.WriteFile(file, want, 0o644)
}
