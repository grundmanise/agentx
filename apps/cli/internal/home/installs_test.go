package home

import (
	"os"
	"reflect"
	"testing"
	"time"
)

// TestRecordInstalls keeps the last add and the last removal of each
// logical asset: a removal leaves the add as it was, an add again
// overwrites the add and leaves the removal, the name is the last one
// given, an entry that names no logical asset is passed over, and the
// entries are stored sorted, hub skills first. A file that
// cannot be parsed is replaced.
func TestRecordInstalls(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(MutationsDir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installsPath(dir), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	day := func(d int) time.Time { return time.Date(2026, 11, d, 12, 0, 0, 0, time.FixedZone("CET", 3600)) }
	pdf, own := SourceInstall("pdf", "https://example.com/skills", "skills/pdf"), ForkInstall("notes", "f-1")
	record := func(at time.Time, added, removed []Install) {
		t.Helper()
		if err := RecordInstalls(dir, at, added, removed); err != nil {
			t.Fatal(err)
		}
	}
	record(day(1), []Install{pdf, own, {Name: "unmanaged"}}, nil)
	record(day(2), nil, []Install{pdf})
	record(day(3), []Install{SourceInstall("pdf-tools", pdf.Source, *pdf.Subpath)}, nil)

	got, err := LoadInstalls(dir)
	if err != nil {
		t.Fatal(err)
	}
	subpath := "skills/pdf"
	want := []Install{
		{ForkID: "f-1", Name: "notes", Added: "2026-11-01T11:00:00Z"},
		{Source: "https://example.com/skills", Subpath: &subpath, Name: "pdf-tools", Added: "2026-11-03T11:00:00Z", Removed: "2026-11-02T11:00:00Z"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LoadInstalls =\n %+v\nwant\n %+v", got, want)
	}
}
