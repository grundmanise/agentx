package cli

import (
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// TestSkillOrigin is what every event says of a skill's origin: its kind
// is managed or unmanaged, a skill of a shared source is published to the
// source its import names, and your own skills, the fork branches, to the
// account remote, at no subpath and with no source while none is set,
// carrying the upstream a forked one came from.
func TestSkillOrigin(t *testing.T) {
	t.Parallel()
	const account, shared, upstream = "https://github.com/me/skills", "https://github.com/example/skills", "https://github.com/example/tools"
	imported := lineage.Import{Source: shared, Path: "skills/alpha", Commit: "c1", Hash: "h1"}
	based := &lineage.ForkLineage{ID: "id", Base: "b1", Import: lineage.Import{Source: upstream, Path: "", Commit: "c2", Hash: "h2"}}
	str := func(p *string) string {
		if p == nil {
			return "<absent>"
		}
		return *p
	}
	for _, c := range []struct {
		name    string
		rec     lineage.Record
		account string
		want    [5]string // kind, source, subpath, upstream, upstream subpath
	}{
		{"unmanaged", lineage.Record{}, account, [5]string{"unmanaged", "", "<absent>", "", "<absent>"}},
		{"a shared source's", lineage.Record{Kind: lineage.KindManaged, Import: imported, HasImport: true}, account,
			[5]string{"managed", shared, "skills/alpha", "", "<absent>"}},
		{"an import branch with no lineage", lineage.Record{Kind: lineage.KindManaged}, account, [5]string{"managed", "", "<absent>", "", "<absent>"}},
		{"forked, its upstream at its root", lineage.Record{Kind: lineage.KindFork, Fork: based}, account,
			[5]string{"managed", account, "<absent>", upstream, ""}},
		{"forked, no account remote", lineage.Record{Kind: lineage.KindFork, Fork: based}, "",
			[5]string{"managed", "", "<absent>", upstream, ""}},
		{"made by skill new", lineage.Record{Kind: lineage.KindFork, Fork: &lineage.ForkLineage{ID: "id", NoUpstream: true}}, account,
			[5]string{"managed", account, "<absent>", "", "<absent>"}},
		{"the walk left unread", lineage.Record{Kind: lineage.KindFork, Import: imported, HasImport: true, Fork: &lineage.ForkLineage{}}, account,
			[5]string{"managed", account, "<absent>", "", "<absent>"}},
		{"a fork's base version", lineage.Record{Kind: lineage.KindFork, Import: imported, HasImport: true}, account,
			[5]string{"managed", account, "<absent>", shared, "skills/alpha"}},
		{"forked, no lineage read and no import", lineage.Record{Kind: lineage.KindFork}, "", [5]string{"managed", "", "<absent>", "", "<absent>"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			kind, source, subpath, up, upSubpath := skillOrigin(c.rec, c.account)
			got := [5]string{kind, source, str(subpath), up, str(upSubpath)}
			equal(t, "origin", got, c.want)
		})
	}
}
