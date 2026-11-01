package lineage

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const anOperationID = "01JBXK8Q5D4N7V2M9R3T6W8Y0Z"

func TestParseOperationAndOperationMessage(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		message string
		want    OperationTrailers
		err     bool
	}{
		{"a place", "Place pdf in cursor\n\nAgentx-Operation-ID: " + anOperationID + "\nAgentx-Operation: place\nAgentx-Skill: pdf\nAgentx-Configurations: claude-code, cursor\nAgentx-Copy: true\nAgentx-Machine: " + aMachine + "\n",
			OperationTrailers{ID: anOperationID, Operation: "place", Skill: "pdf", Configurations: []string{"claude-code", "cursor"}, Copy: true, Machine: aMachine}, false},
		{"a remove of the whole skill", "Remove pdf\n\nAgentx-Operation-ID: " + anOperationID + "\nAgentx-Operation: remove\nAgentx-Skill: pdf\n",
			OperationTrailers{ID: anOperationID, Operation: "remove", Skill: "pdf"}, false},
		{"an earlier agentx's commit, with the machine alone", "Export laptop\n\nAgentx-Machine: " + aMachine + "\n", OperationTrailers{Machine: aMachine}, false},
		{"one paragraph has no trailers", "Agentx-Operation: export\n", OperationTrailers{}, false},
		{"a fork's trailers and unknown ones are ignored", "s\n\nAgentx-Fork-ID: " + aForkID + "\nAgentx-Later: x\n", OperationTrailers{}, false},
		{"given twice", "s\n\nAgentx-Operation: place\nAgentx-Operation: remove\n", OperationTrailers{}, true},
		{"an id that is no ULID", "s\n\nAgentx-Operation-ID: 1234\n", OperationTrailers{}, true},
		{"a lowercase id", "s\n\nAgentx-Operation-ID: " + strings.ToLower(anOperationID) + "\n", OperationTrailers{}, true},
		{"a kind agentx does not write", "s\n\nAgentx-Operation: install\n", OperationTrailers{}, true},
		{"a skill with a space", "s\n\nAgentx-Skill: my skill\n", OperationTrailers{}, true},
		{"configurations out of order", "s\n\nAgentx-Configurations: cursor, claude-code\n", OperationTrailers{}, true},
		{"a configuration twice", "s\n\nAgentx-Configurations: cursor, cursor\n", OperationTrailers{}, true},
		{"configurations with no space after the comma", "s\n\nAgentx-Configurations: claude-code,cursor\n", OperationTrailers{}, true},
		{"a copy that is not true", "s\n\nAgentx-Copy: yes\n", OperationTrailers{}, true},
		{"a machine of the wrong length", "s\n\nAgentx-Machine: abc\n", OperationTrailers{}, true},
	} {
		got, err := ParseOperation(tc.message)
		if (err != nil) != tc.err || !got.equal(tc.want) {
			t.Errorf("%s: ParseOperation = %+v, %v; want %+v, error %v", tc.name, got, err, tc.want, tc.err)
		}
		if err != nil && !errors.Is(err, ErrOperationTrailer) {
			t.Errorf("%s: the error %v is not ErrOperationTrailer", tc.name, err)
		}
	}

	for _, tc := range []struct {
		name, subject string
		t             OperationTrailers
		err           bool
	}{
		{"an export", "Export laptop", OperationTrailers{ID: anOperationID, Operation: OperationExport, Machine: aMachine}, false},
		{"a place of a copy", "Place pdf in cursor", OperationTrailers{ID: anOperationID, Operation: OperationPlace, Skill: "pdf", Configurations: []string{"cursor"}, Copy: true, Machine: aMachine}, false},
		{"a remove from configurations", "Remove pdf from cursor", OperationTrailers{ID: anOperationID, Operation: OperationRemove, Skill: "pdf", Configurations: []string{"claude-code", "cursor"}, Machine: aMachine}, false},
		{"a value git would not read back", "s", OperationTrailers{Skill: "my skill"}, true},
		{"configurations out of order", "s", OperationTrailers{Configurations: []string{"cursor", "claude-code"}}, true},
	} {
		m, err := OperationMessage(tc.subject, tc.t)
		if (err != nil) != tc.err {
			t.Errorf("%s: OperationMessage = %q, %v; want error %v", tc.name, m, err, tc.err)
			continue
		}
		if err != nil {
			continue
		}
		if got, err := ParseOperation(m); err != nil || !got.equal(tc.t) || !strings.HasPrefix(m, tc.subject+"\n") {
			t.Errorf("%s: %q reads back as %+v, %v", tc.name, m, got, err)
		}
	}
	m, _ := OperationMessage("Place pdf in cursor", OperationTrailers{ID: anOperationID, Operation: OperationPlace, Skill: "pdf", Configurations: []string{"cursor"}, Copy: true, Machine: aMachine})
	if want := "Place pdf in cursor\n\nAgentx-Operation-ID: " + anOperationID + "\nAgentx-Operation: place\nAgentx-Skill: pdf\nAgentx-Configurations: cursor\nAgentx-Copy: true\nAgentx-Machine: " + aMachine + "\n"; m != want {
		t.Errorf("a place message reads %q, want %q", m, want)
	}
}

func TestNewOperationIDIsAULIDThatSortsByTime(t *testing.T) {
	t.Parallel()
	before := time.Now().UnixMilli()
	a := NewOperationID()
	time.Sleep(2 * time.Millisecond)
	b := NewOperationID()
	if !IsOperationID(a) || !IsOperationID(b) {
		t.Errorf("NewOperationID = %q, %q, not ULIDs", a, b)
	}
	if a >= b {
		t.Errorf("an id made later, %s, does not sort after %s", b, a)
	}
	// The first ten characters encode the time in milliseconds.
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	var ms int64
	for _, c := range a[:10] {
		ms = ms*32 + int64(strings.IndexRune(alphabet, c))
	}
	if ms < before || ms > time.Now().UnixMilli() {
		t.Errorf("%s encodes the time %d, which is not between %d and now", a, ms, before)
	}
}
