package home

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// checkedPath is the file that says when an update check of agentx home
// dir last ended well: the time, as RFC 3339. It has a directory of its own,
// which the serve child does not watch, so a write of it starts no rescan.
func checkedPath(dir string) string { return filepath.Join(dir, "checks", "updates") }

// Checked is when an update check of agentx home dir last ended well, and
// false when the file that says so is missing or does not hold a time.
func Checked(dir string) (time.Time, bool) {
	b, err := os.ReadFile(checkedPath(dir))
	if err != nil {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(b)))
	return t, err == nil
}

// SetChecked records that an update check of agentx home dir ended well
// at t. The file is written whole or not at all, and no journal covers it:
// losing it only makes an inventory name the check before.
func SetChecked(dir string, t time.Time) error {
	path := checkedPath(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeAtomic(path, []byte(t.UTC().Format(time.RFC3339)+"\n"))
}
