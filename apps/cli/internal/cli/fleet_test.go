package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
)

// TestCloudFleet holds how the cloud machine's snapshot reads as a machine
// of the fleet: its id and label are cloud whoever saved the snapshot, its
// configurations are the snapshot's and every one a skill is placed in,
// each reading the library when its client does on the cloud machine, and
// its library is the snapshot's skills, with a logical asset wherever the
// entry names one, the same a computer reports, and a base version only
// for an exact entry.
func TestCloudFleet(t *testing.T) {
	t.Parallel()
	const hub, shared = "https://example.com/hub.git", "https://github.com/example/skills"
	fork, pdf := scan.SkillLogicalID("fork-1", "", nil, ""), scan.SkillLogicalID("", shared, ptr("skills/pdf"), "")
	off := false
	for _, tc := range []struct {
		name string
		snap snapshot
		want string
	}{
		{"a snapshot with nothing", snapshot{Settings: home.DefaultSettings()},
			`{"machine":{"id":"cloud","label":"cloud"},"commit":"","changed_at":"","configurations":[],"skills":[],"library":[],"plugins":[]}`},
		{"a snapshot skill place wrote", snapshot{
			Settings: home.Settings{DisabledConfigurations: []string{"cursor"}},
			Skills: []snapshotSkill{
				{Name: "pdf", Kind: lineage.KindManaged, Source: shared, Subpath: ptr("skills/pdf"), Placements: []string{"cursor", "claude-code"}},
				{Name: "notes", Kind: lineage.KindManaged, ForkID: "fork-1", Source: hub, Placements: []string{"not-a-client"}},
			}},
			`{"machine":{"id":"cloud","label":"cloud"},"commit":"","changed_at":"",` +
				`"configurations":[{"id":"claude-code","name":"Claude Code","enabled":true,"reads_library":false},` +
				`{"id":"cursor","name":"Cursor","enabled":false,"reads_library":true},` +
				`{"id":"not-a-client","name":"not-a-client","enabled":true,"reads_library":false}],"skills":[],"library":[` +
				`{"name":"notes","kind":"managed","logical_id":"` + fork + `","fork_id":"fork-1","source":"` + hub + `","placements":[{"configuration":"not-a-client"}],"universal":["cursor"]},` +
				`{"name":"pdf","kind":"managed","logical_id":"` + pdf + `","source":"` + shared + `","subpath":"skills/pdf",` +
				`"placements":[{"configuration":"claude-code"},{"configuration":"cursor"}],"universal":["cursor"]}],"plugins":[]}`},
		{"a snapshot a computer saved", snapshot{
			Machine:  &exportMachine{ID: "0123456789abcdef0123456789abcdef", Label: "studio"},
			Settings: home.DefaultSettings(),
			Configurations: []snapshotConfiguration{
				{ID: "claude-code", Client: "claude-code", Name: "Claude Code", Enabled: true, ReadsLibrary: true},
				{ID: "codex", Client: "codex", Name: "Codex", Enabled: false},
			},
			Skills: []snapshotSkill{
				{Name: "pdf", Kind: lineage.KindManaged, Commit: "c0ffee", Source: shared, Subpath: ptr("skills/pdf"), UpstreamCommit: "f00d", BaseHash: "b0b0", Placements: []string{"claude-code"}},
				{Name: "notes", Kind: lineage.KindManaged, ForkID: "fork-1", Source: hub, Upstream: shared, UpstreamSubpath: ptr(""), UpstreamCommit: "f00d", BaseHash: "b0b0", Placements: []string{}},
				{Name: "scratch", Kind: lineage.KindUnmanaged, Placements: []string{}},
			},
			Plugins: []snapshotPlugin{{Name: "design-kit", Marketplace: "example", Version: "1.2.0", Configuration: "claude-code", Enabled: &off}}},
			`{"machine":{"id":"cloud","label":"cloud"},"commit":"","changed_at":"",` +
				`"configurations":[{"id":"claude-code","name":"Claude Code","enabled":true,"reads_library":false},{"id":"codex","name":"Codex","enabled":false,"reads_library":true}],"skills":[],"library":[` +
				`{"name":"notes","kind":"managed","logical_id":"` + fork + `","fork_id":"fork-1","source":"` + hub + `","upstream":"` + shared + `","upstream_subpath":"","placements":[],"universal":["codex"]},` +
				`{"name":"pdf","kind":"managed","logical_id":"` + pdf + `","source":"` + shared + `","subpath":"skills/pdf","upstream_commit":"f00d","base_hash":"b0b0",` +
				`"placements":[{"configuration":"claude-code"}],"universal":["codex"]},` +
				`{"name":"scratch","kind":"unmanaged","placements":[],"universal":["codex"]}],` +
				`"plugins":[{"name":"design-kit","marketplace":"example","version":"1.2.0","configuration":"claude-code","enabled":false}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			equal(t, "the machine", asJSON(t, cloudFleet(tc.snap)), tc.want)
		})
	}
}

// TestMachineNamed holds what the argument of machine show and machine
// remove names: the cloud machine by its id alone, a computer by its id,
// the start of its id or its label, and nothing when it names several,
// exit code 6, or none, exit code 5.
func TestMachineNamed(t *testing.T) {
	t.Parallel()
	machine := func(id, label string) fleetMachine {
		return fleetMachine{Machine: machineFacts{ID: id, Label: label}}
	}
	const studio, twin, bench, cee = "0123456789abcdef0123456789abcdef", "01ffffffffffffffffffffffffffffff", "89abcdef0123456789abcdef01234567", "c10dc10dc10dc10dc10dc10dc10dc10d"
	machines := []fleetMachine{machine(studio, "studio"), machine(twin, "studio"), machine(bench, "workbench"), machine(cee, "89"), machine(cloudMachine, cloudMachine)}
	for _, tc := range []struct {
		name     string
		arg      string
		machines []fleetMachine
		want     string // the id of the machine named
		exit     int
		says     string
	}{
		{"the cloud machine", "cloud", machines, cloudMachine, 0, ""},
		{"a whole id", twin, machines, twin, 0, ""},
		{"the start of one id", "012", machines, studio, 0, ""},
		{"a label", "workbench", machines, bench, 0, ""},
		{"the start of an id that is the cloud machine's too", "c", machines, cee, 0, ""},
		{"a label two machines have", "studio", machines, "", 6,
			"studio names more than one machine: studio (" + studio + "), studio (" + twin + ")"},
		{"the start of two ids", "01", machines, "", 6, "01 names more than one machine"},
		{"the start of one id and the label of another", "89", machines, "", 6, "89 names more than one machine: workbench (" + bench + "), 89 (" + cee + ")"},
		{"no machine", "laptop", machines, "", 5, "the hub holds no report of a machine called laptop"},
		{"the cloud machine when the hub holds no snapshot of it", "cloud", machines[:4], "", 5, "the hub holds no machines/cloud"},
		{"no name", "", machines, "", 1, "no machine given"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m, err := machineNamed(tc.arg, tc.machines)
			if tc.exit == 0 {
				if err != nil {
					t.Fatalf("machineNamed(%q) = %v", tc.arg, err)
				}
				equal(t, "the machine", m.Machine.ID, tc.want)
				return
			}
			var f *failure
			if !errors.As(err, &f) {
				t.Fatalf("machineNamed(%q) = %v, %v, want a refusal", tc.arg, m.Machine.ID, err)
			}
			equal(t, "the exit code", f.status.exit, tc.exit)
			contains(t, "the refusal", f.message, tc.says)
		})
	}
}

// TestShownSkills holds where machine show says each skill a configuration
// sees comes from: the library, for a skill placed there and for every
// library skill of a universal client, a plugin, or the agent client's own
// skills directory. A placement of a library skill is the library skill's
// row and no row of its own, a copy edited since included.
func TestShownSkills(t *testing.T) {
	t.Parallel()
	computer := reportContents{
		Skills: []reportSkill{
			{Name: "pdf", ContentHash: "p1", Occurrences: []reportOccurrence{{Configuration: "claude-code", Kind: scan.KindSymlink}, {Configuration: "codex", Kind: scan.KindDirectory}}},
			{Name: "notes", ContentHash: "n1", Occurrences: []reportOccurrence{{Configuration: "codex", Kind: scan.KindDirectory}}},
			{Name: "notes", ContentHash: "n2", Occurrences: []reportOccurrence{{Configuration: "windsurf", Kind: scan.KindCopy}, {Configuration: "claude-code", Kind: scan.KindDirectory}}},
			{Name: "lint", ContentHash: "l1", Occurrences: []reportOccurrence{
				{Configuration: "claude-code", Kind: scan.KindDirectory, Plugin: "design-kit"}, {Configuration: "claude-code", Kind: scan.KindDirectory, Plugin: "audit"}}},
			{Name: "own", ContentHash: "o1", Occurrences: []reportOccurrence{{Configuration: "claude-code", Kind: scan.KindDirectory}}},
		},
		Library: []reportLibrarySkill{
			{Name: "notes", ContentHash: "n1", Placements: []reportPlacement{{Configuration: "windsurf", Mode: modeCopy, Kind: scan.KindCopy}, {Configuration: "codex", Mode: modeLibrary, Kind: modeLibrary}}, Universal: []string{"codex"}},
			{Name: "pdf", ContentHash: "p1", Placements: []reportPlacement{{Configuration: "claude-code", Mode: modeSymlink, Kind: scan.KindSymlink}, {Configuration: "codex", Mode: modeLibrary, Kind: modeLibrary}}, Universal: []string{"codex"}},
		},
	}
	cloud := reportContents{Library: []reportLibrarySkill{
		{Name: "notes", Placements: []reportPlacement{{Configuration: "claude-code"}}, Universal: []string{"codex"}},
		{Name: "pdf", Placements: []reportPlacement{}, Universal: []string{"codex"}},
	}}
	for _, tc := range []struct {
		name          string
		held          reportContents
		configuration string
		want          string
	}{
		{"a library skill, two plugins' skill, a skill of its own and one named as a library skill it does not see", computer, "claude-code",
			"pdf library, lint plugin audit, lint plugin design-kit, notes skills directory, own skills directory"},
		{"a universal client", computer, "codex", "notes library, pdf library"},
		{"a copy edited since it was placed", computer, "windsurf", "notes library"},
		{"a configuration that sees nothing", computer, "cursor", ""},
		{"the cloud machine, a placement", cloud, "claude-code", "notes library"},
		{"the cloud machine, a universal client", cloud, "codex", "notes library, pdf library"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var got []string
			for _, s := range shownSkills(tc.held, tc.configuration) {
				got = append(got, strings.TrimSpace(s.name+" "+s.from+" "+s.plugin))
			}
			equal(t, "the skills", strings.Join(got, ", "), tc.want)
		})
	}
}
