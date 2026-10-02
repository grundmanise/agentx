package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

func newSkillNewCommand(inv *invocation) *cobra.Command {
	var description string
	cmd := &cobra.Command{
		Use:   "new <name>",
		Short: "Create a skill from the template, with a history of its own",
		Long: "Create a greenfield skill called <name> from agentx's template: a SKILL.md with\n" +
			"the name and the description in its frontmatter. The skill gets its own branch,\n" +
			"skills/<name>, in the account repo, checked out as a worktree in agentx home, and\n" +
			"the library holds a symlink to it. It is placed into every enabled configuration,\n" +
			"as an install places a skill, and listed as a fork with no upstream.\n\n" +
			"The name must be 1 to 64 lowercase letters, digits and hyphens, with no hyphen at\n" +
			"the start or end and no two in a row.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return inv.skillNew(cmd.Context(), args[0], description)
		},
	}
	cmd.Flags().StringVar(&description, "description", "", "what the skill does and when an agent should use it, one line")
	return cmd
}

// descriptionLimit is the longest description the Agent Skills
// frontmatter allows.
const descriptionLimit = 1024

// skillNew creates a greenfield skill: a branch of its own in the account
// repo, whose first commit holds the template SKILL.md under a directory
// named after the skill and carries the skill's new fork id, checked out
// as a worktree in agentx home, with the library's symlink to the skill's
// directory and a placement in every enabled configuration. All of it is
// one journaled mutation; the commit is written before it, and nothing
// points at it until the journal moves the branch there.
func (inv *invocation) skillNew(ctx context.Context, name, description string) error {
	if description == "" {
		description = "Describe what " + name + " does and when an agent should use it."
	} else if len(description) > descriptionLimit || strings.IndexFunc(description, unicode.IsControl) >= 0 {
		return fail(exitUsage, fmt.Sprintf("--description must be one line of at most %d bytes with no control character", descriptionLimit),
			"say in one line what the skill does and when an agent should use it")
	}
	// The name is checked before anything is created, the account repo
	// included, so that a name agentx refuses leaves the machine as it was.
	if refusal := forkNameRefusal(name); refusal != "" {
		return fail(exitRefused, refusal, "choose a name such as my-skill")
	}
	libPath, root := inv.libraryPath(name), inv.worktreeRoot(name)
	if err := inv.newSkillRoom(libPath, root); err != nil {
		return err
	}
	gitDir, _, err := gitx.OpenAccountRepo(ctx, inv.git, inv.dirs.Home)
	if err != nil {
		return accountRepoFailure(err)
	}
	// The account repo's info/exclude is brought in line with
	// ignore_system_files before any fork command, so that git applies the
	// list to the new worktree as the setting says, even in an account repo
	// created before the setting existed.
	if err := home.SyncExclude(gitDir, inv.systemFilesIgnored()); err != nil {
		return accountRepoFailure(err)
	}
	records, err := lineage.List(ctx, inv.git, gitDir)
	if err != nil {
		return accountRepoFailure(err)
	}
	if err := inv.checkForkName(ctx, records, name, "", "choose another name"); err != nil {
		return err
	}
	w, err := inv.newForkWriter(ctx, gitDir)
	if err != nil {
		return err
	}
	tree, err := inv.git.EditTree(ctx, gitDir, "", []gitx.TreeEdit{{Path: name + "/SKILL.md", Mode: treeid.FileMode, Content: skillTemplate(name, description)}})
	if err != nil {
		return accountRepoFailure(err)
	}
	commit, err := w.commit(ctx, tree, nil, forkMessage{subject: creationSubject(name), trailers: lineage.ForkTrailers{ForkID: lineage.NewForkID()}})
	if err != nil {
		return accountRepoFailure(err)
	}
	targets, err := inv.placementTargets(nil)
	if err != nil {
		return err
	}
	var done placements
	err = home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		return inv.createFork(ctx, gitDir, name, name, commit, name, targets, &done)
	})
	if errors.Is(err, home.ErrMovedBeforeApply) {
		return fail(exitRefused, name+" changed while it was being created, so nothing was changed", "run the command again")
	}
	if err != nil {
		return mutationFailure(err)
	}
	return inv.reportCreated(ctx, name, targets, done)
}

// newSkillRoom refuses a name whose library entry or worktree is taken. A
// symlink into the worktrees directory that leads nowhere is a fork's
// placement whose worktree is gone, and the new skill takes its place.
func (inv *invocation) newSkillRoom(libPath, root string) error {
	state, err := home.State(libPath)
	if err != nil {
		return libraryFailure(inv.dirs.Library, err)
	}
	forkLeft := home.IsDangling(libPath) && inv.intoWorktrees(libPath)
	if !home.IsAbsent(state) && !forkLeft {
		return fail(exitRefused, "the library already holds "+quotedPath(libPath),
			"choose another name, or move "+quotedPath(libPath)+" aside")
	}
	if _, err := os.Lstat(root); err == nil {
		return fail(exitRefused, quotedPath(root)+" already exists", "move it aside, or choose another name")
	}
	return nil
}

// createFork records and applies, under the lock, the creation of a fork
// called name whose branch starts at commit: its branch, its worktree, its
// skill directory dir laid out from the commit, the library symlink and the
// placements, a copy wherever copy_mode records one for the skill called
// copiesOf, which is the fork itself for a new skill and the skill it was
// forked from for a fork beside it. Every input is read again first: the
// branches, for a name another command took meanwhile, and the library and
// worktree paths.
func (inv *invocation) createFork(ctx context.Context, gitDir, name, dir, commit, copiesOf string, targets []placeTarget, done *placements) error {
	records, err := lineage.List(ctx, inv.git, gitDir)
	if err != nil {
		return accountRepoFailure(err)
	}
	if err := takenRefusal(records, name, "", "choose another name"); err != nil {
		return err
	}
	libPath, root := inv.libraryPath(name), inv.worktreeRoot(name)
	if err := inv.newSkillRoom(libPath, root); err != nil {
		return err
	}
	libState, err := home.State(libPath)
	if err != nil {
		return libraryFailure(inv.dirs.Library, err)
	}
	for _, parent := range []string{inv.dirs.Library, inv.worktreesDir()} {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return libraryFailure(parent, err)
		}
	}
	// A creation killed before its journal was written left what it staged
	// with nothing to name it, in the worktrees directory, in the library
	// and in each client directory a copy was staged in.
	sweepStaged(inv.worktreesDir())
	sweepStaged(inv.dirs.Library)
	for _, t := range targets {
		if !t.readsLibrary {
			sweepStaged(t.dir)
		}
	}
	link, err := inv.forkLink(libPath, filepath.Join(root, dir))
	if err != nil {
		return libraryFailure(inv.dirs.Library, err)
	}
	edit, err := inv.beginSettings()
	if err != nil {
		return err
	}
	m := home.NewMutation(inv.dirs.Home)
	// The content is staged beside the worktree, in the worktrees directory,
	// never inside it, where git would see it as a file of the branch.
	staged := m.Sibling(root, "staged")
	fingerprint, err := inv.stageForkContent(ctx, gitDir, commit, dir, staged, "", nil)
	if err != nil {
		os.RemoveAll(staged)
		return accountRepoFailure(err)
	}
	m.Ref(gitDir, lineage.ForkRef(name), "", commit)
	m.Worktree(gitDir, root, strings.TrimPrefix(lineage.ForkRef(name), "refs/heads/"))
	m.Publish(filepath.Join(root, dir), staged, fingerprint)
	if !home.IsAbsent(libState) {
		m.Remove(libPath, libState) // a fork's symlink whose worktree is gone
	}
	m.Link(libPath, link)
	hash, _ := scan.ContentHashAt(staged)
	p := placeable{name: name, hash: hash, stage: func(dest string) error { return copyTreeTo(staged, dest) }}
	*done = placements{}
	for _, t := range targets {
		inv.stagePlacement(m, p, t, libPath, false, edit.copiesOf(copiesOf), done)
	}
	edit.addCopies(name, done.copies)
	if err := edit.stage(m, inv.dirs.Home); err != nil {
		m.Discard()
		return err
	}
	return m.Apply(inv.refs(ctx))
}

// forkLink is what the library symlink of a fork whose skill directory is
// target records, see libraryLink: both sides are resolved first, the
// worktree's through agentx home, since neither exists yet.
func (inv *invocation) forkLink(libPath, target string) (string, error) {
	libDir, err := filepath.EvalSymlinks(filepath.Dir(libPath))
	if err != nil {
		return "", err
	}
	homeDir, err := filepath.EvalSymlinks(inv.dirs.Home)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(inv.dirs.Home, target)
	if err != nil {
		return "", err
	}
	return libraryLink(libDir, filepath.Join(homeDir, rel)), nil
}

// reportCreated reads the machine again and reports the new skill as an
// install reports one: the library_skill event of what the rescan found, a
// confirmation and one row per configuration it was placed in.
func (inv *invocation) reportCreated(ctx context.Context, name string, targets []placeTarget, done placements) error {
	snap, err := inv.scan(ctx, lockWait, "", false)
	if err != nil {
		return err
	}
	lib, ok := librarySkill(inv.dirs.Library, name)
	if !ok {
		return fail(exitInternal, "the library holds no "+name+" after creating it", "run 'agentx doctor' and check the library it names")
	}
	sc, err := inv.skillContext(ctx)
	if err != nil {
		return err
	}
	ev := sc.librarySkillEventFor(ctx, inv, snap, lib, targetIDs(done.placed))
	inv.out.emit(ev)
	out := inv.out
	rows := inv.ownPlacements(name, done.placed, ev.Placements)
	line := "created " + out.paint(heading, name) + " in " + quotedPath(inv.libraryPath(name)) + ": " + out.paint(noteStyle, plural(len(rows), "placement"))
	if n := len(done.skipped); n > 0 {
		line += ", " + out.paint(warnStyle, plural(n, "placement")+" skipped")
	}
	out.done(line)
	inv.printPlacementRows(name, rows)
	inv.printUniversal(ev.Universal)
	inv.summary = "created " + name + " in " + plural(len(done.placed), "configuration")
	if n := len(done.skipped); n > 0 {
		inv.summary += ", " + plural(n, "placement") + " skipped"
	}
	inv.summary += universalClause(ev.Universal)
	return nil
}

// plainScalar is a value the template can write as a plain YAML scalar
// and every YAML reader reads back as the same string: it starts with a
// letter, so that no reader takes it for a number or a date, holds none of
// the characters that would start a comment, a mapping or a quoted or
// escaped form, and is not a word YAML reads as something other than a
// string.
var plainScalar = regexp.MustCompile(`^[A-Za-z][^:#"'\\\x60{}\[\]|>&*!%@]*[^:#"'\\\x60{}\[\]|>&*!%@\s]$|^[A-Za-z]$`)

// yamlWords are the plain scalars YAML reads as a boolean or as null.
var yamlWords = map[string]bool{"y": true, "n": true, "yes": true, "no": true, "true": true, "false": true, "on": true, "off": true, "null": true}

// yamlString writes s as a YAML scalar that reads back as the string s: a
// plain scalar when that does, and double-quoted otherwise.
func yamlString(s string) string {
	if plainScalar.MatchString(s) && !yamlWords[strings.ToLower(s)] {
		return s
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// skillTemplate is the SKILL.md a greenfield skill starts with: the Agent
// Skills frontmatter, with the name and the description, and a heading and
// a line that asks for the instructions. Both values are written so that
// they read back as strings, which a name such as null or 123 would not
// as a plain scalar.
func skillTemplate(name, description string) []byte {
	return []byte("---\nname: " + yamlString(name) + "\ndescription: " + yamlString(description) + "\n---\n\n# " + name + "\n\n" +
		"Write here the instructions an agent follows when it uses this skill.\n")
}
