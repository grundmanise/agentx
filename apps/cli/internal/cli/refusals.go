package cli

import (
	"fmt"
	"strings"
)

// brokenSkill is one skill of a run over several that could not be done,
// and why.
type brokenSkill struct {
	subject string
	fail    *failure
}

// refusals collects the skills a run over several could not do and answers
// for all of them at once. One skill that failed in a run that did nothing
// else answers exactly as that one skill on its own would, which is what a
// run of one skill is; anything else names every skill that failed and
// takes the exit code they agree on, exit code 6 standing for a run whose
// causes disagree, the way exit code 3 does for a fetch of several sources.
//
// Installing and adopting both answer this way, and differ only in the word
// for what did not happen and in the hint a run of mixed causes ends with.
type refusals struct {
	broken []brokenSkill
	verb   string // what the skills could not be: installed, adopted
	mixed  string // the hint of a run whose causes disagree
}

// add records one skill the run gave up on.
func (r *refusals) add(subject string, f *failure) {
	r.broken = append(r.broken, brokenSkill{subject: subject, fail: f})
}

// failure is how the run answers: selected is how many skills it set out to
// do and done how many it did.
func (r *refusals) failure(selected, done int) *failure {
	switch {
	case len(r.broken) == 0:
		return nil
	case len(r.broken) == 1 && done == 0:
		return r.broken[0].fail
	}
	status, hint := r.broken[0].fail.status, r.broken[0].fail.hint
	reasons := make([]string, 0, len(r.broken))
	for _, s := range r.broken {
		if s.fail.status != status {
			status = exitRefused
		}
		if s.fail.hint != hint {
			hint = r.mixed
		}
		reasons = append(reasons, s.subject+": "+s.fail.message)
	}
	return &failure{
		status:  status,
		message: fmt.Sprintf("%d of %s could not be %s: %s", len(r.broken), plural(selected, "skill"), r.verb, strings.Join(reasons, "; ")),
		hint:    hint,
	}
}
