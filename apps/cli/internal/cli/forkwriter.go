package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// forkWriter writes every commit agentx makes on a fork's branch: the
// creation commit of a fork or a greenfield skill, an explicit commit of a
// fork's edits, a revert to an earlier commit and a merge. There is no
// other writer, so every such commit carries the same identity, the same
// trailers and the same environment. A commit is written when a command is
// asked to write it, never on a timer, and none is ever amended: a commit
// made with git directly in a fork's worktree is left as it is.
//
// The commit is written in the isolated environment, as every object agentx
// writes is, so nothing of the user's configuration applies to it but the
// identity, which is read from that configuration once per command and
// passed explicitly: no hook runs, nothing signs it, and no attribute or
// setting of theirs changes the tree. The tree is the caller's, built over
// the fork's skill directory by git with the ignore rules applied, see
// judgeFork and judgeTip.
type forkWriter struct {
	git     *gitx.Runner
	gitDir  string
	ident   gitx.Ident
	machine string // the machine id, which every commit carries in its Agentx-Machine trailer
	label   string // the machine label, the author's name when the user set no identity
	env     map[string]string
}

// newForkWriter reads what the commits of one command are written with:
// the user's identity, in the same read of their git configuration that
// fetches their core.excludesFile, which every git over a fork's directory
// is then given, the machine id and the machine label. A user who set no
// identity, or one git could not carry, gets the machine's: its label as
// the name and an address of agentx's own that names the machine and
// reaches nobody.
func (inv *invocation) newForkWriter(ctx context.Context, gitDir string) (*forkWriter, error) {
	config := inv.git.ReadUserConfig(ctx, gitDir)
	inv.excludes.Do(func() { inv.excludesPath = config.ExcludesFile })
	machine, _, err := home.MachineID(inv.dirs.Home, inv.env, inv.refs(ctx))
	if err != nil {
		return nil, err
	}
	s, err := inv.loadSettings()
	if err != nil {
		return nil, err
	}
	w := &forkWriter{git: inv.git, gitDir: gitDir, ident: config.Ident.Sanitised(), machine: machine, label: inv.label(s), env: inv.env}
	if w.ident.Name == "" || w.ident.Email == "" {
		w.ident = fallbackIdent(w.label, machine)
	}
	return w, nil
}

// fallbackIdent is the identity of a machine whose user set none.
func fallbackIdent(label, machine string) gitx.Ident {
	id := gitx.Ident{Name: label, Email: "machine-" + machine[:min(12, len(machine))] + "@agentx.invalid"}.Sanitised()
	if id.Name == "" {
		id.Name = "agentx"
	}
	return id
}

// forkMessage is what one commit says: its subject, an optional body and
// the trailers of its kind. The writer adds the machine.
type forkMessage struct {
	subject, body string
	trailers      lineage.ForkTrailers
}

// commit writes one commit of rootTree with parents, dated now in the time
// zone TZ names when it names one, and returns its id. Nothing points at it
// yet: the command's journal moves the branch to it, with the tip it was
// written on as the value the branch must still hold.
func (w *forkWriter) commit(ctx context.Context, rootTree string, parents []string, m forkMessage) (string, error) {
	m.trailers.Machine = w.machine
	message, err := lineage.ForkMessage(m.subject, m.body, m.trailers)
	if err != nil {
		return "", err
	}
	return w.git.CommitTreeAs(ctx, w.gitDir, w.ident, gitDate(time.Now(), w.env["TZ"]), rootTree, parents, message)
}

// gitDate is t as git writes a date, "<epoch> <+hhmm>", with the offset of
// the time zone tz names, or of the local one when it names none git or Go
// can read.
func gitDate(t time.Time, tz string) string {
	if tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			t = t.In(loc)
		}
	}
	return fmt.Sprintf("%d %s", t.Unix(), t.Format("-0700"))
}

// creationSubject is the subject of a greenfield skill's creation commit.
func creationSubject(name string) string { return "Create " + name }

// mergeMessage is the message of a merge commit on a fork's branch: subject
// and the trailers of a merge, base, the import commit the merge records as
// the fork's base, when it records one, and the machine. A merge that
// conflicts is left pending with it as its MERGE_MSG, and the commit that
// completes it carries it as it is, see commitText.
func (w *forkWriter) mergeMessage(subject, base string) (string, error) {
	return lineage.ForkMessage(subject, "", lineage.ForkTrailers{Base: base, Machine: w.machine})
}

// commitText is commit for a message written already, mergeMessage's as a
// pending merge holds it: the commit that completes the merge is written
// with the writer's identity and the message the merge was started with,
// the same commit git commit in the checkout would write with that
// identity, with no hook, editor or clean-up of the message.
func (w *forkWriter) commitText(ctx context.Context, rootTree string, parents []string, message string) (string, error) {
	return w.git.CommitTreeAs(ctx, w.gitDir, w.ident, gitDate(time.Now(), w.env["TZ"]), rootTree, parents, message)
}

// upstreamMergeSubject is the subject of the commit that merges an update
// into a fork: the skill, the upstream commit of the version merged and
// the label of the machine it was merged on, as in "pdf: merge upstream
// 1a2b3c4 (laptop)".
func upstreamMergeSubject(name, label, upstream string) string {
	return name + ": merge upstream " + short(upstream) + " (" + sanitised(label) + ")"
}
