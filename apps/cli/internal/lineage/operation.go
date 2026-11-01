package lineage

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"strings"
	"time"
)

// The trailers of a commit on a machine's branch, machines/<machine id>
// on the account remote, which name the operation the commit made: so that
// the branch's history is a log of changes a reader can type, not only a
// list of snapshots to diff, and every change has an id of its own, the
// shape the operations of the account service take.
const (
	// TrailerOperationID is the operation's id, a ULID the writer makes.
	TrailerOperationID = "Agentx-Operation-ID"
	// TrailerOperation is the kind of operation: export, place or remove.
	TrailerOperation = "Agentx-Operation"
	// TrailerSkill is the skill a place or remove is about.
	TrailerSkill = "Agentx-Skill"
	// TrailerConfigurations are the configuration ids the operation was
	// asked for, "<id>, <id>", sorted and each once: the ones a place was
	// to place the skill in, --to or every enabled one, or a remove was to
	// take it out of. They are the operation's arguments, which a replay
	// of it needs, not the entries it changed: a configuration that held
	// the skill already is still named.
	TrailerConfigurations = "Agentx-Configurations"
	// TrailerCopy is "true" on a place that recorded a copy in copy_mode.
	TrailerCopy = "Agentx-Copy"
)

// The kinds of operation a commit on a machine's branch makes.
const (
	OperationExport = "export"
	OperationPlace  = "place"
	OperationRemove = "remove"
)

// OperationTrailers are the trailers of one commit on a machine's branch:
// the operation it made, and the machine that made it. "" and nil stand
// for a trailer the commit does not carry.
type OperationTrailers struct {
	ID             string   // the operation's id, a ULID
	Operation      string   // export, place or remove
	Skill          string   // the skill, for place and remove
	Configurations []string // the configurations the operation was asked for; none for export and for a remove of the whole skill
	Copy           bool     // a place that recorded a copy
	Machine        string   // the machine that wrote the commit
}

// ErrOperationTrailer is the error of a message whose operation trailers
// agentx cannot read.
var ErrOperationTrailer = errors.New("unreadable operation trailers")

var (
	operationID   = regexp.MustCompile(`^[0-7][0-9A-HJKMNP-TV-Z]{25}$`)
	operationKind = regexp.MustCompile(`^(export|place|remove)$`)
	// skillName is a value with no whitespace and no control character,
	// which every skill name is, and what a trailer hands back as written.
	skillName = regexp.MustCompile(`^[^\s[:cntrl:]]+$`)
	// configurationID is the shape of a configuration id, see the CLI's
	// configurationID.
	configurationID = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
)

// IsOperationID reports whether s has the shape of an operation id, a
// ULID: 26 characters of Crockford base32, the first one 0 to 7.
func IsOperationID(s string) bool { return operationID.MatchString(s) }

// NewOperationID is a new operation id, a ULID: the time now in
// milliseconds in its first 48 bits and 80 random bits after them, as
// 26 characters of Crockford base32, so that ids sort by the time they
// were made and two made in one millisecond still differ.
func NewOperationID() string {
	var b [16]byte
	ms := uint64(time.Now().UnixMilli())
	for i := range 6 {
		b[i] = byte(ms >> (40 - 8*i))
	}
	_, _ = rand.Read(b[6:])
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	n := new(big.Int).SetBytes(b[:])
	out := make([]byte, 26)
	thirtyTwo, digit := big.NewInt(32), new(big.Int)
	for i := 25; i >= 0; i-- {
		n.DivMod(n, thirtyTwo, digit)
		out[i] = alphabet[digit.Int64()]
	}
	return string(out)
}

// ParseOperation reads the operation trailers of a commit message, from
// its trailer block alone, as ParseFork reads a fork commit's. One of
// them given twice, or with a value of the wrong shape, is an error, since
// a reader that picked one would be guessing; a message with none of them
// is an operation of no kind, which is what a commit an earlier agentx
// wrote on a machine's branch reads as. Every other Agentx- trailer is
// left to its own reader.
func ParseOperation(message string) (OperationTrailers, error) {
	var t OperationTrailers
	seen := map[string]bool{}
	for _, line := range strings.Split(trailerBlock(message), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		switch key {
		case TrailerOperationID, TrailerOperation, TrailerSkill, TrailerConfigurations, TrailerCopy, TrailerMachine:
		default:
			continue
		}
		if seen[key] {
			return OperationTrailers{}, fmt.Errorf("%w: the trailer %s is given twice", ErrOperationTrailer, key)
		}
		seen[key] = true
		bad := fmt.Errorf("%w: %s %q is not what agentx writes there", ErrOperationTrailer, key, value)
		switch key {
		case TrailerOperationID:
			if !operationID.MatchString(value) {
				return OperationTrailers{}, bad
			}
			t.ID = value
		case TrailerOperation:
			if !operationKind.MatchString(value) {
				return OperationTrailers{}, bad
			}
			t.Operation = value
		case TrailerSkill:
			if !skillName.MatchString(value) {
				return OperationTrailers{}, bad
			}
			t.Skill = value
		case TrailerConfigurations:
			ids := strings.Split(value, ", ")
			for _, id := range ids {
				if !configurationID.MatchString(id) {
					return OperationTrailers{}, bad
				}
			}
			if !slices.IsSorted(ids) || len(slices.Compact(slices.Clone(ids))) != len(ids) {
				return OperationTrailers{}, bad
			}
			t.Configurations = ids
		case TrailerCopy:
			if value != "true" {
				return OperationTrailers{}, bad
			}
			t.Copy = true
		case TrailerMachine:
			if !machineID.MatchString(value) {
				return OperationTrailers{}, bad
			}
			t.Machine = value
		}
	}
	return t, nil
}

// OperationMessage is the message of a commit agentx writes on a machine's
// branch: the subject and the trailers it carries, in the order
// Operation-ID, Operation, Skill, Configurations, Copy, Machine, each left
// out when t does not set it. A message ParseOperation would not read back
// as t is refused, as ForkMessage refuses one, so that the two sides cannot
// drift apart.
func OperationMessage(subject string, t OperationTrailers) (string, error) {
	var trailers []string
	add := func(key, value string) {
		if value != "" {
			trailers = append(trailers, key+": "+value)
		}
	}
	add(TrailerOperationID, t.ID)
	add(TrailerOperation, t.Operation)
	add(TrailerSkill, t.Skill)
	add(TrailerConfigurations, strings.Join(t.Configurations, ", "))
	if t.Copy {
		add(TrailerCopy, "true")
	}
	add(TrailerMachine, t.Machine)
	message := strings.TrimSpace(subject)
	if len(trailers) > 0 {
		message += "\n\n" + strings.Join(trailers, "\n")
	}
	message += "\n"
	got, err := ParseOperation(message)
	if err != nil {
		return "", err
	}
	if !got.equal(t) {
		return "", fmt.Errorf("%w: the message would be read back as %+v, not %+v", ErrOperationTrailer, got, t)
	}
	return message, nil
}

// equal reports whether t and u name the same operation, nil and no
// configurations being the same.
func (t OperationTrailers) equal(u OperationTrailers) bool {
	return t.ID == u.ID && t.Operation == u.Operation && t.Skill == u.Skill &&
		slices.Equal(t.Configurations, u.Configurations) && t.Copy == u.Copy && t.Machine == u.Machine
}
