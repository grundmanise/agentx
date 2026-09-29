package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// installChildEnv marks the process TestSkillAddRecoversFromAKilledBatch
// starts and carries what it installs: the source URL and the arguments
// after it, one per line.
const installChildEnv = "AGENTX_TEST_INSTALL_CHILD"

// TestInstallChildProcess is not a test: it is the body of that process,
// which installs against the parent's temporary home and is killed in the
// middle of it. It does nothing when the variable that marks it is not set.
func TestInstallChildProcess(t *testing.T) {
	words := os.Getenv(installChildEnv)
	if words == "" {
		t.Skip("not the install child process")
	}
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	args := append([]string{"skill", "add"}, strings.Split(words, "\n")...)
	os.Exit(Run(context.Background(), args, env, strings.NewReader(""), os.Stdout, os.Stderr))
}

// TestSkillAddRecoversFromAKilledBatch kills an install of several skills
// with SIGKILL at a durable boundary: a git wrapper on the PATH hands
// update-ref to the real git and then kills the process that ran it, so the
// import branches are written and the machine is left with the journal
// that describes the rest, one for the whole batch and not one per skill.
// The next command that mutates agentx home recovers it, and every skill of
// the batch is then whole: library directory, branch and placements. A
// batch that left one journal per skill would recover each of them on its
// own and could stop between two, leaving the machine holding branches for
// skills it does not have. Recovery from every other boundary is generic,
// and TestInstallRecoversFromEveryBoundary in internal/home covers it.
func TestSkillAddRecoversFromAKilledBatch(t *testing.T) {
	t.Parallel()
	h, s := bulkHarness(t)
	// The wrapper kills its own parent, which is the agentx process, right
	// after the refs are created: a process stopped after a live write and
	// before it could record that it made it.
	out := killedChild(t, h, "TestInstallChildProcess", installChildEnv, s.url+"\n--all", `
case " $* " in
*" update-ref "*)
	%GIT% "$@"
	status=$?
	kill -9 $PPID
	exit $status
	;;
esac
exec %GIT% "$@"
`)

	// The ref is written before the library directory, which is the order
	// the contract prescribes. Were the library published first, a process
	// stopped here would leave a real skill directory that no lineage
	// branch names, and the next scan would read it as a new unmanaged
	// skill, which is what the mutation safety spec forbids.
	for _, name := range bulkSkills {
		if head := h.accountGit("for-each-ref", "--format=%(objectname)", "refs/heads/managed/"+name); head == "" {
			t.Fatalf("the killed run created no branch for %s, so it was not killed where the test expects:\n%s", name, out)
		}
		if _, err := os.Stat(filepath.Join(h.library, name)); err == nil {
			t.Fatalf("the killed run got as far as the library of %s:\n%s", name, out)
		}
	}
	// One journal for the whole batch, in which each skill's ref comes
	// before its library directory and its two placements.
	var kinds []string
	for _, step := range readJournal(t, h) {
		kinds = append(kinds, step.Kind)
	}
	equal(t, "the journal's steps", strings.Join(kinds, ", "), strings.TrimSuffix(strings.Repeat("ref, publish, link, link, ", len(bulkSkills)), ", "))

	// The next mutation recovers it, under the lock it takes anyway, and
	// every skill of the batch is whole afterwards.
	if got := h.run("config", "set", "label", "recovered"); got.exit != 0 {
		t.Fatalf("the command after the killed batch: exit %d\n%s", got.exit, got.stderr)
	}
	equal(t, "journals after recovery", journalCount(t, h), 0)
	for _, name := range bulkSkills {
		if _, err := os.Stat(filepath.Join(h.library, name, "SKILL.md")); err != nil {
			t.Errorf("the library directory of %s was not recovered: %v", name, err)
		}
		for _, rel := range []string{".claude/skills/", ".cursor/skills/"} {
			if _, err := os.Readlink(filepath.Join(h.home, rel+name)); err != nil {
				t.Errorf("the placement %s%s was not recovered: %v", rel, name, err)
			}
		}
	}
	list := h.run("--json", "skill", "list")
	equal(t, "exit", list.exit, 0)
	events := h.eventsOfType(list.stdout, "library_skill")
	equal(t, "skills listed", len(events), len(bulkSkills))
	for _, ev := range events {
		equal(t, ev["name"].(string)+" kind", ev["kind"], "managed")
		equal(t, ev["name"].(string)+" state", ev["state"], "current")
	}
}
