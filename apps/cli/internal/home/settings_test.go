package home

import (
	"errors"
	"os"
	"testing"
)

// TestSourceMerge: an add decides the URL, the pin and when it fetched, and
// keeps whatever else the entry knew and it did not find out. The access
// and the time of its check travel as a pair, and so do the layout and the
// account flag.
func TestSourceMerge(t *testing.T) {
	t.Parallel()
	prev := Source{
		URL: "https://example.com/skills", Alias: "https://example.com/old", Layout: LayoutFork, Account: true,
		Pin: "v1", Access: AccessWritable, AccessChecked: "2026-10-01T10:00:00Z", DefaultBranch: "main", LastFetched: "2026-10-01T10:00:00Z",
	}
	added := Source{URL: prev.URL, LastFetched: "2026-10-02T10:00:00Z"}
	for _, tc := range []struct {
		name      string
		add, want Source
	}{
		{"an add that found nothing out keeps everything but the pin and the fetch time", added,
			Source{URL: prev.URL, Alias: prev.Alias, Layout: LayoutFork, Account: true,
				Access: AccessWritable, AccessChecked: prev.AccessChecked, DefaultBranch: "main", LastFetched: added.LastFetched}},
		{"an add's own answers win", Source{URL: prev.URL, Pin: "v2", Access: AccessReadOnly, AccessChecked: "2026-10-02T10:00:00Z", DefaultBranch: "trunk"},
			Source{URL: prev.URL, Alias: prev.Alias, Layout: LayoutFork, Account: true, Pin: "v2",
				Access: AccessReadOnly, AccessChecked: "2026-10-02T10:00:00Z", DefaultBranch: "trunk"}},
		{"a check that could not decide does not bring the old answer back", Source{URL: prev.URL, AccessChecked: "2026-10-02T10:00:00Z"},
			Source{URL: prev.URL, Alias: prev.Alias, Layout: LayoutFork, Account: true,
				AccessChecked: "2026-10-02T10:00:00Z", DefaultBranch: "main"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.add.Merge(prev); got != tc.want {
				t.Errorf("Merge =\n %+v\nwant\n %+v", got, tc.want)
			}
		})
	}
	// The layout and the account flag go together: a fork source that is
	// not the account remote stays one.
	fork := Source{URL: prev.URL, Layout: LayoutFork}
	if got := (Source{URL: prev.URL}).Merge(fork); got.Layout != LayoutFork || got.Account {
		t.Errorf("Merge of a fork source = %+v", got)
	}
}

// TestLoadSettingsRefusesALaterSchema: a file a later agentx wrote is not
// read as this one's, which would misread what changed and lose it on the
// next write.
func TestLoadSettingsRefusesALaterSchema(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(SettingsPath(dir), []byte(`{"schema_version": 2, "sources": []}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadSettings(dir)
	var newer *NewerSettingsError
	if !errors.As(err, &newer) || newer.Version != 2 {
		t.Fatalf("LoadSettings = %v, want a *NewerSettingsError of version 2", err)
	}
}
