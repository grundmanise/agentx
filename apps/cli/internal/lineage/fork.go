package lineage

import (
	"crypto/rand"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/weburl"
)

// The trailers of the commits agentx writes on a fork's branch, beside the
// four of an import commit, which a fork's history holds as its base
// versions. None of them ever enters an import commit.
const (
	// TrailerBase names, on every merge commit agentx writes on a fork's
	// branch, the import commit that is the fork's base after the merge. A
	// skill made by skill new has none to name.
	TrailerBase = "Agentx-Base"
	// TrailerForkID is the permanent id of a fork, a new UUID on the commit
	// that creates the fork. That commit is never amended, so the id never
	// changes; a fork installed elsewhere keeps it, a new fork gets its own.
	TrailerForkID = "Agentx-Fork-ID"
	// TrailerMachine is the id of the machine that wrote the commit, on
	// every commit agentx writes on a fork's branch. A commit without it was
	// made with git directly.
	TrailerMachine = "Agentx-Machine"
	// TrailerOriginSkill is the name of the skill a fork was made from, on
	// the creation commit skill fork writes of a plugin's skill, of an
	// unmanaged skill of the library or of another fork: the plugin's
	// skill directory, or the library's or the fork's name. A fork of a
	// managed skill records none, since the import commit its creation
	// commit has as its parent says where it came from.
	TrailerOriginSkill = "Agentx-Origin-Skill"
	// TrailerOriginPlugin is the plugin that provides the skill a fork was
	// made from, beside TrailerOriginSkill.
	TrailerOriginPlugin = "Agentx-Origin-Plugin"
	// TrailerOriginMarketplace is where that plugin was installed from,
	// beside TrailerOriginPlugin, when the client records it.
	TrailerOriginMarketplace = "Agentx-Origin-Marketplace"
	// TrailerOriginURL is an http or https URL where the skill a fork was
	// made from can be seen, beside TrailerOriginSkill, when it is known.
	TrailerOriginURL = "Agentx-Origin-URL"
)

// Origin is where a fork made of a skill with no import commit came from,
// as its creation commit records it, "" for each it does not record.
type Origin struct {
	Skill       string // the skill's name
	Plugin      string // the plugin that provides it
	Marketplace string // where the plugin was installed from
	URL         string // where it can be seen on the web
}

// IsOriginName reports whether s is a name an origin trailer can carry:
// 1 to 255 characters, none of them whitespace or a control character,
// which is every name agentx gives or reads of a skill, a plugin or a
// marketplace. A name of another shape is left out of the trailers.
func IsOriginName(s string) bool { return originName.MatchString(s) }

// ForkTrailers are the trailers of one commit on a fork's branch, "" for
// each it does not carry.
type ForkTrailers struct {
	Base    string // the base import commit a merge records
	ForkID  string // the fork's id, on its creation commit
	Origin  Origin // where the fork came from, on its creation commit
	Machine string // the machine that wrote the commit
}

// ErrForkTrailer is the error of a message whose fork trailers agentx
// cannot read.
var ErrForkTrailer = errors.New("unreadable fork trailers")

var (
	forkID    = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	machineID = regexp.MustCompile(`^[0-9a-f]{32}$`)
	// originName is a name of 1 to 255 characters with no whitespace and
	// no control character.
	originName = regexp.MustCompile(`^[^\s[:cntrl:]]{1,255}$`)
	// trailerLine is a line git reads as a trailer, "<token>: <value>".
	trailerLine = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*:[ \t]`)
)

// IsForkID reports whether s has the shape of a fork id, which is what a
// reader outside this package checks one it was handed with.
func IsForkID(s string) bool { return forkID.MatchString(s) }

// ParseFork reads the fork trailers of a commit message, from its trailer
// block alone, as Parse reads an import commit's: a user's message that
// quotes a trailer in its body is not lineage. One of them given twice, or
// with a value of the wrong shape, is an error, since a reader that picked
// one would be guessing, and so are origin trailers agentx would not write
// together: any of them without a fork id or without the origin's skill,
// and a marketplace without its plugin. Every other Agentx- trailer is
// left to its own reader: an import commit's four, and whatever a later
// agentx adds.
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
		var shape func(string) bool
		switch key {
		case TrailerBase:
			field, shape = &t.Base, objectID.MatchString
		case TrailerForkID:
			field, shape = &t.ForkID, forkID.MatchString
		case TrailerMachine:
			field, shape = &t.Machine, machineID.MatchString
		case TrailerOriginSkill:
			field, shape = &t.Origin.Skill, IsOriginName
		case TrailerOriginPlugin:
			field, shape = &t.Origin.Plugin, IsOriginName
		case TrailerOriginMarketplace:
			field, shape = &t.Origin.Marketplace, IsOriginName
		case TrailerOriginURL:
			field, shape = &t.Origin.URL, weburl.Is
		default:
			continue
		}
		if seen[key] {
			return ForkTrailers{}, fmt.Errorf("%w: the trailer %s is given twice", ErrForkTrailer, key)
		}
		seen[key] = true
		if !shape(value) {
			return ForkTrailers{}, fmt.Errorf("%w: %s %q is not what agentx writes there", ErrForkTrailer, key, value)
		}
		*field = value
	}
	switch o := t.Origin; {
	case o == Origin{}:
	case t.ForkID == "":
		return ForkTrailers{}, fmt.Errorf("%w: an origin is given without %s", ErrForkTrailer, TrailerForkID)
	case o.Skill == "":
		return ForkTrailers{}, fmt.Errorf("%w: an origin is given without %s", ErrForkTrailer, TrailerOriginSkill)
	case o.Marketplace != "" && o.Plugin == "":
		return ForkTrailers{}, fmt.Errorf("%w: %s is given without %s", ErrForkTrailer, TrailerOriginMarketplace, TrailerOriginPlugin)
	}
	return t, nil
}

// ForkMessage is the message of a commit agentx writes on a fork's branch:
// the subject, the body when there is one, and the trailers it carries in
// the order Base, Fork-ID, Origin-Skill, Origin-Plugin,
// Origin-Marketplace, Origin-URL, Machine. A body that ends in trailers of its
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
	for _, kv := range [][2]string{
		{TrailerBase, t.Base}, {TrailerForkID, t.ForkID},
		{TrailerOriginSkill, t.Origin.Skill}, {TrailerOriginPlugin, t.Origin.Plugin},
		{TrailerOriginMarketplace, t.Origin.Marketplace}, {TrailerOriginURL, t.Origin.URL},
		{TrailerMachine, t.Machine},
	} {
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
