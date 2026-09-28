package gitx

import (
	"bytes"
	"io"
	"os"
	"sync"
)

// A process that runs many commands, as a test binary or anything that
// embeds the CLI does, would ask the same git for its version once per
// command, and every command asks before it does anything else. The
// answer is a property of the executable, so the process keeps it per
// executable: the file PATH resolved to, known by its path, its identity
// on disk, its size, mode and modification time and, for a file small
// enough to be a script, its whole content, so that a script rewritten in
// place within one tick of the clock is still another git. Nothing is kept
// of a git that failed to answer, and a git that changed is asked again.
var versions = struct {
	sync.Mutex
	byPath map[string]knownGit
}{byPath: map[string]knownGit{}}

type knownGit struct {
	exe     executable
	version Version
}

// scriptSize bounds the files whose content identifies them: a script that
// stands in for git is a few hundred bytes, while a git binary is
// megabytes that reading every command would cost more than asking it.
const scriptSize = 64 << 10

// executable is what identifies the file at path.
type executable struct {
	path    string
	info    os.FileInfo
	content []byte // the whole file when it is at most scriptSize bytes
}

// identify reads what identifies the executable at path; ok is false when
// it cannot be read, and then nothing is kept of it.
func identify(path string) (exe executable, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return exe, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return exe, false
	}
	exe = executable{path: path, info: info}
	if info.Size() <= scriptSize {
		if exe.content, err = io.ReadAll(io.LimitReader(f, scriptSize+1)); err != nil {
			return exe, false
		}
	}
	return exe, true
}

// same reports whether e and o identify the same file with the same bytes.
func (e executable) same(o executable) bool {
	return e.path == o.path && os.SameFile(e.info, o.info) && e.info.Size() == o.info.Size() &&
		e.info.Mode() == o.info.Mode() && e.info.ModTime().Equal(o.info.ModTime()) && bytes.Equal(e.content, o.content)
}

// knownVersion is the version exe answered earlier in this process, if the
// file at its path is still the one that answered.
func knownVersion(exe executable) (Version, bool) {
	versions.Lock()
	defer versions.Unlock()
	k, ok := versions.byPath[exe.path]
	if !ok || !k.exe.same(exe) {
		return Version{}, false
	}
	return k.version, true
}

// rememberVersion keeps v as the answer of exe, identified before it ran,
// unless the file changed while it ran: an answer is kept only for the file
// that gave it.
func rememberVersion(exe executable, v Version) {
	after, ok := identify(exe.path)
	if !ok || !after.same(exe) {
		return
	}
	versions.Lock()
	defer versions.Unlock()
	versions.byPath[exe.path] = knownGit{exe: exe, version: v}
}
