package lineage

import (
	"crypto/rand"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// The trailers of the commits agentx writes on a fork's branch, beside the
// four of an import commit, which a fork's history holds as its base
// versions. None of them ever enters an import commit.
const (
	// TrailerBase names, on every merge commit agentx writes on a fork's
	// branch, the import commit that is the fork's base after the merge. A
	// greenfield skill has none to name.
	TrailerBase = "Agentx-Base"
	// TrailerForkID is the permanent id of a fork, a new UUID on the commit
	// that creates the fork. That commit is never amended, so the id never
	// changes; a fork installed elsewhere keeps it, a new fork gets its own.
	TrailerForkID = "Agentx-Fork-ID"
	// TrailerMachine is the id of the machine that wrote the commit, on
	// every commit agentx writes on a fork's branch. A commit without it was
	// made with git directly.
	TrailerMachine = "Agentx-Machine"
)

// ForkTrailers are the trailers of one commit on a fork's branch, "" for
// each it does not carry.
type ForkTrailers struct {
	Base    string // the base import commit a merge records
	ForkID  string // the fork's id, on its creation commit
	Machine string // the machine that wrote the commit
}

// ErrForkTrailer is the error of a message whose fork trailers agentx
// cannot read.
var ErrForkTrailer = errors.New("unreadable fork trailers")

var (
	forkID    = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	machineID = regexp.MustCompile(`^[0-9a-f]{32}$`)
	// trailerLine is a line git reads as a trailer, "<token>: <value>".
	trailerLine = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*:[ \t]`)
)

// ParseFork reads the fork trailers of a commit message, from its trailer
// block alone, as Parse reads an import commit's: a user's message that
// quotes a trailer in its body is not lineage. One of the three given twice,
// or with a value of the wrong shape, is an error, since a reader that
// picked one would be guessing. Every other Agentx- trailer is left to its
// own reader: an import commit's four, and whatever a later agentx adds.
func ParseFork(message string) (ForkTrailers, error) {
	var t ForkTrailers
	seen := map[string]bool{}
	for _, line := range strings.Split(trailerBlock(message), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		var field *string
		var shape *regexp.Regexp
		switch key {
		case TrailerBase:
			field, shape = &t.Base, objectID
		case TrailerForkID:
			field, shape = &t.ForkID, forkID
		case TrailerMachine:
			field, shape = &t.Machine, machineID
		default:
			continue
		}
		if seen[key] {
			return ForkTrailers{}, fmt.Errorf("%w: the trailer %s is given twice", ErrForkTrailer, key)
		}
		seen[key] = true
		if !shape.MatchString(value) {
			return ForkTrailers{}, fmt.Errorf("%w: %s %q is not what agentx writes there", ErrForkTrailer, key, value)
		}
		*field = value
	}
	return t, nil
}

// ForkMessage is the message of a commit agentx writes on a fork's branch:
// the subject, the body when there is one, and the trailers it carries in
// the order Base, Fork-ID, Machine. A body that ends in trailers of its
// own, such as a Signed-off-by a user gave, has agentx's added to that
// block, so that git still reads the user's as trailers. A message
// ParseFork would not read back as t is refused, as Unrecordable refuses
// an import commit's, so that the two sides cannot drift apart: a subject
// or body may not carry an agentx trailer of its own in that block.
func ForkMessage(subject, body string, t ForkTrailers) (string, error) {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(subject))
	body = strings.Trim(body, "\n")
	if body != "" {
		b.WriteString("\n\n" + body)
	}
	var trailers []string
	for _, kv := range [][2]string{{TrailerBase, t.Base}, {TrailerForkID, t.ForkID}, {TrailerMachine, t.Machine}} {
		if kv[1] != "" {
			trailers = append(trailers, kv[0]+": "+kv[1])
		}
	}
	if len(trailers) > 0 {
		separator := "\n\n"
		if isTrailerBlock(trailerBlock("\n\n" + body)) {
			separator = "\n"
		}
		b.WriteString(separator + strings.Join(trailers, "\n"))
	}
	message := b.String() + "\n"
	got, err := ParseFork(message)
	if err != nil {
		return "", err
	}
	if got != t {
		return "", fmt.Errorf("%w: the message would be read back as %+v, not %+v", ErrForkTrailer, got, t)
	}
	return message, nil
}

// isTrailerBlock reports whether the paragraph is one git reads as
// trailers: a trailer on its first line, and on every other line a trailer
// or a value continued from the line before.
func isTrailerBlock(paragraph string) bool {
	if paragraph == "" {
		return false
	}
	for i, line := range strings.Split(paragraph, "\n") {
		continued := i > 0 && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t"))
		if !continued && !trailerLine.MatchString(line) {
			return false
		}
	}
	return true
}

// RenameSubject is the subject of the commit skill rename writes on top
// of the old skill's history when it renames old to newName.
func RenameSubject(old, newName string) string {
	return "Rename " + old + " to " + newName
}

// RenamedFrom is the old name the subject of message, its first line,
// records a rename from, see RenameSubject; "" when it records none. A
// skill's name holds no space, so a subject whose names would is none.
func RenamedFrom(message string) string {
	subject, _, _ := strings.Cut(message, "\n")
	rest, ok := strings.CutPrefix(subject, "Rename ")
	if !ok {
		return ""
	}
	old, newName, ok := strings.Cut(rest, " to ")
	if !ok || old == "" || newName == "" || strings.ContainsAny(old+newName, " \t") {
		return ""
	}
	return old
}

// NewForkID is a new random fork id, a version 4 UUID in lowercase.
func NewForkID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// IsImport reports whether c is an import commit: one with no parent whose
// message carries the four import trailers. A commit with a parent is never
// one, whatever its message quotes: every import commit is a root, so a
// user's commit that happens to end in the four lines is not taken for a
// base version.
func IsImport(c Commit) bool {
	if len(c.Parents) > 0 {
		return false
	}
	_, err := Parse(c.Message)
	return err == nil
}
