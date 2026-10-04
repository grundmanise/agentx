package cli

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// The two account-repo checks below report and never repair, which is what
// every other doctor row does and what the account repo leaves room for:
// doctor takes no lock, so it cannot tell a run that is working from one
// that died. A remote is a settings write away from being named again, and
// a staging ref of a run still fetching looks exactly like one of a run
// that was killed: the run id says nothing about which, and no lock is
// held while either exists. A sweep would therefore have to guess, and a
// wrong guess takes the objects a live install is about to publish out from
// under it. Naming the state costs nothing and is always true.

// sourceRemotes reports the source remotes of the account repo that no
// settings entry names. A remote no entry names is not a source: `source
// fetch` and `source skills` both answer from the settings, so nothing
// names it again or cleans it up.
//
// An add that fails takes its own remote back, waiting for the lock to do
// it, so what reaches this row is only what no in-run cleanup can cover: a
// run killed outright, and a take-back an unrecoverable journal refused.
// The hint gives the two repairs the add itself gives when its take-back
// fails, in the same words, so the user hears one story whichever of the
// two told it; a remote of fork branches is told apart, see
// sourceRemotesHint. The other direction, an entry whose remote is gone,
// needs no row: every command that uses the remote names it as missing,
// and `source fetch` writes it back.
func (d *doctor) sourceRemotes(ctx context.Context, gitDir string) {
	settings, err := d.inv.loadSettings()
	if err != nil {
		// The settings row above already said what is wrong with the file;
		// this one says only that it could not be compared with, and its
		// hint names that file: the account repo itself opened fine. A file
		// a later agentx wrote is not to be fixed but read by that version,
		// so the hint then repeats the settings row's.
		hint := "fix " + home.SettingsPath(d.inv.dirs.Home) + ", then run doctor again"
		var newer *home.NewerSettingsError
		if errors.As(err, &newer) {
			hint = failureOf(err).hint
		}
		d.row("source_remotes", "fail", "cannot read the settings to compare the remotes with", hint)
		return
	}
	named := make(map[string]bool, len(settings.Sources))
	account := ""
	for _, s := range settings.Sources {
		named[source.ID(s.URL)] = true
		if s.Account {
			account = s.URL
		}
	}
	remotes := source.Remotes(ctx, d.inv.git, gitDir)
	var orphans, forks []string
	for id, remote := range remotes {
		switch {
		case named[id]:
		case remote.Refspec == gitx.ForkRefspec(source.RemoteName(id)):
			forks = append(forks, remoteSubject(id, remote.URL))
		default:
			orphans = append(orphans, remoteSubject(id, remote.URL))
		}
	}
	if len(orphans)+len(forks) == 0 {
		detail := "no source remote"
		if len(remotes) > 0 {
			detail = plural(len(remotes), "source remote") + " the settings name"
		}
		d.row("source_remotes", "ok", detail, "")
		return
	}
	sort.Strings(orphans)
	sort.Strings(forks)
	all := append(append([]string{}, orphans...), forks...)
	hint := sourceRemotesHint(orphans, forks, account)
	d.row("source_remotes", "warn", plural(len(all), "source remote")+" the settings do not name: "+strings.Join(all, ", "), hint)
}

// sourceRemotesHint is the source_remotes hint for the orphan remotes
// orphans and the orphan remotes of fork branches forks, account being the
// URL of the account remote the settings hold, "" when none. A remote of
// fork branches added back as a shared source would have its forks fetched
// without blobs. With no account remote set, it is one the settings lost,
// and only one remote can be made the account remote again. With one set,
// it is what a replaced account remote left, and adding it back with
// --account would replace the account remote the user chose, so it is
// only cleared.
func sourceRemotesHint(orphans, forks []string, account string) string {
	plain := "run 'agentx source add <url>' to add the source and take the remote with it, or 'agentx source remove <id>' to clear it"
	if len(forks) == 0 {
		return "for each, " + plain
	}
	var held string
	switch {
	case account != "":
		held = "run 'agentx source remove <id>' to clear it, since 'agentx source add <url> --account' would replace the account remote " + account
	case len(forks) == 1:
		held = "run 'agentx source add <url> --account' to make it the account remote again and take the remote with it, or 'agentx source remove <id>' to clear it"
	default:
		held = "run 'agentx source add <url> --account' for the one to make the account remote again, and 'agentx source remove <id>' for the rest"
	}
	if len(orphans) == 0 && (len(forks) == 1 || account != "") {
		return "for each, " + held
	}
	hint := "for " + strings.Join(forks, ", ") + ", which held your skills, " + held
	if len(orphans) > 0 {
		hint += "; for each remote that held none, " + plain
	}
	return hint
}

// remoteSubject is what a row calls a remote: the canonical URL the remote
// records, or its source id when that URL is not one agentx would have
// written. A URL agentx stored carries no user and no token (`source add`
// canonicalises it first), and one that does not parse back to itself did
// not come from agentx, so it is never printed. The id is the bare one and
// not the remote's name, since it is what the hint's `source remove <id>`
// accepts.
func remoteSubject(id, url string) string {
	if parsed, err := source.Parse(url); err == nil && !parsed.Stripped && parsed.URL == url {
		return url
	}
	return id
}

// stagedImports reports the import staging refs of runs that never
// published them. An install writes its import commits under
// refs/agentx/importing/<run>/<n> and points the import branches at them
// through its journal, and an update check does the same for the
// candidates it pins; the refs go once the journal has them, and a run
// killed in between leaves them. They hold the objects they name, so a
// source removed afterwards leaves its trees and blobs pinned by refs
// nothing reads.
func (d *doctor) stagedImports(ctx context.Context, gitDir string) {
	out, err := d.inv.git.Isolated(ctx, gitDir, "for-each-ref", "--format=%(refname)", lineage.ImportingPrefix)
	if err != nil {
		d.row("staged_imports", "fail", err.Error(), "run with --verbose to see the git commands")
		return
	}
	refs := strings.Fields(out) // a ref name holds no space, so a field is a ref
	if len(refs) == 0 {
		d.row("staged_imports", "ok", "no import is left staged", "")
		return
	}
	runs := map[string]bool{}
	for _, ref := range refs {
		run, _, _ := strings.Cut(strings.TrimPrefix(ref, lineage.ImportingPrefix), "/")
		runs[run] = true
	}
	sort.Strings(refs)
	interrupted := "1 interrupted install or update check"
	if len(runs) > 1 {
		interrupted = fmt.Sprintf("%d interrupted installs or update checks", len(runs))
	}
	d.row("staged_imports", "warn",
		plural(len(refs), "staging ref")+" from "+interrupted+": "+refs[0],
		"nothing reads them and they pin what they name; delete each with 'git --git-dir="+gitDir+" update-ref -d <ref>' while no agentx command is running, agentx serve included")
}
