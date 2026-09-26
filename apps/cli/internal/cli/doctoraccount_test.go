package cli

import (
	"os"
	"reflect"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// TestDoctorReadsTheAccountRepoOnlyWhenItOpens: the two account-repo rows
// follow the row that says the repo can be read, and are left out of a run
// on a machine that has no account repo yet – there is nothing to say about
// a repository that does not exist.
func TestDoctorReadsTheAccountRepoOnlyWhenItOpens(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	rows, order := doctorRows(t, h.events(h.mustRun("--json", "doctor").stdout))
	bare := []string{"git", "fork_merges", "commit_identity", "home", "lock", "mutations", "settings",
		"account_repo", "library", "clients"}
	if !reflect.DeepEqual(order, bare) {
		t.Fatalf("checks with no account repo = %v, want %v", order, bare)
	}
	for _, check := range []string{"source_remotes", "staged_imports"} {
		if _, found := rows[check]; found {
			t.Errorf("%s is reported on a machine with no account repo", check)
		}
	}

	s, _, _ := h.standardSource(true)
	h.mustRun("source", "add", s.url)
	rows, order = doctorRows(t, h.events(h.mustRun("--json", "doctor").stdout))
	want := []string{"git", "fork_merges", "commit_identity", "home", "lock", "mutations", "settings",
		"account_repo", "source_remotes", "staged_imports", "library", "clients"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("checks = %v, want %v", order, want)
	}
	equal(t, "source_remotes.status", rows["source_remotes"]["status"], "ok")
	equal(t, "source_remotes.detail", rows["source_remotes"]["detail"], "1 source remote the settings name")
	equal(t, "staged_imports.status", rows["staged_imports"]["status"], "ok")
	equal(t, "staged_imports.detail", rows["staged_imports"]["detail"], "no import is left staged")
}

// TestDoctorNamesARemoteTheSettingsDoNotName is the state a run killed
// between the two holds of the lock that `source add` takes leaves behind:
// the remote is written and the settings entry is not. An add that fails
// takes its own remote back, so only a run killed outright, or one whose
// take-back an unrecoverable journal refused, reaches this row, and then
// doctor is the only thing that ever names it, since `source fetch` and
// `source skills` both answer from the settings. It reports and does not
// repair: doctor takes no lock, so it cannot tell a remote a run is writing
// now from one a run died over.
func TestDoctorNamesARemoteTheSettingsDoNotName(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	s, _, _ := h.standardSource(true)
	h.mustRun("source", "add", s.url)
	h.mustRun("source", "remove", s.url)
	// The remove took the remote with it; putting it back on its own is
	// exactly what the killed add left: a remote, and no entry naming it.
	remote := source.RemoteName(source.ID(s.url))
	h.accountGit("config", "remote."+remote+".url", s.url)

	rows, _ := doctorRows(t, h.events(h.mustRun("--json", "doctor").stdout))
	equal(t, "source_remotes.status", rows["source_remotes"]["status"], "warn")
	equal(t, "source_remotes.detail", rows["source_remotes"]["detail"],
		"1 source remote the settings do not name: "+s.url)
	equal(t, "source_remotes.hint", rows["source_remotes"]["hint"],
		"for each, run 'agentx source add <url>' to add the source and take the remote with it, or 'agentx source remove <id>' to clear it")

	// Both remedies the hint names have to work, or the hint is a dead end.
	// Removing by id clears the remote although no settings entry names it.
	h.mustRun("source", "remove", source.ID(s.url))
	rows, _ = doctorRows(t, h.events(h.mustRun("--json", "doctor").stdout))
	equal(t, "source_remotes.status after removing by id", rows["source_remotes"]["status"], "ok")
	equal(t, "source_remotes.detail after removing by id", rows["source_remotes"]["detail"], "no source remote")

	// And adding the source again writes the entry and the remote together.
	h.accountGit("config", "remote."+remote+".url", s.url)
	h.mustRun("source", "add", s.url)
	rows, _ = doctorRows(t, h.events(h.mustRun("--json", "doctor").stdout))
	equal(t, "source_remotes.status after adding again", rows["source_remotes"]["status"], "ok")
	equal(t, "source_remotes.detail after adding again", rows["source_remotes"]["detail"], "1 source remote the settings name")
}

// TestDoctorNamesARemoteItWillNotPrintByItsID: a remote whose URL agentx
// would not have written, here one carrying a token, is named by its bare
// source id and never by the URL. That id is what the hint's `source remove
// <id>` accepts, so the value the row prints works in the command it gives.
func TestDoctorNamesARemoteItWillNotPrintByItsID(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	s, _, _ := h.standardSource(true)
	h.mustRun("source", "add", s.url)
	h.mustRun("source", "remove", s.url)
	id := source.ID(s.url)
	h.accountGit("config", "remote."+source.RemoteName(id)+".url", "https://user:token@github.com/owner/repo")

	rows, _ := doctorRows(t, h.events(h.mustRun("--json", "doctor").stdout))
	equal(t, "source_remotes.status", rows["source_remotes"]["status"], "warn")
	equal(t, "source_remotes.detail", rows["source_remotes"]["detail"],
		"1 source remote the settings do not name: "+id)

	h.mustRun("source", "remove", id)
	rows, _ = doctorRows(t, h.events(h.mustRun("--json", "doctor").stdout))
	equal(t, "source_remotes.status after removing the printed id", rows["source_remotes"]["status"], "ok")
	equal(t, "source_remotes.detail after removing the printed id", rows["source_remotes"]["detail"], "no source remote")
}

// TestDoctorSendsUnreadableSettingsBackToTheSettings: when the settings do
// not parse, source_remotes cannot compare the remotes with them, and its
// hint names the settings file to fix, not the account repo, which the row
// above has just reported as fine.
func TestDoctorSendsUnreadableSettingsBackToTheSettings(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	s, _, _ := h.standardSource(true)
	h.mustRun("source", "add", s.url)
	settings := home.SettingsPath(h.agentx)
	if err := os.WriteFile(settings, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	rows, _ := doctorRows(t, h.events(h.mustRun("--json", "doctor").stdout))
	equal(t, "account_repo.status", rows["account_repo"]["status"], "ok")
	equal(t, "source_remotes.status", rows["source_remotes"]["status"], "fail")
	equal(t, "source_remotes.hint", rows["source_remotes"]["hint"], "fix "+settings+", then run doctor again")
}

// TestDoctorNamesStagingRefsOfAnInterruptedInstall: an install writes its
// import commits under refs/agentx/importing/<run>/<n> and takes them away
// once its journal holds them. A run killed in between leaves them, they
// pin what they name, and nothing else ever looks at them. Reported, not
// swept: the refs of a run that is still fetching look exactly like these,
// no lock is held while either exists, and a sweep that guessed wrong would
// take the objects a live install is about to publish out from under it.
func TestDoctorNamesStagingRefsOfAnInterruptedInstall(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha")
	commit := h.accountGit("rev-parse", lineage.ManagedRef("alpha"))

	// Two refs of one run, as a killed install of two skills leaves them.
	run := "0123456789abcdef"
	for _, ref := range []string{lineage.ImportingRef(run, 0), lineage.ImportingRef(run, 1)} {
		h.accountGit("update-ref", ref, commit)
	}

	out := h.mustRun("--json", "doctor")
	rows, _ := doctorRows(t, h.events(out.stdout))
	equal(t, "staged_imports.status", rows["staged_imports"]["status"], "warn")
	equal(t, "staged_imports.detail", rows["staged_imports"]["detail"],
		"2 staging refs from 1 interrupted install: "+lineage.ImportingRef(run, 0))
	contains(t, "staged_imports.hint", rows["staged_imports"]["hint"].(string),
		"update-ref -d <ref>")

	// Text output lists it under Issues with its hint, like every other warning.
	text := h.mustRun("doctor", "--color", "off").stdout
	for _, line := range []string{
		"staged_imports",
		"2 staging refs from 1 interrupted install: " + lineage.ImportingRef(run, 0),
		"hint: nothing reads them and they pin what they name",
	} {
		contains(t, "doctor", text, line)
	}

	// And doctor repaired nothing: the refs are still there, which is the
	// whole of the decision this row stands for.
	left := h.accountGit("for-each-ref", "--format=%(refname)", lineage.ImportingPrefix)
	equal(t, "the staging refs after doctor", left,
		lineage.ImportingRef(run, 0)+"\n"+lineage.ImportingRef(run, 1))
}
