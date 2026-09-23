package cli

import (
	"context"
	"sort"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// The two account-repo checks below report and never repair, which is what
// every other doctor row does and what the account repo leaves room for:
// doctor takes no lock, so it cannot tell a run that is working from one
// that died. A remote is a settings write away from being named again, and
// a staging ref of a run still fetching looks exactly like one of a run
// that was killed — the run id says nothing about which, and no lock is
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
// two told it. The other direction — an entry whose remote is gone — needs
// no row: every command that uses the remote names it as missing, and
// `source fetch` writes it back.
func (d *doctor) sourceRemotes(ctx context.Context, gitDir string) {
	settings, err := d.inv.loadSettings()
	if err != nil {
		// The settings row above already said what is wrong with the file;
		// this one says only that it could not be compared with, and its
		// hint names that file: the account repo itself opened fine.
		d.row("source_remotes", "fail", "cannot read the settings to compare the remotes with",
			"fix "+home.SettingsPath(d.inv.dirs.Home)+", then run doctor again")
		return
	}
	named := make(map[string]bool, len(settings.Sources))
	for _, s := range settings.Sources {
		named[source.ID(s.URL)] = true
	}
	remotes := source.Remotes(ctx, d.inv.git, gitDir)
	var orphans []string
	for id, remote := range remotes {
		if !named[id] {
			orphans = append(orphans, remoteSubject(id, remote.URL))
		}
	}
	if len(orphans) == 0 {
		detail := "no source remote"
		if len(remotes) > 0 {
			detail = plural(len(remotes), "source remote") + " the settings name"
		}
		d.row("source_remotes", "ok", detail, "")
		return
	}
	sort.Strings(orphans)
	d.row("source_remotes", "warn",
		plural(len(orphans), "source remote")+" the settings do not name: "+strings.Join(orphans, ", "),
		"for each, run 'agentx source add <url>' to add the source and take the remote with it, or 'agentx source remove <id>' to clear it")
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
// through its journal; the refs go once the journal has them, and a run
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
	d.row("staged_imports", "warn",
		plural(len(refs), "staging ref")+" from "+plural(len(runs), "interrupted install")+": "+refs[0],
		"nothing reads them and they pin what they name; delete each with 'git --git-dir="+gitDir+" update-ref -d <ref>' while no agentx command is running")
}
