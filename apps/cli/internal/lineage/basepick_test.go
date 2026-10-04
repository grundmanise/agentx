package lineage

import "testing"

func TestChooseBase(t *testing.T) {
	t.Parallel()
	at := func(base, src, path, upstream string) ForkLineage {
		return ForkLineage{Base: base, Import: Import{Source: src, Path: path, Commit: upstream}}
	}
	const src = "https://github.com/example/skills"
	v1, v2 := at("b1", src, "pdf", "u1"), at("b2", src, "pdf", "u2")
	for _, tc := range []struct {
		name          string
		local, remote ForkLineage
		remoteNewer   bool
		want          string
	}{
		{"the same import on both sides", v1, v1, true, "b1"},
		{"made by skill new on both sides", ForkLineage{NoUpstream: true}, ForkLineage{NoUpstream: true}, false, ""},
		{"the remote's version proved newer", v1, v2, true, "b2"},
		{"the local version newer or not proved", v1, v2, false, "b1"},
		{"another directory of the source", v1, at("b2", src, "docx", "u2"), true, "b1"},
		{"another source", v1, at("b2", "https://github.com/other/skills", "pdf", "u2"), true, "b1"},
		{"made by skill new here, a base there", ForkLineage{NoUpstream: true}, v2, true, ""},
		{"a base here, none readable there", v1, ForkLineage{Problem: "unreadable"}, true, "b1"},
		{"one upstream commit, two imports", v1, at("b2", src, "pdf", "u1"), true, "b1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := chooseBase(tc.local, tc.remote, tc.remoteNewer); got != tc.want {
				t.Errorf("chooseBase = %q, want %q", got, tc.want)
			}
		})
	}
}
