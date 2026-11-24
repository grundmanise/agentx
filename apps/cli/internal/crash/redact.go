package crash

import (
	"cmp"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// MaxValue is how many bytes of a panic's message a crash report keeps.
const MaxValue = 1024

// Paths are the directories of one run that a crash report names by a
// placeholder: each of them, wherever it appears, says who the user is or
// where they keep their files.
type Paths struct {
	Home    string // the agentx home: <agentx-home>
	Library string // <library>
	Config  string // XDG_CONFIG_HOME: <xdg-config>
	Temp    string // the temporary directory: <tmp>
	User    string // HOME: ~
}

// userPath is an absolute path under a directory that holds users' files,
// up to the next whitespace, quote or colon. It starts a string or follows
// a character that cannot be part of a path, so that a module path such as
// .../internal/home/dirs.go is left whole.
var userPath = regexp.MustCompile(`(^|[^\w.\-/~<>])(?:/Users/|/home/|/root/|/private/|/tmp/|/var/folders/)[^\s"':]*`)

// Redact replaces every directory of p in s with its placeholder, the
// longest first so that the agentx home inside HOME is named as the agentx
// home, then every other absolute path under a users' directory with
// <path>. A directory is replaced only where its name ends, so that HOME
// /home/jo leaves /home/joe to the second rule.
func Redact(s string, p Paths) string {
	type dir struct{ path, placeholder string }
	var dirs []dir
	for _, d := range []dir{
		{p.Home, "<agentx-home>"}, {p.Library, "<library>"}, {p.Config, "<xdg-config>"},
		{p.Temp, "<tmp>"}, {p.User, "~"},
	} {
		if d.path == "" {
			continue
		}
		d.path = filepath.Clean(d.path)
		if d.path == "/" || !filepath.IsAbs(d.path) {
			continue
		}
		dirs = append(dirs, d)
	}
	slices.SortStableFunc(dirs, func(a, b dir) int { return cmp.Compare(len(b.path), len(a.path)) })
	for _, d := range dirs {
		s = replaceDir(s, d.path, d.placeholder)
	}
	return userPath.ReplaceAllString(s, "$1<path>")
}

// replaceDir replaces each occurrence of dir in s that ends where a path
// name ends.
func replaceDir(s, dir, placeholder string) string {
	var b strings.Builder
	for {
		i := strings.Index(s, dir)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		end := i + len(dir)
		if end < len(s) && namePart(s[end]) {
			b.WriteString(s[:end])
		} else {
			b.WriteString(s[:i])
			b.WriteString(placeholder)
		}
		s = s[end:]
	}
}

// namePart says whether c can continue a file name.
func namePart(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
		c == '.' || c == '_' || c == '-' || c == '+' || c == '@' || c >= utf8.RuneSelf
}

// Truncate cuts s to at most n bytes, at a rune boundary.
func Truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// Report is what a crash report carries of p: its type, its message
// redacted and cut to MaxValue, and its frames with their file names
// redacted.
func (p *Panic) Report(paths Paths) (exceptionType, exceptionValue string, frames []Frame) {
	frames = make([]Frame, len(p.Frames))
	for i, f := range p.Frames {
		f.Filename = Redact(f.Filename, paths)
		frames[i] = f
	}
	return p.Type(), Truncate(Redact(fmt.Sprint(p.Value), paths), MaxValue), frames
}
