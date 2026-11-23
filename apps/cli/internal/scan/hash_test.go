package scan

import "testing"

// TestSkillLogicalID holds what makes two library skills one logical
// asset: a hub skill is its fork id, whatever its source and its content;
// a skill of a shared source is its source and its subpath there, the
// root included, whatever its content; and any other skill is its
// content, with the logical id of the skill node of that content.
func TestSkillLogicalID(t *testing.T) {
	t.Parallel()
	const hub, shared = "https://example.com/hub.git", "https://github.com/example/skills"
	root, pdf, xlsx := "", "skills/pdf", "skills/xlsx"
	for _, tc := range []struct {
		name string
		a, b string
		same bool
	}{
		{"one hub skill on two machines, edited on one", SkillLogicalID("fork-1", hub, nil, "h1"), SkillLogicalID("fork-1", "git@example.com:me/hub.git", nil, "h2"), true},
		{"two hub skills with the same content", SkillLogicalID("fork-1", hub, nil, "h1"), SkillLogicalID("fork-2", hub, nil, "h1"), false},
		{"one skill of a shared source in two versions", SkillLogicalID("", shared, &pdf, "h1"), SkillLogicalID("", shared, &pdf, "h2"), true},
		{"two skills of one source", SkillLogicalID("", shared, &pdf, "h1"), SkillLogicalID("", shared, &xlsx, "h1"), false},
		{"the skill at a source's root and one below it", SkillLogicalID("", shared, &root, "h1"), SkillLogicalID("", shared, &pdf, "h1"), false},
		{"the skill at a source's root, in two versions", SkillLogicalID("", shared, &root, "h1"), SkillLogicalID("", shared, &root, "h2"), true},
		{"a hub skill whose fork id is not known is its content", SkillLogicalID("", hub, nil, "h1"), SkillLogicalID("", "", nil, "h1"), true},
		{"an unmanaged skill is the skill node of its content", SkillLogicalID("", "", nil, "h1"), id("skill", "content", "h1"), true},
		{"two unmanaged skills", SkillLogicalID("", "", nil, "h1"), SkillLogicalID("", "", nil, "h2"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if (tc.a == tc.b) != tc.same {
				t.Errorf("the ids are %s and %s, want the same: %v", tc.a, tc.b, tc.same)
			}
		})
	}
}
