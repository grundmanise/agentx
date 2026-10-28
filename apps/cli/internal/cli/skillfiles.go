package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
)

func newSkillFilesCommand(inv *invocation) *cobra.Command {
	var configuration string
	cmd := &cobra.Command{
		Use:   "files <name>",
		Short: "Read every file of a skill, with its content",
		Long: "Read every file of a skill as it is on this machine, one event or one row per\n" +
			"file sorted by path, with its size, whether it is binary, how many lines it has\n" +
			"and, for a text file within " + skillFileLimitText + ", its content. A binary file and a\n" +
			"file over the limit are described, never dumped. Nothing is written and nothing\n" +
			"reaches the network.\n\n" +
			"<name> is a skill of the library, or a skill a plugin provides, named as\n" +
			"'agentx skill fork' names it, <plugin>:<skill> when several plugins provide one\n" +
			"of that name. A file git ignores in a library skill's directory is marked\n" +
			"ignored, as 'agentx skill diff' leaves it out. With --configuration <id>, read\n" +
			"instead the skill directory called <name> in that configuration's own skills\n" +
			"directory, a plugin's left out.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("configuration") && strings.TrimSpace(configuration) == "" {
				return fail(exitUsage, "--configuration needs a configuration", "name one by its id, such as claude-code; 'agentx scan' lists them")
			}
			return inv.skillFiles(cmd.Context(), args[0], configuration)
		},
	}
	cmd.Flags().StringVar(&configuration, "configuration", "", "read the skill in this configuration's own skills directory")
	return cmd
}

// skillFileEvent is one file of a skill as skill files reads it. Content
// is carried for a text file within skillFileLimit and left out otherwise;
// lines is counted for a text file whose content is carried. Both are
// pointers so that an empty text file, 0 lines and "" content, is carried
// and not left out as a zero value would be.
type skillFileEvent struct {
	event
	Name      string  `json:"name"`
	Path      string  `json:"path"`
	Size      int64   `json:"size"`
	Binary    bool    `json:"binary"`
	Lines     *int    `json:"lines,omitempty"`
	Content   *string `json:"content,omitempty"`
	Truncated bool    `json:"truncated,omitempty"`
	Ignored   bool    `json:"ignored,omitempty"`
}

// skillFileLimit is the size over which a file's content is not carried:
// 256 KiB, past what a file an agent reads as instructions is. A reader
// that wants more opens the file itself.
const skillFileLimit = 256 << 10

const skillFileLimitText = "256 KiB"

// binaryProbe is how far into a file git looks for a NUL byte to call it
// binary: the first 8000 bytes.
const binaryProbe = 8000

// skillFile is one file of a skill before it becomes an event or a row.
type skillFile struct {
	path      string // relative to the skill directory, / as the separator
	size      int64
	binary    bool
	truncated bool
	lines     int
	content   string
	ignored   bool
}

// fileFacts is what skill files says of a file from its bytes: whether it
// is binary, by git's rule, a NUL byte within the first binaryProbe bytes;
// whether its content is left out, being over skillFileLimit; and, for a
// text file whose content is carried, how many lines it has, the number of
// newlines plus one for a last line that ends without one, so that an
// empty file has none and "a" and "a\n" have one each. data is the whole
// file when size is within the limit, and its first bytes otherwise. Pure.
func fileFacts(data []byte, size int64) (binary, truncated bool, lines int) {
	binary = bytes.IndexByte(data[:min(len(data), binaryProbe)], 0) >= 0
	if size > skillFileLimit || int64(len(data)) > skillFileLimit {
		return binary, true, 0
	}
	if binary {
		return true, false, 0
	}
	lines = bytes.Count(data, []byte{'\n'})
	if len(data) > 0 && data[len(data)-1] != '\n' {
		lines++
	}
	return false, false, lines
}

// skillFiles reads every file of the skill arg names and reports them, see
// newSkillFilesCommand. The library is looked in first, then the plugins
// the scan found, as skill fork looks; with configuration, that
// configuration's own skills directory alone.
//
// A library skill is read under the shared lock, as a scan reads, so that
// a mutation in progress is waited for and no file is read from half-
// applied state; the lineage is read under the same hold, and decides, as
// skill diff decides, which files git ignores: the base version of a
// managed skill, or the tip of a fork, is loaded into a throwaway index,
// so a file that version holds counts whatever an ignore rule says. A
// plugin's or a configuration's skill is found by the scan, which takes
// the lock itself, and read where its occurrence leads.
func (inv *invocation) skillFiles(ctx context.Context, arg, configuration string) error {
	if configuration != "" {
		if err := inv.detectedConfiguration(configuration); err != nil {
			return err
		}
		inventory, err := inv.scan(ctx, lockWait, "", false)
		if err != nil {
			return err
		}
		o, err := configurationSkill(inventory, configuration, arg)
		if err != nil {
			return err
		}
		files, err := inv.readSkillDir(o.ResolvedPath, nil)
		if err != nil {
			return err
		}
		inv.reportSkillFiles(arg, sanitised(arg)+" in "+configuration, o.ResolvedPath, files)
		return nil
	}
	var files []skillFile
	var dir string
	held := false
	err := inv.readLocked(ctx, lockWait, func() error {
		lib, ok := librarySkill(inv.dirs.Library, arg)
		if !ok {
			return nil
		}
		held = true
		ignored, err := inv.libraryIgnored(ctx, arg, lib)
		if err != nil {
			return err
		}
		dir = lib.ResolvedPath
		files, err = inv.readSkillDir(dir, ignored)
		return err
	})
	if err != nil {
		return err
	}
	if held {
		inv.reportSkillFiles(arg, sanitised(arg), dir, files)
		return nil
	}
	inventory, err := inv.scan(ctx, lockWait, "", false)
	if err != nil {
		return err
	}
	o, err := pluginOccurrence(inventory, arg)
	if err != nil {
		return err
	}
	name := filepath.Base(o.Path)
	if files, err = inv.readSkillDir(o.ResolvedPath, nil); err != nil {
		return err
	}
	inv.reportSkillFiles(name, sanitised(name)+" of plugin "+sanitised(o.Plugin), o.ResolvedPath, files)
	return nil
}

// readLocked runs read, a command's local reads, under the shared lock of
// agentx home as a scan runs its own, waiting for a mutation in progress
// for wait. An unfinished mutation journal found under the shared lock is
// recovered under the exclusive one and the reads start over, so nothing
// is read from half-applied state.
func (inv *invocation) readLocked(ctx context.Context, wait time.Duration, read func() error) error {
	lockCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	for {
		var journals []string
		err := home.ReadLocked(lockCtx, inv.dirs.Home, func() error {
			var err error
			if journals, err = home.Journals(inv.dirs.Home); err != nil || len(journals) > 0 {
				return err
			}
			return read()
		})
		if err != nil {
			return err
		}
		if len(journals) == 0 {
			return nil
		}
		inv.out.debugf("recovering %s", strings.Join(journals, ", "))
		if err := home.Recover(lockCtx, inv.dirs.Home, inv.refs(ctx)); err != nil {
			return err
		}
	}
}

// configurationSkill is the skill directory called name in the own skills
// directory of the configuration id, among what the scan found: a
// user-scope occurrence of that configuration that is no plugin's, the
// first by path when it has several. A name it holds none of is exit code
// 5. Pure.
func configurationSkill(inventory scan.Inventory, id, name string) (scan.Occurrence, error) {
	var found []scan.Occurrence
	for _, node := range inventory.Skills {
		for _, o := range node.Occurrences {
			if o.Configuration == id && o.Scope == scan.ScopeUser && o.Plugin == "" && filepath.Base(o.Path) == name {
				found = append(found, o)
			}
		}
	}
	if len(found) == 0 {
		return scan.Occurrence{}, fail(exitNotFound, fmt.Sprintf("%s holds no skill called %q", id, sanitised(name)),
			"run 'agentx scan' to see the skills of each configuration")
	}
	sort.Slice(found, func(i, j int) bool { return found[i].Path < found[j].Path })
	return found[0], nil
}

// libraryIgnored is the set of files in the directory of the library skill
// called name that git ignores, relative to it with / as the separator, as
// skill diff leaves them out: listed by git over a throwaway index loaded
// with the version the skill is compared with, the base version of a
// managed skill or the tip of a fork, so a file that version holds is
// never ignored; an unmanaged skill's over an empty index. A fork's
// directory is read in its worktree, so a .gitignore at the worktree's
// root counts. Nothing is written to the library. While no account repo
// exists there is no repository to run git over, and nothing is ignored.
func (inv *invocation) libraryIgnored(ctx context.Context, name string, lib scan.LibrarySkill) (map[string]bool, error) {
	gitDir, rec, held, err := inv.accountRecord(ctx, name)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(gitDir); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	top, load := lib.ResolvedPath, ""
	switch {
	case held && rec.Kind == lineage.KindFork:
		if _, ok := inv.placedForkDir(name, lib.ResolvedPath); ok {
			top, load = inv.worktreeRoot(name), rec.Commit
		}
	case held && rec.HasImport:
		load = baseVersion(rec).load
	}
	wt, err := inv.openWorkTreeWithin(ctx, gitDir, top, lib.ResolvedPath)
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	defer wt.Close()
	if load != "" {
		if err := wt.Load(ctx, load); err != nil {
			return nil, accountRepoFailure(err)
		}
	}
	paths, err := wt.Ignored(ctx)
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	ignored := make(map[string]bool, len(paths))
	for _, p := range paths {
		ignored[strings.TrimSuffix(p, "/")] = true
	}
	return ignored, nil
}

// readSkillDir reads every file under dir, a skill directory: each regular
// file, and each symlink that leads to one, which is what an agent reading
// the skill reads through it. Anything else is left out with a warning
// naming it: a .git entry, which git cannot record and no agent reads as
// part of the skill; a symlink that leads to a directory or nowhere, which
// is not followed; a named pipe or a socket. Files are sorted by path, and
// ignored marks the paths among them, relative to dir with / as the
// separator. A directory this machine cannot read is refused as a library
// it cannot use is.
func (inv *invocation) readSkillDir(dir string, ignored map[string]bool) ([]skillFile, error) {
	var files []skillFile
	leftOut := func(full, why string) {
		inv.out.warn(quotedPath(full) + " " + why + " and is left out")
	}
	err := filepath.WalkDir(dir, func(full string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if full == dir {
			return nil
		}
		if strings.EqualFold(d.Name(), ".git") {
			leftOut(full, "cannot be recorded by git")
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := os.Stat(full) // through a symlink
		switch {
		case err != nil:
			if d.Type()&fs.ModeSymlink != 0 {
				leftOut(full, "is a symlink that leads nowhere")
				return nil
			}
			return err
		case info.IsDir():
			leftOut(full, "is a symlink to a directory")
			return nil
		case !info.Mode().IsRegular():
			leftOut(full, "is neither a file nor a symlink to one")
			return nil
		}
		f, err := readSkillFile(full, info.Size())
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, full)
		if err != nil {
			return err
		}
		f.path = filepath.ToSlash(rel)
		f.ignored = ignored[f.path]
		files = append(files, f)
		return nil
	})
	if err != nil {
		return nil, libraryFailure(dir, err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	return files, nil
}

// readSkillFile reads the file at full, whose size a stat gave, as far as
// fileFacts needs: whole when it is within skillFileLimit, its first
// binaryProbe bytes otherwise, so a file of any size costs one small read.
// A file that grew past the limit between the stat and the read is over it
// all the same.
func readSkillFile(full string, size int64) (skillFile, error) {
	var data []byte
	var err error
	if size > skillFileLimit {
		data, err = readHead(full, binaryProbe)
	} else {
		data, err = os.ReadFile(full)
	}
	if err != nil {
		return skillFile{}, err
	}
	f := skillFile{size: size}
	f.binary, f.truncated, f.lines = fileFacts(data, size)
	if !f.truncated {
		f.size = int64(len(data))
	}
	if !f.binary && !f.truncated {
		f.content = string(data)
	}
	return f, nil
}

// readHead reads the first n bytes of the file at full, fewer when it is
// shorter.
func readHead(full string, n int) ([]byte, error) {
	r, err := os.Open(full)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	buf := make([]byte, n)
	read, err := io.ReadFull(r, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buf[:read], nil
}

// reportSkillFiles emits one skill_file event per file of the skill called
// name, and prints one row per file under a line that says what was read
// and from where, subject being the skill as the line names it. The rows
// hold the path, the size, what the file is, binary, over the limit or so
// many lines, and whether git ignores it; the content is for the events.
func (inv *invocation) reportSkillFiles(name, subject, dir string, files []skillFile) {
	out := inv.out
	for _, f := range files {
		ev := skillFileEvent{event: newEvent("skill_file"), Name: name, Path: f.path, Size: f.size, Binary: f.binary, Truncated: f.truncated, Ignored: f.ignored}
		if !f.binary && !f.truncated {
			lines, content := f.lines, f.content
			ev.Lines, ev.Content = &lines, &content
		}
		out.emit(ev)
	}
	inv.summary = subject + " has " + plural(len(files), "file")
	out.print(out.paint(heading, subject), " has ", out.paint(noteStyle, plural(len(files), "file")), " in ", quotedPath(dir))
	t := &table{}
	for _, f := range files {
		what := plural(f.lines, "line")
		switch {
		case f.binary:
			what = "binary"
		case f.truncated:
			what = "over " + skillFileLimitText
		}
		flag := ""
		if f.ignored {
			flag = "ignored"
		}
		t.add(c(quotedPath(f.path), plain), c(plural(int(f.size), "byte"), muted), c(what, muted), c(flag, muted))
	}
	out.render(t, "  ")
}
