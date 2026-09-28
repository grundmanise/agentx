package gitx

import (
	"os"
	"sync"
)

// A process that runs many commands, as a test binary or anything that
// embeds the CLI does, would ask the same git for its version once per
// command, and every command asks before it does anything else. The
// answer is a property of the executable, so the process keeps it per
// executable: the file PATH resolved to, known by its path and its status,
// the file itself, its size, mode and modification time. A git that changed
// in any of these is asked again, and nothing is kept of a git that failed
// to answer.
var versions = struct {
	sync.Mutex
	byPath map[string]knownGit
}{byPath: map[string]knownGit{}}

type knownGit struct {
	info    os.FileInfo
	version Version
}

// sameFile reports whether a and b are the status of the same file,
// unchanged.
func sameFile(a, b os.FileInfo) bool {
	return os.SameFile(a, b) && a.Size() == b.Size() && a.Mode() == b.Mode() && a.ModTime().Equal(b.ModTime())
}

// knownVersion is the version the git at path answered earlier in this
// process, if the file there is still the one that answered.
func knownVersion(path string, info os.FileInfo) (Version, bool) {
	versions.Lock()
	defer versions.Unlock()
	k, ok := versions.byPath[path]
	if !ok || !sameFile(k.info, info) {
		return Version{}, false
	}
	return k.version, true
}

// rememberVersion keeps v as the answer of the git at path, whose status
// was info before it ran, unless the file changed while it ran: an answer is
// kept only for the file that gave it.
func rememberVersion(path string, info os.FileInfo, v Version) {
	after, err := os.Stat(path)
	if err != nil || !sameFile(info, after) {
		return
	}
	versions.Lock()
	defer versions.Unlock()
	versions.byPath[path] = knownGit{info: info, version: v}
}
