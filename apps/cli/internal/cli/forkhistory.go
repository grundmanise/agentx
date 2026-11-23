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
	// Import is true on an import commit: a version of the source's skill
	// agentx stored, which a view of the skill's own edits may leave out.
	Import bool `json:"import,omitempty"`
}

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
	byID := make(map[string]lineage.Commit, len(commits))
	for _, c := range commits {
		byID[c.id] = lineage.Commit{ID: c.id, Parents: c.parents, Message: c.message}
	}
	for _, c := range commits {
		ev := historyEvent{event: newEvent("history"), Name: name, Commit: c.id, Parents: c.parents, Time: c.time, Author: c.author,
			Message: strings.TrimRight(c.message, "\n"), Files: []changedFile{}}
		ev.Subject, _, _ = strings.Cut(ev.Message, "\n")
		imported := lineage.IsImport(lineage.Commit{ID: c.id, Parents: c.parents, Message: c.message})
		ev.Import = imported
		if t, err := lineage.ParseFork(c.message); err == nil && !imported {
			ev.Machine = t.Machine
			var parent *lineage.Commit
			if len(c.parents) > 0 {
				if p, ok := byID[c.parents[0]]; ok {
					parent = &p
				}
			}
			ev.Origin = historyOrigin(t, parent)
		}
		for _, f := range c.files {
			f.Path = historyPath(f.Path, dir)
			ev.Files = append(ev.Files, f)
		}
		out.emit(ev)
		out.print(out.paint(noteStyle, short(c.id)), "  ", historyDate(c.time), "  ", sanitised(ev.Subject),
			"  ", out.paint(muted, plural(len(ev.Files), "file")))
	}
	inv.summary = sanitised(name) + " has " + plural(len(commits), "commit")
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
