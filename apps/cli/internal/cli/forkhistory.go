package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/weburl"
)

func newSkillHistoryCommand(inv *invocation) *cobra.Command {
	return &cobra.Command{
		Use:         "history <name>",
		Short:       "List the commits of a hub skill",
		Annotations: map[string]string{annotationSection: "skills"},
		Long: "List every commit of <name>, a hub skill, newest first: the commits\n" +
			"agentx wrote, those made with git in its worktree, and the upstream imports it\n" +
			"was forked from or merged.\n" +
			"Each names the files it changes in the skill's directory, a merge against its\n" +
			"first parent. Nothing is written.",
		Example: examples(
			"agentx skill history release-notes", "every commit, newest first",
		),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return inv.skillHistory(cmd.Context(), args[0])
		},
	}
}

// historyEvent is one commit of a fork's history.
type historyEvent struct {
	event
	Name    string        `json:"name"`
	Commit  string        `json:"commit"`
	Parents []string      `json:"parents"`
	Time    string        `json:"time"`
	Author  string        `json:"author"`
	Subject string        `json:"subject"`
	Message string        `json:"message"`
	Machine string        `json:"machine,omitempty"`
	Files   []changedFile `json:"files"`
	Origin  *forkOrigin   `json:"origin,omitempty"`
	// Kind is what the commit did to the skill, read off its trailers and
	// parents, see historyKindOf. Generated, MachineLabel, RenamedFrom and
	// Upstream are the facts a reader needs to say it in words.
	Kind         string           `json:"kind"`
	Generated    bool             `json:"generated,omitempty"`
	MachineLabel string           `json:"machine_label,omitempty"`
	RenamedFrom  string           `json:"renamed_from,omitempty"`
	Upstream     *historyUpstream `json:"upstream,omitempty"`
}

// historyUpstream is the version of a source's skill a commit took in: the
// import commit a fork was made from, or the one an update merged. Source
// and Path are its canonical URL and directory, Path "" for the root;
// Commit the upstream commit and Time its committer time, the date the
// import commit carries. URL and SkillURL are as a forkOrigin's: the
// source's web page and the skill directory's page at Commit, when known.
type historyUpstream struct {
	Source   string `json:"source"`
	Path     string `json:"path,omitempty"`
	Commit   string `json:"commit"`
	Time     string `json:"time"`
	URL      string `json:"url,omitempty"`
	SkillURL string `json:"skill_url,omitempty"`
}

// The kinds of a history event: what its commit did to the skill.
const (
	historyUpstreamKind = "upstream" // an import commit: a version of the source's skill agentx stored
	historyFork         = "fork"     // the commit that made the fork of another skill
	historyCreate       = "create"   // the commit skill new wrote
	historyAdopt        = "adopt"    // the commit skill import wrote of a skill on disk
	historyRename       = "rename"   // the commit skill rename wrote
	historyEdit         = "edit"     // edits agentx recorded, on a publish or before a fork, update or rename
	historyUpdate       = "update"   // a merge of a newer version of the source's skill
	historySync         = "sync"     // a merge of the edits another machine published
	historyGit          = "git"      // a commit made with git directly, by the user or an agent
)

// forkOrigin is where a fork came from, on the event of the commit that
// created it: kind "plugin" for a plugin's skill, "source" for a skill of a
// shared source, and "skill" for a skill of the library or another fork.
// Skill is the original skill's name; Plugin and Marketplace name the
// plugin of a plugin's skill, Source and Path the canonical URL and the
// directory of a source's skill, Path "" for the root. URL is where the
// origin can be seen on the web, an http or https URL, when it is known,
// and SkillURL, for a source's skill on a host whose web pages agentx
// knows, the page of the skill's directory at the upstream commit the fork
// was made from.
type forkOrigin struct {
	Kind        string `json:"kind"`
	Skill       string `json:"skill"`
	Plugin      string `json:"plugin,omitempty"`
	Marketplace string `json:"marketplace,omitempty"`
	Source      string `json:"source,omitempty"`
	Path        string `json:"path,omitempty"`
	URL         string `json:"url,omitempty"`
	SkillURL    string `json:"skill_url,omitempty"`
}

// The kinds of a fork's origin.
const (
	originPlugin = "plugin"
	originSource = "source"
	originSkill  = "skill"
)

// historyOrigin is where the fork came from, read off the commit whose
// trailers are t and whose first parent is parent, nil for none: the
// origin the trailers of a creation commit record, of a plugin's skill or
// another skill, else, for a creation commit made from an import commit,
// that import's source and directory, and nil for every other commit and
// a creation commit that records neither, as skill new's and an earlier
// agentx's do. Pure.
func historyOrigin(t lineage.ForkTrailers, parent *lineage.Commit) *forkOrigin {
	switch o := t.Origin; {
	case t.ForkID == "":
		return nil
	case o.Plugin != "":
		return &forkOrigin{Kind: originPlugin, Skill: o.Skill, Plugin: o.Plugin, Marketplace: o.Marketplace, URL: o.URL}
	case o.Skill != "":
		return &forkOrigin{Kind: originSkill, Skill: o.Skill, URL: o.URL}
	case parent == nil || !lineage.IsImport(*parent):
		return nil
	}
	imp, err := lineage.Parse(parent.Message)
	if err != nil {
		return nil
	}
	web := weburl.Of(imp.Source)
	return &forkOrigin{Kind: originSource, Skill: imp.Dir(), Source: imp.Source, Path: imp.Path, URL: web,
		SkillURL: weburl.Tree(web, imp.Commit, imp.Path)}
}

// forkLogCommit is one commit of a fork's branch as git log wrote it.
type forkLogCommit struct {
	id, time, author, message string
	parents                   []string
	files                     []changedFile // against its first parent, every path as the commit's tree holds it
}

// historyFormat is what git log writes of each commit for parseForkLog: a
// byte no field begins with, then the commit, its parents, the author time
// in strict ISO 8601, the author and the raw message, each ending in a NUL.
const historyFormat = "--format=%x01%H%x00%P%x00%aI%x00%an <%ae>%x00%B%x00"

// skillHistory lists the history of the fork called name, newest first, in
// one git log of its branch: every commit reachable from the tip, the
// imports a merge took in as its second parent included, each with the
// files it changes against its first parent, a root commit against
// nothing, so an import lists every file it holds as added. A commit
// with no Agentx-Machine, which every commit agentx writes on a fork's
// branch carries, was made with git directly. Every path is relative to
// the skill's directory. Nothing is
// written, and the fork needs no worktree on this machine.
func (inv *invocation) skillHistory(ctx context.Context, name string) error {
	gitDir, rec, held, err := inv.accountRecord(ctx, name)
	if err != nil {
		return err
	}
	switch _, inLibrary := librarySkill(inv.dirs.Library, name); {
	case held && rec.Kind != lineage.KindFork:
		return fail(exitRefused, sanitised(name)+" is not a skill of the hub, so it has no history of its own",
			"a skill of a shared source is at the version it was installed at or last published; fork it with '"+skillCommand("fork", name)+"' to give its edits a history")
	case !held && inLibrary:
		return fail(exitRefused, sanitised(name)+" is not a skill of the hub, so it has no history",
			"import it with '"+adoptCommand(name)+"' to give its edits a history")
	case !held:
		return inv.noLibrarySkill(name)
	}
	dir, err := inv.forkDir(ctx, gitDir, rec)
	if err != nil {
		return err
	}
	out, err := inv.git.Isolated(ctx, gitDir, "log", "--no-renames", "--diff-merges=first-parent", "--root", "--name-status", "-z", historyFormat, rec.Commit)
	if err != nil {
		return accountRepoFailure(err)
	}
	commits, err := parseForkLog(out)
	if err != nil {
		return accountRepoFailure(err)
	}
	inv.reportHistory(name, dir, commits)
	return nil
}

// parseForkLog reads git log's output in historyFormat with --name-status
// -z: each commit's fields, then, after one more NUL, a status and a path
// for each file it changes, NUL-terminated, the first status after a line
// feed. A commit that changes nothing has no files. A path is read by its
// place after a status, so no byte a path may hold confuses the two.
func parseForkLog(out string) ([]forkLogCommit, error) {
	fields := strings.Split(out, "\x00")
	var commits []forkLogCommit
	for i := 0; i < len(fields); {
		head := strings.TrimLeft(fields[i], "\n")
		if head == "" {
			i++
			continue
		}
		if !strings.HasPrefix(head, "\x01") || i+4 >= len(fields) {
			return nil, fmt.Errorf("git log: cannot read a commit at %q", head)
		}
		c := forkLogCommit{id: head[1:], parents: strings.Fields(fields[i+1]), time: fields[i+2], author: fields[i+3], message: fields[i+4], files: []changedFile{}}
		i += 5
		if i < len(fields) && fields[i] == "" {
			i++
		}
		for i < len(fields) {
			status := strings.TrimLeft(fields[i], "\n")
			if status == "" || strings.HasPrefix(status, "\x01") {
				break
			}
			if i+1 >= len(fields) || fields[i+1] == "" {
				return nil, fmt.Errorf("git log: no path after status %q", status)
			}
			f := changedFile{Path: fields[i+1], Status: diffModified}
			switch status {
			case "A":
				f.Status = diffAdded
			case "D":
				f.Status = diffDeleted
			}
			c.files = append(c.files, f)
			i += 2
		}
		commits = append(commits, c)
	}
	return commits, nil
}

// reportHistory emits one history event per commit of the fork called
// name, whose skill directory is dir, in the order git log listed them,
// the one of the commit that created the fork with where it came from,
// and prints one line each: the short commit, the author date, the
// subject and how many files it changes.
func (inv *invocation) reportHistory(name, dir string, commits []forkLogCommit) {
	out := inv.out
	byID := make(map[string]forkLogCommit, len(commits))
	for _, c := range commits {
		byID[c.id] = c
	}
	for _, c := range commits {
		ev := historyEvent{event: newEvent("history"), Name: name, Commit: c.id, Parents: c.parents, Time: c.time, Author: c.author,
			Message: strings.TrimRight(c.message, "\n"), Files: []changedFile{}, Kind: historyGit}
		ev.Subject, _, _ = strings.Cut(ev.Message, "\n")
		for _, f := range c.files {
			f.Path = historyPath(f.Path, dir)
			ev.Files = append(ev.Files, f)
		}
		parent := func(i int) *forkLogCommit {
			if i < len(c.parents) {
				if p, ok := byID[c.parents[i]]; ok {
					return &p
				}
			}
			return nil
		}
		if c.isImport() {
			ev.Kind = historyUpstreamKind
		} else if t, err := lineage.ParseFork(c.message); err == nil {
			ev.Machine = t.Machine
			var first *lineage.Commit
			if p := parent(0); p != nil {
				first = &lineage.Commit{ID: p.id, Parents: p.parents, Message: p.message}
			}
			ev.Origin = historyOrigin(t, first)
			if t.Machine != "" {
				// A commit with no machine was made with git directly.
				describeHistory(&ev, t, parent(0), parent(1))
			}
		}
		out.emit(ev)
		out.print(out.paint(noteStyle, short(c.id)), "  ", historyDate(c.time), "  ", sanitised(ev.Subject),
			"  ", out.paint(muted, plural(len(ev.Files), "file")))
	}
	inv.summary = sanitised(name) + " has " + plural(len(commits), "commit")
}

// isImport reports whether c is an import commit, see lineage.IsImport.
func (c forkLogCommit) isImport() bool {
	return lineage.IsImport(lineage.Commit{ID: c.id, Parents: c.parents, Message: c.message})
}

// upstreamOf is the version of a source's skill the import commit c
// stores, nil when c is none.
func upstreamOf(c *forkLogCommit) *historyUpstream {
	if c == nil || !c.isImport() {
		return nil
	}
	imp, err := lineage.Parse(c.message)
	if err != nil {
		return nil
	}
	web := weburl.Of(imp.Source)
	return &historyUpstream{Source: imp.Source, Path: imp.Path, Commit: imp.Commit, Time: c.time, URL: web,
		SkillURL: weburl.Tree(web, imp.Commit, imp.Path)}
}

// describeHistory sets the kind of ev, a commit agentx wrote, whose
// trailers are t and whose first and second parents, as the history holds
// them, are first and second, nil for none, and the facts that kind is
// told with. A commit is a rename when its subject is the one skill rename
// writes, then a fork's creation when it carries a fork id: of another
// skill when it records an origin or its subject is skill fork's, else
// skill import's, by its subject, or skill new's. A merge is an update
// when it took in an import commit, and the edits of another machine
// otherwise; every other commit records edits. Generated says the subject
// is the one agentx writes for the kind, which, for edits and merges, also
// names the label of the machine they were made on. Pure.
func describeHistory(ev *historyEvent, t lineage.ForkTrailers, first, second *forkLogCommit) {
	name, label, labelled := subjectLabel(ev.Subject)
	switch {
	case lineage.RenamedFrom(ev.Message) != "":
		ev.Kind, ev.Generated, ev.RenamedFrom = historyRename, true, lineage.RenamedFrom(ev.Message)
	case t.ForkID != "" && (ev.Origin != nil || strings.HasPrefix(ev.Subject, "Fork ")):
		ev.Kind, ev.Generated, ev.Upstream = historyFork, true, upstreamOf(first)
	case t.ForkID != "" && strings.HasPrefix(ev.Subject, "Adopt "):
		ev.Kind, ev.Generated = historyAdopt, true
	case t.ForkID != "":
		ev.Kind, ev.Generated = historyCreate, true
	case second != nil && second.isImport():
		ev.Kind, ev.Upstream = historyUpdate, upstreamOf(second)
		ev.Generated = labelled && upstreamMergeSubject(name, label, ev.Upstream.Commit) == ev.Subject
	case len(ev.Parents) > 1:
		ev.Kind = historySync
		ev.Generated = labelled && accountMergeSubject(name, label) == ev.Subject
	default:
		ev.Kind = historyEdit
		ev.Generated = labelled && commitSubject(name, label, ev.Files) == ev.Subject
	}
	if ev.Generated && labelled && (ev.Kind == historyEdit || ev.Kind == historyUpdate || ev.Kind == historySync) {
		ev.MachineLabel = label
	}
}

// subjectLabel splits a subject agentx writes for edits and merges,
// "<name>: <what> (<label>)", into the name and the label; ok is false
// for a subject of another shape. Pure.
func subjectLabel(subject string) (name, label string, ok bool) {
	name, _, found := strings.Cut(subject, ": ")
	open := strings.LastIndex(subject, " (")
	if !found || open == -1 || !strings.HasSuffix(subject, ")") {
		return "", "", false
	}
	return name, subject[open+len(" (") : len(subject)-1], true
}

// historyPath is the path p of a commit's tree relative to the skill
// directory dir: an entry beside the directory, such as a README.md
// committed with git at the worktree's root, starts with ../, so it is
// never taken for the skill's own file of that name. Pure.
func historyPath(p, dir string) string {
	if rel, ok := strings.CutPrefix(p, dir+"/"); ok {
		return rel
	}
	return "../" + p
}

// historyDate is an author time git wrote in strict ISO 8601 as the text
// output prints it, to the minute in the author's own time zone.
func historyDate(iso string) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return sanitised(iso)
	}
	return t.Format("2006-01-02 15:04")
}
