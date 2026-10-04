package cli

import (
	"os"
	"reflect"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// TestDoctorReadsTheAccountRepoOnlyWhenItOpens: the account-repo rows
// follow the row that says the repo can be read. A run on a machine that
// has no account repo yet leaves them out, there being nothing to say about
// a repository that does not exist: TestDoctorPassesAndChangesNothing pins
// the rows of that run.
func TestDoctorReadsTheAccountRepoOnlyWhenItOpens(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	s, _, _ := h.standardSource(true)
	h.mustRun("source", "add", s.url)
	rows, order := doctorRows(t, h.events(h.mustRun("--json", "doctor").stdout))
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
// now from one a run died over. The row names the remote by its URL, or by
// its bare source id when the URL is one agentx would not have written, here
// one carrying a token: that id is what the hint's `source remove <id>`
// accepts. A remote of fork branches is told to come back with --account
// while the settings hold no account remote, and only to be cleared while
// they hold another: sourceRemotesHint's table has the wording of each.
func TestDoctorNamesARemoteTheSettingsDoNotName(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	s, _, _ := h.standardSource(true)
	h.mustRun("source", "add", s.url)
	h.mustRun("source", "remove", s.url)
	id := source.ID(s.url)
	remote := source.RemoteName(id)
	const other = "https://github.com/me/forks"

	for _, tc := range []struct {
		url, fetch, account, named, hint string
	}{
		{s.url, "", "", s.url, "'agentx source add <url>' to add the source"},
		{"https://user:token@github.com/owner/repo", "", "", id, "'agentx source add <url>' to add the source"},
		// A remote of fork branches is an account remote the settings lost:
		// added back without --account, its forks would be fetched blobless.
		{s.url, gitx.ForkRefspec(remote), "", s.url, "'agentx source add <url> --account' to make it the account remote again"},
		// With another account remote set, it is what replacing that one
		// left, and --account would replace the remote the user chose.
		{s.url, gitx.ForkRefspec(remote), other, s.url, "'agentx source remove <id>' to clear it, since 'agentx source add <url> --account' would replace the account remote " + other},
	} {
		// The remove took the remote with it; putting it back on its own is
		// exactly what the killed add left: a remote, and no entry naming it.
		h.accountGit("config", "remote."+remote+".url", tc.url)
		if tc.fetch != "" {
			h.accountGit("config", "remote."+remote+".fetch", tc.fetch)
		}
		if tc.account != "" {
			editSettings(t, h, func(s *home.Settings) { s.SetSource(home.Source{URL: tc.account, Account: true}) })
		}
		rows, _ := doctorRows(t, h.events(h.mustRun("--json", "doctor").stdout))
		equal(t, "source_remotes.status", rows["source_remotes"]["status"], "warn")
		equal(t, "source_remotes.detail", rows["source_remotes"]["detail"],
			"1 source remote the settings do not name: "+tc.named)
		contains(t, "source_remotes.hint", rows["source_remotes"]["hint"].(string), tc.hint)

		// The remedy every hint names has to work, or the hint is a dead
		// end: removing by id clears the remote although no settings entry
		// names it. The plain add is checked after the loop.
		h.mustRun("source", "remove", id)
		rows, _ = doctorRows(t, h.events(h.mustRun("--json", "doctor").stdout))
		equal(t, "source_remotes.status after removing by id", rows["source_remotes"]["status"], "ok")
		if tc.account != "" {
			editSettings(t, h, func(s *home.Settings) { s.RemoveSource(tc.account) })
		}
	}

	// And adding the source again writes the entry and the remote together.
	h.accountGit("config", "remote."+remote+".url", s.url)
	h.mustRun("source", "add", s.url)
	rows, _ := doctorRows(t, h.events(h.mustRun("--json", "doctor").stdout))
	equal(t, "source_remotes.status after adding again", rows["source_remotes"]["status"], "ok")
	equal(t, "source_remotes.detail after adding again", rows["source_remotes"]["detail"], "1 source remote the settings name")
}

// TestSourceRemotesHint: the hint of each mix of orphan remotes, of fork
// branches or not, with and without an account remote in the settings.
// Only one remote can be made the account remote, so several of fork
// branches are told to pick one.
func TestSourceRemotesHint(t *testing.T) {
	t.Parallel()
	const account = "https://github.com/me/forks"
	for _, tc := range []struct {
		name           string
		orphans, forks []string
		account        string
		want           string
	}{
		{"plain only", []string{"a"}, nil, "",
			"for each, run 'agentx source add <url>' to add the source and take the remote with it, or 'agentx source remove <id>' to clear it"},
		{"one fork", nil, []string{"f"}, "",
			"for each, run 'agentx source add <url> --account' to make it the account remote again and take the remote with it, or 'agentx source remove <id>' to clear it"},
		{"two forks", nil, []string{"f", "g"}, "",
			"for f, g, which held your skills, run 'agentx source add <url> --account' for the one to make the account remote again, and 'agentx source remove <id>' for the rest"},
		{"forks beside an account remote", nil, []string{"f", "g"}, account,
			"for each, run 'agentx source remove <id>' to clear it, since 'agentx source add <url> --account' would replace the account remote " + account},
		{"both", []string{"a"}, []string{"f"}, "",
			"for f, which held your skills, run 'agentx source add <url> --account' to make it the account remote again and take the remote with it, or 'agentx source remove <id>' to clear it; for each remote that held none, run 'agentx source add <url>' to add the source and take the remote with it, or 'agentx source remove <id>' to clear it"},
		{"both, two forks", []string{"a"}, []string{"f", "g"}, "",
			"for f, g, which held your skills, run 'agentx source add <url> --account' for the one to make the account remote again, and 'agentx source remove <id>' for the rest; for each remote that held none, run 'agentx source add <url>' to add the source and take the remote with it, or 'agentx source remove <id>' to clear it"},
		{"both beside an account remote", []string{"a"}, []string{"f"}, account,
			"for f, which held your skills, run 'agentx source remove <id>' to clear it, since 'agentx source add <url> --account' would replace the account remote " + account + "; for each remote that held none, run 'agentx source add <url>' to add the source and take the remote with it, or 'agentx source remove <id>' to clear it"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			equal(t, "hint", sourceRemotesHint(tc.orphans, tc.forks, tc.account), tc.want)
		})
	}
}

// TestDoctorSendsUnreadableSettingsBackToTheSettings: when the settings do
// not parse, source_remotes cannot compare the remotes with them, and its
// hint names the settings file to fix, not the account repo, which the row
// above has just reported as fine; for settings a later agentx wrote, it
// gives the settings row's hint to upgrade.
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

	// A file a later agentx wrote is read by that version, not fixed: both
	// rows give the settings row's hint.
	if err := os.WriteFile(settings, []byte(`{"schema_version":2,"sources":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, _ = doctorRows(t, h.events(h.mustRun("--json", "doctor").stdout))
	equal(t, "settings.hint", rows["settings"]["hint"], "upgrade agentx to the version that wrote it")
	equal(t, "source_remotes.hint", rows["source_remotes"]["hint"], "upgrade agentx to the version that wrote it")
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
	h.mustRun("skill", "add", s.url, "--name", "alpha")
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
		"2 staging refs from 1 interrupted install or update check: "+lineage.ImportingRef(run, 0))
	contains(t, "staged_imports.hint", rows["staged_imports"]["hint"].(string),
		"update-ref -d <ref>")

	// Text output lists it under Issues with its hint, like every other warning.
	text := h.mustRun("doctor", "--color", "off").stdout
	for _, line := range []string{
		"staged_imports",
		"2 staging refs from 1 interrupted install or update check: " + lineage.ImportingRef(run, 0),
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
