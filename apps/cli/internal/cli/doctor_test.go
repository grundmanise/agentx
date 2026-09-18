package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

// stubGit puts a git shell script alone on the harness PATH.
func stubGit(t *testing.T, h *harness, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	h.env["PATH"] = dir
}

func versionStub(version string) string {
	return "#!/bin/sh\necho 'git version " + version + "'\n"
}

// doctorRows indexes the doctor events by check and returns their order.
func doctorRows(t *testing.T, events []jsonEvent) (map[string]jsonEvent, []string) {
	t.Helper()
	rows := map[string]jsonEvent{}
	var order []string
	for _, e := range events {
		if e["type"] != "doctor" {
			continue
		}
		check := e["check"].(string)
		rows[check] = e
		order = append(order, check)
	}
	return rows, order
}

func accountConfig(t *testing.T, h *harness, key string) string {
	t.Helper()
	out, err := exec.Command("git", "--git-dir="+filepath.Join(h.agentx, "account.git"), "config", "--get", key).Output()
	if err != nil {
		t.Fatalf("git config %s: %v", key, err)
	}
	return strings.TrimSpace(string(out))
}

func TestDoctorNoGitExits2(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.env["PATH"] = t.TempDir()

	out := h.run("--json", "doctor")
	equal(t, "exit", out.exit, 2)
	events := h.events(out.stdout)
	if got, want := h.types(events), []string{"doctor", "error", "result"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	equal(t, "doctor.check", events[0]["check"], "git")
	equal(t, "doctor.status", events[0]["status"], "fail")
	equal(t, "doctor.detail", events[0]["detail"], "git not found in PATH")
	equal(t, "error.code", events[1]["code"], "git")
	contains(t, "error.hint", events[1]["hint"].(string), "install git 2.40 or newer")
	equal(t, "result.ok", events[2]["ok"], false)

	out = h.run("doctor")
	equal(t, "exit", out.exit, 2)
	contains(t, "stdout", out.stdout, "git  fail  git not found in PATH")
	contains(t, "stderr", out.stderr, "error: git not found in PATH")
	contains(t, "stderr", out.stderr, "hint: install git 2.40 or newer")
}

func TestDoctorRejectsOldGit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		version string
		hint    string
	}{
		{"2.34.1", "Ubuntu 22.04 ships git 2.34"},
		{"2.4.0", "install git 2.40 or newer"},
		{"2.39.5", "install git 2.40 or newer"},
	}
	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			stubGit(t, h, versionStub(tt.version))

			out := h.run("--json", "doctor")
			equal(t, "exit", out.exit, 2)
			events := h.events(out.stdout)
			if got, want := h.types(events), []string{"doctor", "error", "result"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("event types = %v, want %v", got, want)
			}
			equal(t, "doctor.status", events[0]["status"], "fail")
			equal(t, "error.code", events[1]["code"], "git")
			equal(t, "error.message", events[1]["message"], "git "+tt.version+" is older than 2.40")
			contains(t, "error.hint", events[1]["hint"].(string), tt.hint)
		})
	}
}

func TestDoctorPassesAndCreatesAccountRepo(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	out := h.run("--json", "doctor")
	equal(t, "exit", out.exit, 0)
	equal(t, "stderr", out.stderr, "")
	events := h.events(out.stdout)
	rows, order := doctorRows(t, events)
	wantOrder := []string{"git", "merge_tree", "relative_worktree_paths", "isolated_commit", "home", "lock", "settings", "account_repo", "library"}
	if !reflect.DeepEqual(order, wantOrder) {
		t.Fatalf("checks = %v, want %v", order, wantOrder)
	}
	equal(t, "last event", events[len(events)-1]["type"], "result")
	equal(t, "result.ok", events[len(events)-1]["ok"], true)
	for _, check := range []string{"git", "merge_tree", "isolated_commit", "home", "lock", "settings", "account_repo", "library"} {
		equal(t, check+".status", rows[check]["status"], "ok")
	}
	equal(t, "relative_worktree_paths.status", rows["relative_worktree_paths"]["status"], "info")
	contains(t, "git.detail", rows["git"]["detail"].(string), "git 2.")
	contains(t, "isolated_commit.detail", rows["isolated_commit"]["detail"].(string), "commit 5d75017e77f5413f4337ef776244b8d8dc77ca90")
	contains(t, "home.detail", rows["home"]["detail"].(string), h.agentx)
	contains(t, "settings.detail", rows["settings"]["detail"].(string), "defaults")
	equal(t, "account_repo.detail", rows["account_repo"]["detail"], "created "+filepath.Join(h.agentx, "account.git"))
	equal(t, "library.detail", rows["library"]["detail"], h.library)

	equal(t, "gc.auto", accountConfig(t, h, "gc.auto"), "0")
	equal(t, "core.logAllRefUpdates", accountConfig(t, h, "core.logAllRefUpdates"), "true")
	equal(t, "merge.conflictStyle", accountConfig(t, h, "merge.conflictStyle"), "zdiff3")
	equal(t, "core.bare", accountConfig(t, h, "core.bare"), "true")
	equal(t, "version", readVersion(t, h), 1)
	equal(t, "home entries", listDir(t, h.agentx), "account.git lock ops version")

	// The second run finds the repo and changes nothing.
	out = h.run("--json", "doctor")
	equal(t, "exit", out.exit, 0)
	rows, _ = doctorRows(t, h.events(out.stdout))
	equal(t, "account_repo.status", rows["account_repo"]["status"], "ok")
	equal(t, "account_repo.detail", rows["account_repo"]["detail"], filepath.Join(h.agentx, "account.git")+" opens")
	equal(t, "version", readVersion(t, h), 1)

	out = h.run("doctor")
	equal(t, "exit", out.exit, 0)
	equal(t, "stderr", out.stderr, "")
	for _, line := range []string{
		"git                      ok    git 2.",
		"merge_tree               ok    ",
		"relative_worktree_paths  info  ",
		"isolated_commit          ok    commit 5d75017e77f5413f4337ef776244b8d8dc77ca90",
		"lock                     ok    free: " + filepath.Join(h.agentx, "lock"),
		"account_repo             ok    " + filepath.Join(h.agentx, "account.git") + " opens",
		"library                  ok    " + h.library,
	} {
		contains(t, "stdout", out.stdout, line)
	}
}

func TestDoctorAcceptsGit2100ByNumericComparison(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	stubGit(t, h, "#!/bin/sh\nif [ \"$1\" = --version ]; then echo 'git version 2.100.0'; exit 0; fi\nexec "+real+" \"$@\"\n")

	out := h.run("--json", "doctor")
	equal(t, "exit", out.exit, 0)
	rows, _ := doctorRows(t, h.events(out.stdout))
	equal(t, "git.status", rows["git"]["status"], "ok")
	equal(t, "git.detail", rows["git"]["detail"], "git 2.100.0")
	equal(t, "merge_tree.status", rows["merge_tree"]["status"], "ok")
	contains(t, "relative_worktree_paths.detail", rows["relative_worktree_paths"]["detail"].(string), "available: git 2.100.0")
	equal(t, "worktree.useRelativePaths", accountConfig(t, h, "worktree.useRelativePaths"), "true")
}

func TestDoctorIsolatedFromUserGitConfig(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	hooks := filepath.Join(h.home, "hooks")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	hostile := "[core]\n\tautocrlf = true\n\thooksPath = " + hooks + "\n[commit]\n\tgpgsign = true\n[user]\n\tname = Someone\n\temail = someone@example.com\n"
	if err := os.WriteFile(filepath.Join(h.home, ".gitconfig"), []byte(hostile), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(h.config, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.config, "git", "config"), []byte(hostile), 0o644); err != nil {
		t.Fatal(err)
	}
	h.env["GIT_AUTHOR_NAME"] = "Someone Else"
	h.env["GIT_COMMITTER_DATE"] = "1000000000 +0100"
	h.env["GIT_CONFIG_PARAMETERS"] = "'commit.gpgsign=true'"

	out := h.run("--json", "doctor")
	equal(t, "exit", out.exit, 0)
	rows, _ := doctorRows(t, h.events(out.stdout))
	equal(t, "isolated_commit.status", rows["isolated_commit"]["status"], "ok")
	equal(t, "isolated_commit.detail", rows["isolated_commit"]["detail"], "commit 5d75017e77f5413f4337ef776244b8d8dc77ca90")
	equal(t, "merge_tree.status", rows["merge_tree"]["status"], "ok")
	equal(t, "account_repo.status", rows["account_repo"]["status"], "ok")
}

func TestDoctorReportsHeldLock(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	lock := filepath.Join(h.agentx, "lock")
	f, err := os.OpenFile(lock, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}

	// Creating the account repo is a mutation, so a held lock blocks it.
	out := h.run("--json", "doctor")
	equal(t, "exit", out.exit, 7)
	events := h.events(out.stdout)
	rows, _ := doctorRows(t, events)
	equal(t, "lock.status", rows["lock"]["status"], "warn")
	contains(t, "lock.detail", rows["lock"]["detail"].(string), "held")
	contains(t, "lock.detail", rows["lock"]["detail"].(string), lock)
	equal(t, "account_repo.status", rows["account_repo"]["status"], "fail")
	equal(t, "error.code", events[len(events)-2]["code"], "locked")

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	equal(t, "exit", h.run("doctor").exit, 0)
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}

	// With the repo in place doctor only reports the lock.
	out = h.run("--json", "doctor")
	equal(t, "exit", out.exit, 0)
	rows, _ = doctorRows(t, h.events(out.stdout))
	equal(t, "lock.status", rows["lock"]["status"], "warn")
	equal(t, "account_repo.status", rows["account_repo"]["status"], "ok")

	out = h.run("doctor")
	equal(t, "exit", out.exit, 0)
	contains(t, "stdout", out.stdout, "lock                     warn  held: another agentx command holds "+lock)
}

func TestDoctorReportsCorruptSettings(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	path := filepath.Join(h.agentx, "settings.json")
	if err := os.WriteFile(path, []byte("{\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := h.run("--json", "doctor")
	equal(t, "exit", out.exit, 0)
	events := h.events(out.stdout)
	rows, _ := doctorRows(t, events)
	equal(t, "settings.status", rows["settings"]["status"], "fail")
	contains(t, "settings.detail", rows["settings"]["detail"].(string), path)
	contains(t, "settings.hint", rows["settings"]["hint"].(string), path)
	equal(t, "result.ok", events[len(events)-1]["ok"], true)
}

func TestDoctorMissingLibraryIsWarning(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	if err := os.Remove(h.library); err != nil {
		t.Fatal(err)
	}

	out := h.run("--json", "doctor")
	equal(t, "exit", out.exit, 0)
	events := h.events(out.stdout)
	rows, _ := doctorRows(t, events)
	equal(t, "library.status", rows["library"]["status"], "warn")
	equal(t, "library.detail", rows["library"]["detail"], "missing: "+h.library)
	contains(t, "library.hint", rows["library"]["hint"].(string), h.library)
	equal(t, "result.ok", events[len(events)-1]["ok"], true)
	if _, err := os.Stat(h.library); !os.IsNotExist(err) {
		t.Errorf("doctor created the library: %v", err)
	}
}

func TestDoctorUnusableAccountRepoExits8(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		setup func(t *testing.T, path string)
	}{
		{"a file", func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte("not a repository\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"an empty directory", func(t *testing.T, path string) {
			if err := os.Mkdir(path, 0o755); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			path := filepath.Join(h.agentx, "account.git")
			tt.setup(t, path)

			out := h.run("--json", "doctor")
			equal(t, "exit", out.exit, 8)
			events := h.events(out.stdout)
			rows, order := doctorRows(t, events)
			equal(t, "last check", order[len(order)-1], "library")
			equal(t, "account_repo.status", rows["account_repo"]["status"], "fail")
			contains(t, "account_repo.detail", rows["account_repo"]["detail"].(string), path)
			errorEvent := events[len(events)-2]
			equal(t, "error.code", errorEvent["code"], "account_repo")
			contains(t, "error.hint", errorEvent["hint"].(string), path)
			equal(t, "result.ok", events[len(events)-1]["ok"], false)
		})
	}
}

func TestStartupRejectsMissingOrOldGit(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		setup func(t *testing.T, h *harness)
		hint  string
	}{
		{"no git", func(t *testing.T, h *harness) { h.env["PATH"] = t.TempDir() }, "install git 2.40 or newer"},
		{"git 2.34", func(t *testing.T, h *harness) { stubGit(t, h, versionStub("2.34.1")) }, "Ubuntu 22.04 ships git 2.34"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			tt.setup(t, h)
			for _, args := range [][]string{{"config", "set", "label", "x"}, {"config", "list"}, {"machine"}} {
				out := h.run(append([]string{"--json"}, args...)...)
				equal(t, "exit", out.exit, 2)
				events := h.events(out.stdout)
				if got, want := h.types(events), []string{"error", "result"}; !reflect.DeepEqual(got, want) {
					t.Fatalf("%v: event types = %v, want %v", args, got, want)
				}
				equal(t, "error.code", events[0]["code"], "git")
				contains(t, "error.hint", events[0]["hint"].(string), tt.hint)
			}
			out := h.run("config", "set", "label", "x")
			equal(t, "exit", out.exit, 2)
			contains(t, "stderr", out.stderr, "hint: "+tt.hint)
			if _, err := os.Stat(filepath.Join(h.agentx, "settings.json")); !os.IsNotExist(err) {
				t.Errorf("config set wrote settings without git: %v", err)
			}

			// version, doctor and help stay available to report the problem.
			equal(t, "version exit", h.run("version").exit, 0)
			equal(t, "help exit", h.run("help").exit, 0)
			equal(t, "bare exit", h.run().exit, 0)
			equal(t, "doctor exit", h.run("doctor").exit, 2)
		})
	}
}
