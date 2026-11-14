---
status: accepted
date: 2026-11-14
---

# Serve polls the account remote with ls-remote and fetches only on a difference

The desktop app shows whether this machine is in sync with the account remote: how many of your own skills have edits to publish, how many another machine published to, or that both agree and when that was last checked. Until now serve checked for updates once, at start, so what another machine published while the app stayed open reached it only when the user ran `agentx skill check-updates`.

Serve now checks the account remote alone every 10 minutes while it runs, timed from the report of the check before it, so that no two checks ever run at once. Each check is one `git ls-remote --refs <url> refs/heads/skills/*` in the user's environment and one `for-each-ref` of the remote-tracking branches the last fetch left. When the two agree, which is what nearly every check finds, it fetches nothing, takes no lock and writes nothing, so a check costs the network one round trip with no pack and agentx home no version bump, and no rescan follows it. Only when they differ does it run the update check of the account remote alone: one fetch, the comparison of your own skills placed here with what it holds, and one write of its `last_fetched`. The `ls-remote` names the URL rather than the account repo's remote, so it needs no remote set up and reads no configuration of the account repo.

Shared sources are not polled. They are other people's repositories, there can be many of them, and an update from upstream is something the user decides to take; the check at start and `agentx skill check-updates` cover them. The account remote is the user's own, one repository, and what another machine published there is what the sync status is about.

Every check of serve ends its report with an `update_check` event, which says which check it was, when it ended, whether it reached the account remote and whether it fetched, so the app can say "checked 5 minutes ago" without inferring it from other events. A check that keeps failing, offline or with credentials refused, warns when it starts failing and then stays quiet until one succeeds again, so that a laptop on a train does not collect a warning every 10 minutes; each check's `update_check` still says `ok: false`.

## Considered and rejected

Fetching every 10 minutes: a fetch negotiates with the remote and can write refs and objects, and the write of `last_fetched` that ends a check bumps `version`, which makes serve rescan the whole machine every time for nothing. A timer of git's own, `git maintenance start` with its prefetch task: it installs a job with the system's scheduler that outlives serve and writes refs serve does not read (see the maintenance paragraph of the serve contract). Polling the shared sources too: many repositories of others, each a fetch, for updates the user takes when they choose.

## Consequences

Serve now makes a network call on a timer, one `ls-remote` every 10 minutes while the app is open and an account remote is set; with none set, or no account repo, it runs no git for it. What another machine published reaches the app within about 10 minutes, as `update_available` events. A remote that changes between two checks only in branches of skills this machine does not hold still causes one fetch, after which the remote-tracking branches agree with it again.
