package cli

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// trailer is the value of one trailer of the commit the account repo holds
// at rev.
func (h *harness) trailer(rev, key string) string {
	h.t.Helper()
	return h.accountGit("log", "-1", "--format=%(trailers:key="+key+",valueonly)", rev)
}

// TestSkillNewCreatesAForkFromTheTemplate creates a skill with skill new on
// two machines. Each gets a branch whose one commit holds the template under
// the skill's directory and carries a new fork id and the machine, checked
// out as a locked worktree whose .git file sits outside the skill, a
// relative library symlink to the skill's directory, a symlink in each
// client with a skills directory of its own and none in the clients that
// read the library. The listing calls it a managed skill with no source,
// since no account remote is set, and no upstream, by its fork id, and the
// two machines' ids differ. One machine's user set a git identity, which
// the commit carries; the other's did not, and the machine's label stands
// in.
func TestSkillNewCreatesAForkFromTheTemplate(t *testing.T) {
	t.Parallel()
	a, _ := installHarness(t)
	b, _ := installHarness(t)
	b.withIdentity("Ada Lovelace", "ada@example.com")
	a.env["GIT_CONFIG_NOSYSTEM"] = "1" // no identity of the test machine's own
	ids, machines := map[*harness]string{}, map[*harness]string{}
	for _, h := range []*harness{a, b} {
		out := h.run("--json", "skill", "new", "notes", "--description", "Keep notes for the user.")
		equal(t, "exit", out.exit, 0)
		ev := h.one(out.stdout, "library_skill")
		equal(t, "kind", ev["kind"], lineage.KindManaged)
		for _, key := range []string{"source", "upstream"} {
			if _, ok := ev[key]; ok {
				t.Errorf("a new skill with no account remote set lists a %s: %v", key, ev[key])
			}
		}
		ids[h] = h.trailer("refs/heads/skills/notes", lineage.TrailerForkID)
		equal(t, "the event's fork id", ev["fork_id"], ids[h])

		tip := h.ref(lineage.ForkRef("notes"))
		equal(t, "the branch's history", h.accountGit("rev-list", "--parents", "refs/heads/skills/notes"), tip)
		equal(t, "the branch's tree", h.accountGit("ls-tree", "-r", "--name-only", tip), "notes/SKILL.md")
		equal(t, "SKILL.md", h.accountGit("cat-file", "blob", tip+":notes/SKILL.md")+"\n", string(skillTemplate("notes", "Keep notes for the user.")))
		machine, _, err := home.MachineID(h.agentx, h.env, nil)
		if err != nil {
			t.Fatal(err)
		}
		machines[h] = machine
		equal(t, "the machine trailer", h.trailer(tip, lineage.TrailerMachine), machine)
		equal(t, "the subject", h.accountGit("log", "-1", "--format=%s", tip), "Create notes")

		root := filepath.Join(h.agentx, "worktrees", "notes")
		contains(t, "the worktree list", h.accountGit("worktree", "list", "--porcelain"),
			"worktree "+root+"\nHEAD "+tip+"\nbranch refs/heads/skills/notes\nlocked agentx fork")
		if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
			t.Errorf("the worktree has no .git file: %v", err)
		}
		if _, err := os.Lstat(filepath.Join(root, "notes", ".git")); err == nil {
			t.Error("the skill directory holds a .git entry")
		}
		equal(t, "git status in the worktree", gitIn(t, h, root, "status", "--porcelain", "--ignored"), "")
		// The fixture's library is ~/.agents/skills, which shares the
		// machine's root with agentx home.
		link, err := os.Readlink(filepath.Join(h.library, "notes"))
		if err != nil {
			t.Fatal(err)
		}
		equal(t, "the library symlink", link, "../../../agentx/worktrees/notes/notes")
		for _, client := range []string{".claude", ".cursor"} {
			if target, _ := os.Readlink(filepath.Join(h.home, client, "skills", "notes")); target != filepath.Join(h.library, "notes") {
				t.Errorf("%s's placement points at %q", client, target)
			}
		}
		for _, client := range []string{".codex", ".gemini"} {
			if _, err := os.Lstat(filepath.Join(h.home, client, "skills", "notes")); err == nil {
				t.Errorf("%s, which reads the library, got a second symlink", client)
			}
		}
		equal(t, "the listing's fork id", h.listed("notes")["fork_id"], ids[h])
		equal(t, "what is left beside the worktree", strings.Join(hiddenEntries(t, filepath.Join(h.agentx, "worktrees")), " "), "")
	}
	equal(t, "the fallback author", a.accountGit("log", "-1", "--format=%an <%ae>", "refs/heads/skills/notes"),
		"test-host <machine-"+machines[a][:12]+"@agentx.invalid>")
	equal(t, "the user's author", b.accountGit("log", "-1", "--format=%an <%ae>|%cn <%ce>", "refs/heads/skills/notes"),
		"Ada Lovelace <ada@example.com>|Ada Lovelace <ada@example.com>")
	if ids[a] == ids[b] || !regexp.MustCompile(`^[0-9a-f-]{36}$`).MatchString(ids[a]) {
		t.Errorf("the two machines' fork ids are %q and %q", ids[a], ids[b])
	}
	// A library symlink left into the worktrees directory by a fork whose
	// worktree is gone is replaced.
	if err := os.Symlink(filepath.Join(a.agentx, "worktrees", "other", "other"), filepath.Join(a.library, "other")); err != nil {
		t.Fatal(err)
	}
	text := a.mustRun("skill", "new", "other")
	contains(t, "the text output", text.stdout, "✓ created other in "+filepath.Join(a.library, "other")+": 4 placements\n")
	// With no account remote set the skill is created all the same, with one
	// line saying how to publish it. That skill publish refuses until then is
	// TestTheAccountRemoteIsSetReplacedAndDetached's.
	contains(t, "the hint", text.stdout, "No account remote is set: run 'agentx source add <url> --account' before publishing other.")
	if link, _ := os.Readlink(filepath.Join(a.library, "other")); link != "../../../agentx/worktrees/other/other" {
		t.Errorf("the dangling library symlink was left as %q", link)
	}
}

// TestSkillNewRefusals refuses, in one home and changing nothing, a name
// outside the grammar, a name the account repo holds a branch of in
// another case, a name the library holds a directory of, a name whose
// worktree path is taken, and a description of more than one line.
func TestSkillNewRefusals(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--name", "alpha")
	h.accountGit("update-ref", "refs/heads/managed/Shout", h.ref(lineage.ManagedRef("alpha")))
	writeFile(t, mkdirs(t, filepath.Join(h.library, "taken"), "SKILL.md"), skill("taken", "mine"))
	if err := os.MkdirAll(filepath.Join(h.agentx, "worktrees", "held"), 0o755); err != nil {
		t.Fatal(err)
	}
	refs := h.accountGit("for-each-ref")
	for _, tc := range []struct {
		args []string
		exit status
		says string
	}{
		{[]string{"My_Skill"}, exitRefused, "My_Skill is not a valid skill name: " + forkNameRule},
		{[]string{"shout"}, exitRefused, "the account repo already holds Shout as a skill of a shared source, which differs from shout only by case"},
		{[]string{"taken"}, exitRefused, "the library already holds"},
		{[]string{"held"}, exitRefused, "worktrees/held already exists"},
		{[]string{"fine", "--description", "two\nlines"}, exitUsage, "--description must be one line"},
	} {
		out := h.run(append([]string{"--json", "skill", "new"}, tc.args...)...)
		equal(t, strings.Join(tc.args, " ")+": exit", out.exit, tc.exit.exit)
		contains(t, strings.Join(tc.args, " ")+": error", h.one(out.stdout, "error")["message"].(string), tc.says)
	}
	equal(t, "the refs", h.accountGit("for-each-ref"), refs)
	if entries, _ := os.ReadDir(filepath.Join(h.agentx, "worktrees")); len(entries) != 1 {
		t.Errorf("a refused creation left %d entries in the worktrees directory, want the one it found", len(entries))
	}
}

// newChildEnv marks the process TestSkillNewRecoversWhereItWasKilled
// starts, which creates the skill it names and is killed part way.
const newChildEnv = "AGENTX_TEST_NEW_CHILD"

// TestNewChildProcess is not a test: it is the body of that process. It
// does nothing when the variable that marks it is not set.
func TestNewChildProcess(t *testing.T) {
	name := os.Getenv(newChildEnv)
	if name == "" {
		t.Skip("not the skill new child process")
	}
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	os.Exit(Run(context.Background(), []string{"skill", "new", name}, env, strings.NewReader(""), os.Stdout, os.Stderr))
}

// TestSkillNewRecoversWhereItWasKilled kills a creation with SIGKILL at two
// of its boundaries: once its journal is on disk and before anything was
// applied, and right after git added the worktree. Every boundary between
// is the journal's, which home.TestCreationRecoversFromEveryBoundary replays
// without git. The next command finishes it: the branch, the worktree on
// it with a clean status, the library symlink and the placement, and
// nothing staged left behind.
func TestSkillNewRecoversWhereItWasKilled(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, script string
	}{
		{"once the journal is on disk", `
for f in %MUTATIONS%/*.json; do
	if [ -e "$f" ]; then
		kill -9 $PPID
		exit 1
	fi
done
exec %GIT% "$@"
`},
		{"after the worktree was added", `
case " $* " in
*" worktree add "*)
	%GIT% "$@"
	kill -9 $PPID
	exit 1 ;;
esac
exec %GIT% "$@"
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h, _ := installHarness(t)
			out := killedChild(t, h, "TestNewChildProcess", newChildEnv, "notes", tc.script)
			_, kinds := journalKinds(t, h)
			equal(t, "the journal's steps", kinds, "ref, worktree, publish, link, link, link")
			if got := h.run("config", "set", "label", "recovered"); got.exit != 0 {
				t.Fatalf("the command after the killed creation: exit %d\n%s\nthe killed run:\n%s", got.exit, got.stderr, out)
			}
			equal(t, "journals after recovery", journalCount(t, h), 0)
			root := filepath.Join(h.agentx, "worktrees", "notes")
			if !home.WorktreeAt(root, "skills/notes") {
				t.Error("the worktree is not on the branch after recovery")
			}
			equal(t, "the branch", h.accountGit("ls-tree", "-r", "--name-only", "refs/heads/skills/notes"), "notes/SKILL.md")
			equal(t, "git status in the worktree", gitIn(t, h, root, "status", "--porcelain"), "")
			if b, err := os.ReadFile(filepath.Join(h.library, "notes", "SKILL.md")); err != nil || !strings.Contains(string(b), "name: notes") {
				t.Errorf("the library symlink leads to %q, %v", b, err)
			}
			if target, _ := os.Readlink(filepath.Join(h.home, ".claude", "skills", "notes")); target != filepath.Join(h.library, "notes") {
				t.Errorf("claude-code's placement points at %q", target)
			}
			for _, dir := range []string{h.library, filepath.Join(h.agentx, "worktrees")} {
				equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
			}
			equal(t, "kind", h.listed("notes")["kind"], lineage.KindManaged)
		})
	}
}

// TestForkTreeFollowsTheIgnoreRules judges fork directories the way the
// fork commit writer does and reads back the tree git wrote of each, which
// is what a commit of the fork records: files the skill's .gitignore, the
// user's global ignore file, at git's default place or at the
// core.excludesFile they set, and the system-file list name are left out;
// an attributes file of the user's changes nothing while the skill's own
// .gitattributes converts; a nested repository is refused unless ignored.
func TestForkTreeFollowsTheIgnoreRules(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()
	inv := h.invocation
	gitDir, _, err := gitx.OpenAccountRepo(ctx, inv().git, h.agentx)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, mkdirs(t, filepath.Join(h.config, "git"), "ignore"), "*.log\n")
	writeFile(t, filepath.Join(h.config, "git", "attributes"), "*.md text\n")
	// committed lists the files of the tree git writes of a directory
	// holding files, with their bytes where the case cares.
	committed := func(t *testing.T, files map[string]string) (forkJudged, map[string]string) {
		t.Helper()
		dir := t.TempDir()
		for path, content := range files {
			writeFile(t, mkdirs(t, filepath.Join(dir, filepath.Dir(path)), filepath.Base(path)), content)
		}
		j, err := inv().judgeFork(ctx, gitDir, dir, version{holds: func(string) bool { return false }}, false)
		if err != nil {
			t.Fatal(err)
		}
		tree := map[string]string{}
		if j.written == "" {
			return j, tree
		}
		for _, path := range strings.Split(h.accountGit("ls-tree", "-r", "--name-only", j.written), "\n") {
			tree[path] = h.accountGit("cat-file", "blob", j.written+":"+path)
		}
		return j, tree
	}
	keys := func(m map[string]string) string {
		var names []string
		for k := range m {
			names = append(names, k)
		}
		sort.Strings(names)
		return strings.Join(names, " ")
	}

	_, tree := committed(t, map[string]string{
		"SKILL.md": "s\n", ".gitignore": "secret.txt\n", "secret.txt": "local\n", "debug.log": "global, default place\n",
		".DS_Store": "finder\n", "notes.md.swp": "vim\n", "crlf.md": "crlf\r\n",
	})
	equal(t, "what the ignore rules leave in", keys(tree), ".gitignore SKILL.md crlf.md")
	equal(t, "a file the user's attributes name", tree["crlf.md"], "crlf\r")

	// A core.excludesFile of the user's takes the place of git's default.
	writeFile(t, filepath.Join(h.home, ".gitconfig"), "[core]\n\texcludesFile = ~/my-ignore\n")
	writeFile(t, filepath.Join(h.home, "my-ignore"), "*.tmp\n")
	_, tree = committed(t, map[string]string{"SKILL.md": "s\n", "scratch.tmp": "global, core.excludesFile\n"})
	equal(t, "a file core.excludesFile names", keys(tree), "SKILL.md")

	_, tree = committed(t, map[string]string{"SKILL.md": "s\n", ".gitattributes": "*.md text\n", "crlf.md": "crlf\r\n"})
	equal(t, "a file the skill's .gitattributes converts", tree["crlf.md"], "crlf")

	_, tree = committed(t, map[string]string{"SKILL.md": "s\n", ".gitignore": "!.DS_Store\n", ".DS_Store": "finder\n"})
	equal(t, "a system file the skill re-includes", keys(tree), ".DS_Store .gitignore SKILL.md")

	j, _ := committed(t, map[string]string{"SKILL.md": "s\n", "vendor/.git/HEAD": "ref: refs/heads/main\n", "vendor/lib.js": "x\n"})
	equal(t, "a nested repository", strings.Join(j.exposed, " "), "vendor/.git")
	if err := nestedRepoRefusal("notes", j.exposed); !strings.Contains(err.Error(), "notes holds a Git repository at vendor/.git") {
		t.Errorf("the refusal reads %v", err)
	}
	j, tree = committed(t, map[string]string{"SKILL.md": "s\n", ".gitignore": "vendor/\n", "vendor/.git/HEAD": "ref: refs/heads/main\n"})
	equal(t, "an ignored nested repository", strings.Join(j.exposed, " ")+"|"+keys(tree), "|.gitignore SKILL.md")

	h.mustRun("config", "set", "ignore_system_files", "false")
	if b, _ := os.ReadFile(filepath.Join(gitDir, "info", "exclude")); len(b) != 0 {
		t.Errorf("info/exclude holds %q with the setting off", b)
	}
	_, tree = committed(t, map[string]string{"SKILL.md": "s\n", ".DS_Store": "finder\n", "notes.md.swp": "vim\n"})
	equal(t, "system files with the setting off", keys(tree), ".DS_Store SKILL.md notes.md.swp")
}
