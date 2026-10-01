package cli

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// forkName is the Agent Skills name grammar, which every name agentx gives
// a fork or a greenfield skill follows: lowercase letters, digits and
// hyphens, with no hyphen at the start or the end and no two in a row. It
// is a strict subset of the names git accepts for a branch, and two names it
// accepts never differ only by case, which on a file system that ignores
// case would make git's loose refs merge one branch into the other.
var forkName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// forkNameLimit is the longest name the grammar allows.
const forkNameLimit = 64

// forkNameRule is the grammar as a refusal states it.
const forkNameRule = "use 1 to 64 lowercase letters, digits and hyphens, with no hyphen at the start or end and no two in a row"

// forkNameRefusal is "" for a name agentx can give a fork, and otherwise
// the refusal of the name, the rule it breaks included.
func forkNameRefusal(name string) string {
	if len(name) > forkNameLimit || !forkName.MatchString(name) {
		return sanitised(name) + " is not a valid skill name: " + forkNameRule
	}
	return ""
}

// nameTaken finds the branch of either namespace whose name is name, or
// differs from it only by case, other than the one called except, which is
// the source's own branch of a fork, "" for none. The comparison ignores
// case because the account repo may sit on a file system that does, where
// two such branches would be one.
func nameTaken(records map[string]lineage.Record, name, except string) (lineage.Record, bool) {
	names := make([]string, 0, len(records))
	for n := range records {
		names = append(names, n)
	}
	sort.Strings(names) // the same answer every time, should two differ by case from name
	for _, n := range names {
		if n != except && strings.EqualFold(n, name) {
			return records[n], true
		}
	}
	return lineage.Record{}, false
}

// checkForkName refuses, with exit code 6, a name agentx cannot give a new
// fork or greenfield skill: one outside the grammar, one git would not take
// for a branch, which the grammar already rules out and git is asked about
// all the same, and one the account repo already holds a branch of, in
// either namespace and whatever its case. records are the account repo's
// branches; except is the source's own, see nameTaken; hint is what the
// refusal of a taken name says to do.
func (inv *invocation) checkForkName(ctx context.Context, records map[string]lineage.Record, name, except, hint string) error {
	if refusal := forkNameRefusal(name); refusal != "" {
		return fail(exitRefused, refusal, "choose a name such as my-skill")
	}
	if err := inv.git.CheckBranchName(ctx, strings.TrimPrefix(lineage.ForkRef(name), "refs/heads/")); err != nil {
		return fail(exitRefused, name+" is not a name git accepts for a branch: "+err.Error(), "choose a name such as my-skill")
	}
	return takenRefusal(records, name, except, hint)
}

// takenRefusal is the refusal of a name the account repo holds a branch of,
// nil when it holds none. It is read again under the lock, where nothing but
// the branches can have changed.
func takenRefusal(records map[string]lineage.Record, name, except, hint string) error {
	rec, taken := nameTaken(records, name, except)
	if !taken {
		return nil
	}
	what := "fork"
	if rec.Kind == lineage.KindManaged {
		what = "managed skill"
	}
	message := "the account repo already holds " + sanitised(rec.Name) + " as a " + what
	if rec.Name != name {
		message += ", which differs from " + name + " only by case"
	}
	return fail(exitRefused, message, hint)
}
