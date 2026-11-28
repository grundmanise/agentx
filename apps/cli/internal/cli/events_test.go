package cli

import (
	"bytes"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// TestSkillOrigin is what every event says of a skill's origin: its kind
// is managed or unmanaged, a skill of a shared source is published to the
// source its import names, and hub skills, the fork branches, to the
// hub, at no subpath and with no source while none is set,
// carrying the upstream a forked one came from.
func TestSkillOrigin(t *testing.T) {
	t.Parallel()
	const hub, shared, upstream = "https://github.com/me/skills", "https://github.com/example/skills", "https://github.com/example/tools"
	imported := lineage.Import{Source: shared, Path: "skills/alpha", Commit: "c1", Hash: "h1"}
	based := &lineage.ForkLineage{ID: "id", Base: "b1", Import: lineage.Import{Source: upstream, Path: "", Commit: "c2", Hash: "h2"}}
	str := func(p *string) string {
		if p == nil {
			return "<absent>"
		}
		return *p
	}
	for _, c := range []struct {
		name string
		rec  lineage.Record
		hub  string
		want [5]string // kind, source, subpath, upstream, upstream subpath
	}{
		{"unmanaged", lineage.Record{}, hub, [5]string{"unmanaged", "", "<absent>", "", "<absent>"}},
		{"a shared source's", lineage.Record{Kind: lineage.KindManaged, Import: imported, HasImport: true}, hub,
			[5]string{"managed", shared, "skills/alpha", "", "<absent>"}},
		{"an import branch with no lineage", lineage.Record{Kind: lineage.KindManaged}, hub, [5]string{"managed", "", "<absent>", "", "<absent>"}},
		{"forked, its upstream at its root", lineage.Record{Kind: lineage.KindFork, Fork: based}, hub,
			[5]string{"managed", hub, "<absent>", upstream, ""}},
		{"forked, no hub", lineage.Record{Kind: lineage.KindFork, Fork: based}, "",
			[5]string{"managed", "", "<absent>", upstream, ""}},
		{"made by skill new", lineage.Record{Kind: lineage.KindFork, Fork: &lineage.ForkLineage{ID: "id", NoUpstream: true}}, hub,
			[5]string{"managed", hub, "<absent>", "", "<absent>"}},
		{"the walk left unread", lineage.Record{Kind: lineage.KindFork, Import: imported, HasImport: true, Fork: &lineage.ForkLineage{}}, hub,
			[5]string{"managed", hub, "<absent>", "", "<absent>"}},
		{"a fork's base version", lineage.Record{Kind: lineage.KindFork, Import: imported, HasImport: true}, hub,
			[5]string{"managed", hub, "<absent>", shared, "skills/alpha"}},
		{"forked, no lineage read and no import", lineage.Record{Kind: lineage.KindFork}, "", [5]string{"managed", "", "<absent>", "", "<absent>"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			kind, source, subpath, up, upSubpath := skillOrigin(c.rec, c.hub)
			got := [5]string{kind, source, str(subpath), up, str(upSubpath)}
			equal(t, "origin", got, c.want)
		})
	}
}

// TestWriterAbout: with --json a warning written while the writer is about
// a skill names it, and one written after names none; a skill a run over
// several gives up on is an error event naming it, and a warning naming it
// in the text.
func TestWriterAbout(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	w := &writer{stdout: &stdout, stderr: &stderr, env: map[string]string{}, json: true}
	restore := w.about("pdf")
	w.warn("kept the copy")
	restore()
	w.warnWith("could not fetch", "try again")
	w.skillFailed("docx", refuse(exitRefused, "docx changed", "run it again"))
	equal(t, "stderr", stderr.String(),
		`{"type":"log","schema_version":6,"level":"warn","message":"kept the copy","skill":"pdf"}`+"\n"+
			`{"type":"log","schema_version":6,"level":"warn","message":"could not fetch; try again"}`+"\n")
	equal(t, "stdout", stdout.String(),
		`{"type":"error","schema_version":6,"code":"refused","message":"docx changed","hint":"run it again","skill":"docx"}`+"\n")

	var text bytes.Buffer
	w = &writer{stdout: &text, stderr: &text, env: map[string]string{}, color: "off"}
	w.skillFailed("docx", refuse(exitRefused, "docx changed", "run it again"))
	equal(t, "text", text.String(), "warning: docx changed\n")
}
