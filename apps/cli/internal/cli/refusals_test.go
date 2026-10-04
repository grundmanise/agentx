package cli

import "testing"

// TestRefusalsFailure is how a run over several skills answers for the
// ones it gave up on. None is no failure. One, in a run that did nothing
// else, is that skill's own refusal, as a run of that name alone would
// give it. Anything else names every skill it gave up on, in the order it
// gave up on them, out of how many it set out to do, with the code and the
// hint they agree on, exit code 6 standing for codes that disagree and the
// run's own hint for hints that do. A skill skipped rather than given up
// on is no refusal, see updateRun.drop, and is not counted here.
func TestRefusalsFailure(t *testing.T) {
	t.Parallel()
	const mixed = "run 'agentx skill list', then update the rest one at a time"
	gone := refuse(exitRefused, "the library holds no skill directory for it", "install it again")
	nested := refuse(exitRefused, "it holds a repository git cannot record", "move it out")
	unreadable := refuse(exitAccountRepo, "its candidate cannot be read", "install it again")
	pending := refuse(exitPendingMerge, "it conflicts with its update in 1 file", "resolve it with git")
	type skill struct {
		name string
		fail *failure
	}
	for _, c := range []struct {
		name           string
		broken         []skill
		selected, done int
		want           *failure // nil for none; the very failure for one given back as it is
		message, hint  string
		status         status
	}{
		{name: "none", selected: 2, done: 2},
		{name: "the one skill of a run that did nothing else", broken: []skill{{"delta", gone}}, selected: 1, want: gone},
		{name: "one skill of several, none done", broken: []skill{{"delta", pending}}, selected: 3, want: pending},
		{
			name: "one skill beside one done", broken: []skill{{"delta", gone}}, selected: 2, done: 1,
			status: exitRefused, message: "1 of 2 skills could not be updated: delta: the library holds no skill directory for it", hint: "install it again",
		},
		{
			name: "two whose codes agree and whose hints disagree", broken: []skill{{"delta", gone}, {"gamma", nested}}, selected: 3, done: 1,
			status:  exitRefused,
			message: "2 of 3 skills could not be updated: delta: the library holds no skill directory for it; gamma: it holds a repository git cannot record",
			hint:    mixed,
		},
		{
			name: "two whose codes disagree and whose hints agree", broken: []skill{{"beta", unreadable}, {"delta", gone}}, selected: 2,
			status:  exitRefused,
			message: "2 of 2 skills could not be updated: beta: its candidate cannot be read; delta: the library holds no skill directory for it",
			hint:    "install it again",
		},
		{
			name: "two conflicts", broken: []skill{{"alpha", pending}, {"beta", pending}}, selected: 4, done: 2,
			status:  exitPendingMerge,
			message: "2 of 4 skills could not be updated: alpha: it conflicts with its update in 1 file; beta: it conflicts with its update in 1 file",
			hint:    "resolve it with git",
		},
	} {
		r := refusals{verb: "updated", mixed: mixed}
		for _, s := range c.broken {
			r.add(s.name, s.fail)
		}
		got := r.failure(c.selected, c.done)
		switch {
		case len(c.broken) == 0:
			if got != nil {
				t.Errorf("%s: %v, want no failure", c.name, got)
			}
			continue
		case c.want != nil:
			if got != c.want {
				t.Errorf("%s: %v, want the skill's own refusal %v", c.name, got, c.want)
			}
			continue
		case got == nil:
			t.Errorf("%s: no failure", c.name)
			continue
		}
		equal(t, c.name+": code", got.status, c.status)
		equal(t, c.name+": message", got.message, c.message)
		equal(t, c.name+": hint", got.hint, c.hint)
	}
}

// TestNamedReason: a reason in a run over several names its skill once,
// with a prefix only when the message does not already start with it.
func TestNamedReason(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ subject, message, want string }{
		{"beta", "it conflicts with its update in 1 file", "beta: it conflicts with its update in 1 file"},
		{"beta", "beta conflicts with the account remote in 1 file", "beta conflicts with the account remote in 1 file"},
		{"gone", "gone's worktree /w/gone is missing", "gone's worktree /w/gone is missing"},
		{"beta", "betamax holds a file", "beta: betamax holds a file"},
		{"beta", "the library already holds beta", "beta: the library already holds beta"},
	} {
		equal(t, c.subject+" "+c.message, namedReason(c.subject, c.message), c.want)
	}
}
