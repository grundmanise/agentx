package home

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MaintainedPath is the file that says when the serve child last
// maintained the account repo: the time, as RFC 3339.
func MaintainedPath(dir string) string { return filepath.Join(dir, "maintained") }

// Maintained is when the account repo of agentx home dir was last
// maintained, and false when the file that says so is missing or does not
// hold a time.
func Maintained(dir string) (time.Time, bool) {
	b, err := os.ReadFile(MaintainedPath(dir))
	if err != nil {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(b)))
	return t, err == nil
}

// SetMaintained records that the account repo of agentx home dir was
// maintained at t. The file is written whole or not at all, and no journal
// covers it: losing it only makes the next serve child maintain once more.
func SetMaintained(dir string, t time.Time) error {
	return writeAtomic(MaintainedPath(dir), []byte(t.UTC().Format(time.RFC3339)+"\n"))
}

// BumpVersion rewrites the version file of agentx home dir, the change
// signal every mutation ends with, for a hold of the lock taken without it
// that turned out to change something. Call it under the exclusive lock.
func BumpVersion(dir string) error { return bumpVersion(dir) }
