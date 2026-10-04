# agentx

A desktop app and CLI that inventories the AI agent clients, skills, MCP servers and plugins on a developer's machines, and lets the developer create, fork, update and sync skills across those machines.

## Language

### Assets

**Agent client**:
A product that runs an AI coding agent on a machine. Examples: Claude Code, Cursor, Codex, Gemini CLI.
_Avoid_: harness, agent, client (accepted as aliases in conversation, never in code or docs)

**Agent configuration**:
One install of one agent client on one machine, identified by the client and its config path.
_Avoid_: config, profile, install

**Skill**:
A directory following the Agent Skills standard, containing a SKILL.md with frontmatter. Rules files, AGENTS.md and custom commands are not skills.
_Avoid_: prompt, command, rule

**MCP server**:
One server declared in an agent configuration, in one specific tool-signature version.

**Plugin**:
A bundle of skills, MCP servers, hooks and subagents installed from a marketplace into an agent client. Identified across machines by its marketplace and plugin name.
_Avoid_: extension, package

**Machine**:
One developer computer that has been scanned. Root of one snapshot.
_Avoid_: device, host, node

**Machine id**:
The stable identifier of a machine, derived from the computer's platform identity, so agentx reinstalled on the same computer is the same machine. A random id is used only where no platform identity exists.
_Avoid_: device id, installation id

### Identity

**Logical asset**:
The fleet-wide identity of a skill or MCP server. For a managed skill its upstream: source and subpath. For a fork or greenfield skill its fork id. For an unmanaged skill its content. For a server its address or package. Copies on any machine, in any version, share one logical asset, and a rename changes nothing.
_Avoid_: skill id, key

**Fork id**:
The permanent identity assigned when a fork or greenfield skill is created, carried by the commit that creates it, which is never amended. Installing that published fork elsewhere preserves it; independently creating another fork assigns a different identity, even from the same upstream version.

**Physical asset**:
One logical asset on one machine in one version, the version being the content hash for a skill and the tool signature for a server. The unit drift is evaluated on and machine views count.

**Occurrence**:
One placement of a physical asset inside one agent configuration.

**Snapshot**:
The complete result of one scan of one machine. Replaced whole on every rescan, never edited.

### Skill lifecycle

**Source**:
A git repository added by URL, with an access. The account remote is a source. A third-party repo is a source. Stored by its canonical URL, never with an embedded user or token.
_Avoid_: registry, marketplace, catalog, remote

**Shared source**:
Any source but the account remote. It holds skills in folders on one branch and is fetched without blobs; installing one of its skills reads a managed copy. One repository is never both a shared source and the account remote.
_Avoid_: tree source

**Source alias**:
A second URL for a source that moved, mapped to the canonical URL before any identity is derived.
_Avoid_: mirror, redirect

**Access**:
What git lets this machine do at a source, as the last add or fetch of it found: writable, read-only when the server answered with a known denial, or unknown, which is everything else. Advice, never a rule: no command refuses on it.
_Avoid_: permission, write rights, writable flag

**Default branch**:
The branch a source's `HEAD` named when it was last looked at. Shown, never followed: an unpinned source follows whatever `HEAD` names at each fetch.

**Upstream**:
The specific place a skill was installed or forked from: a source, a subpath inside it, and the version last taken. A published fork is an upstream for every other machine.
_Avoid_: origin, parent, remote

**Managed skill**:
A skill whose upstream and base version agentx knows, so it can be updated. Its base version is the import commit on its import branch.

**Upstream-removed skill**:
A managed skill whose subpath no longer holds a skill in its source, as the last update check found it: no directory there, or one without a SKILL.md. Kept as it is, never updated, shown with this state until a check finds it in the source again.
_Avoid_: orphaned, dead

**Source-removed skill**:
A skill whose source is gone from this machine. For a managed skill, the canonical URL its lineage records names no source in machine settings; it is kept as it is, coordinates and placements included, and shown with this state until the source is added again, derived on every read and never recorded. A fork's source is the account remote it is published to, not the source of its third-party upstream, so removing that third-party source never marks a fork; a fork is source removed when its branch is deleted from the account remote, and other machines keep its branch and worktree. Only the managed-skill case is reported so far.
_Avoid_: orphaned, detached

**Unmanaged skill**:
A skill found on disk whose upstream agentx cannot determine. Inventoried, never updated.

**Fork**:
A skill derived from an upstream skill and edited by the user, keeping the upstream name unless renamed. A fork supersedes the skill it was forked from in the agent configuration; a fork under a new name sits beside the skill it came from instead. Managed, unmanaged and plugin-owned skills and forks can all be forked with `agentx skill fork`. Renaming a fork with `agentx skill rename` makes a new fork under the new name, with a fork id of its own and the old one's history, and removes the old one; nothing records the rename. Its lineage record keeps the third-party upstream so later upstream versions can be merged in. Lives in the account repo; local until published.
_Avoid_: copy, variant, override

**Greenfield skill**:
A skill created from scratch in agentx with no upstream, with `agentx skill new`. Behaves as a fork with nothing to merge from, and is listed with kind `fork`.
_Avoid_: custom skill, new skill

**Account repo**:
The one git repository per account that holds every fork and greenfield skill as one branch per skill, every managed skill's base version as an import branch, and the last fetched state of each source. Each machine has its own clone in agentx home and checks out only the forks placed on it, one worktree per fork; import branches have no worktree; a machine without an account has the clone before the remote exists. Shared upstream versions establish lineage, separately from each fork's identity.
_Avoid_: cloud repo, library repo, fork repo

**Import commit**:
A commit with no parent whose tree is one upstream version of a skill and whose id is a pure function of that version and its coordinates, so every machine produces the same commit for the same version. The first commit of a fork, and every upstream version merged into it later.
_Avoid_: base commit, snapshot commit, root

**Import branch**:
The branch `managed/<name>` in the account repo that points at a managed skill's current import commit. Never checked out; the library holds the real directory. When the skill is forked in its place, the fork's branch starts from its import commit and the import branch is deleted, so the import commit becomes the fork's base.
_Avoid_: managed branch, shadow branch, cache branch

**Adopt candidate**:
Something of the user's where a fork placed on this machine belongs: a directory at the fork's worktree that Git does not register, or a directory or a symlink of the user's at its library entry. Reported and left as it is; `skill place --force` adopts it, every file kept as an uncommitted edit of the fork.
_Avoid_: orphan, stray directory

**Account remote**:
The source whose settings entry carries the account flag, added with `agentx source add <url> --account`: the repository account repos push forks to and fetch them from. In the MVP it is a Git repository the user owns; later, hosted by agentx once the machine is signed in. Only fork branches travel through it.
_Avoid_: cloud, server, origin, fork source

**Publish**:
An explicit user action, `agentx skill publish`, that pushes one fork's or greenfield skill's branch from the account repo to the account remote. It never merges: when another machine published there first, the update's account step takes that in before the next publish. Another machine with the same remote installs it from there, and takes later versions in with an update, a plain Git merge. Commits are explicit, made by the user with agentx or with git, and publishing pushes commits only.
_Avoid_: sync, share, upload

**Installable skill**:
One of the user's own skills that the account remote holds and this machine's account repo has no branch of, as the last fetch of the remote found it. Listed by `skill list --remote` and installed by `skill add` with no source, `--name <name>` or `--all`, which creates the local branch at the remote's commit: the same skill, with its fork id and its history. A managed copy of its upstream that still holds its base version gives way to it; any other directory of that name in the library is refused.
_Avoid_: remote skill, available fork, installable fork

**Update check**:
Fetching the sources the managed skills and forks came from and comparing each skill's base version with what its source holds now, by tree id; a fork's base version is the import commit its history names, whatever its own commits changed. It records what it found and never applies anything: a newer version becomes the skill's update candidate. Run by hand with `agentx skill check-updates`, and by the desktop app on launch and on a timer, where the same pass also fetches every other added source, so that browsing and search see what the sources hold now. With an account remote set, it also fetches the account remote and lists the user's own skills placed here that another machine published to.
_Avoid_: sync, poll

**Update candidate**:
The import commit of the newer upstream version an update check found for a managed skill or a fork, pinned in the account repo until an update applies it, a later check finds another version or none, or the skill is removed. The same commit an install of that version writes.
_Avoid_: pending update, available version

**Update**:
Applying a managed skill's update candidate, only ever at the user's request: the import branch moves to the candidate, which becomes the skill's base version, the library directory takes the new content, keeping the files git ignores there, and copy placements that held the old version are refreshed while ones edited in place are kept. A skill that is not modified updates by replacement; a modified skill updates by a three-way merge of its edits with the candidate, over the base version, which applies when it is clean and leaves a pending merge when it conflicts; the next update applies a pending merge once it is resolved. The skill keeps its library name and its placements, whatever the newer version calls it. A fork always updates by a merge of its tip with the candidate, with its base version as the merge base, committed on its branch, whether or not it has commits of its own, and only while it has no uncommitted edits. With an account remote set, an update of one of your own skills first takes in what another machine published of it there, its account step: a plain Git merge of the two histories, a fast-forward when this machine has nothing of its own, whose conflict becomes a pending merge like any other; a branch of the same name that is another fork, by its fork id, is refused. For a skill with no upstream that is the whole update.
_Avoid_: upgrade, pull, sync

**Pending merge**:
The merge an update of a modified skill or a fork leaves when the edits and the update candidate conflict, or the account step of an update when a fork's commits and the account remote's conflict: an ordinary Git merge in progress in a Git worktree of the account repo under agentx home, resolved with Git. The library directory, or a fork's worktree and branch, and so every agent, keeps its content until the next update applies the resolved merge, or the merge is given up with `skill update --abort`. While it exists the skill is not removed. Survives restarts.
_Avoid_: merge ref, conflict state

**Modified skill**:
A managed skill or a fork whose on-disk content differs from what it is compared with: a managed skill's base version, a fork's branch tip. It was edited outside agentx, by hand or by any other tool. Decided as git decides for a work tree: git records the directory over an index loaded from that version, and the skill is modified when the tree it writes differs from the version's, so a changed file mode or symlink counts as an edit and a file git ignores does not. For a fork that is what git status in its worktree says of the skill directory. Shown as drift, local to one machine, never synced. A managed skill can be updated by merging its edits with the update, or converted to a fork. A fork stays modified until its edits are committed, and a command that would move its branch refuses until then.
_Avoid_: dirty, drifted, changed

**Lineage record**:
What ties a fork, greenfield or managed skill to its upstream, meaning a source, a subpath and a version, and to its base. Read from the account repo: the lineage trailers on a fork's branch or on a managed skill's import branch. There is no separate copy.
_Avoid_: metadata

**Lineage trailers**:
The `Agentx-` commit trailers in the account repo: source, path, upstream commit and content hash on every imported upstream version, the id of the current base on every merge agentx makes, the fork id on the commit that creates a fork, and the machine on every commit agentx writes on a fork's branch. How a fork's provenance reaches every clone by fetch, with no file in the branch tree.
_Avoid_: manifest, metadata file, provenance file

**Foreign commit**:
A commit on a fork's branch that agentx did not write: one made with git directly in the fork's worktree, by the user or an agent. It carries no `Agentx-Machine` trailer. agentx never rewrites it, and shows it as foreign in the fork's history.
_Avoid_: external commit, manual commit

**Library**:
One machine's canonical directory for skills managed by agentx at ~/.agents/skills. Installing there makes a skill available to every agent client that reads this universal location, regardless of placement choices.
_Avoid_: store, cache, vault

**Placement**:
The path inside one agent configuration's skills directory through which that client sees a library skill. The library itself for a universal client; otherwise a symlink by default or a copy when needed.
_Avoid_: install, link, copy

**Displaced placement**:
A placement whose kind on disk is not the one agentx keeps for it: a real directory where a symlink is kept, or the symlink to the library directory where machine settings record a copy. Reported as drift and never put back on its own: `skill place` puts it back when the user asks, and replaces a directory whose content differs from the library only with `--force`, which discards that content.
_Avoid_: broken link, overwritten

**Missing placement**:
A placement a library skill lacks in an enabled configuration: nothing is at that configuration's own placement path, whether the skill was never placed there or the placement went. Reported as drift, for information only; `skill place` places it again when the user asks. A universal client and a disabled configuration never have one.
_Avoid_: unplaced, orphaned

**Worktree missing**:
A fork placed on this machine whose worktree, the skill directory in it or its library symlink is gone, and was not yet put back from the fork's branch. Not a missing placement, which is a configuration's absent place.
_Avoid_: missing fork, broken fork

**Universal client**:
An agent client that reads the library as one of its own skills directories, such as Codex and Gemini CLI, or one of whose skills directories is the library through a symlink: Cursor, which reads Claude Code's skills directory, is one once that directory is linked to the library. It sees every library skill whether or not its agent configuration is enabled and whatever the placements, since the library entry is its placement. A skill leaves it only by leaving the library, which takes the skill from every agent client.
_Avoid_: library client, library reader

**Base version**:
The upstream content a skill was installed, forked or last updated from, named by its content hash. The recorded upstream version used to distinguish local edits from upstream changes. Always an import commit in the account repo that every machine reproduces identically: the tip of a managed skill's import branch, or the last imported version on a fork's branch.
_Avoid_: original, parent, snapshot

**Agentx home**:
The ~/.agentx directory holding the account repo clone with its worktrees, the machine settings, local mutation journals and in-flight operation records. There is no database. Agents read forks through symlinks from the library into those worktrees; nothing else in it is read by agents.

**Machine settings**:
The one file in agentx home holding what only this machine decides: its label, enabled configurations, sources and their pins, and copy modes. Never leaves the machine; `agentx export` copies it.
_Avoid_: config, preferences, local state

**Enabled configuration**:
An agent configuration selected by default for explicit placements. This choice does not disable an agent client's own discovery of the universal library.
_Avoid_: active, target, selected

### Sync

**Account**:
One user's identity on the account service. Optional; the app is complete without one.
_Avoid_: user, workspace, team

**Account service**:
The hosted service that keeps the account's machine records, snapshots and operations once signed in, and tells a machine when something changed. Never carries skill content.
_Avoid_: sync server, backend, cloud, sync engine

**Install generation**:
The registered incarnation of a machine on the account service. A replacement installation gets a new generation so uploads from the previous installation cannot replace its state; restarting a process keeps the same generation.
_Avoid_: epoch, session

**Operation**:
A user-requested change to one machine (install, remove, update), queued until that machine's agentx executes it. Has a state: pending, claimed, applied, failed, skipped, cancelled or needs-input. Delivering an operation twice changes nothing. A bulk install is a batch of install operations, not a new kind. There is no separate move operation; a move installs on the destination first, then removes from the source after the destination confirms the requested content and placements.
_Avoid_: task, job, command, intent, move

**Operation record**:
The file in agentx home that holds one in-flight operation's state on the target machine until the account service has acknowledged the final state.
_Avoid_: queue entry, outbox row

**Set**:
A named group of skills the user defines, to view or install together. Kept in the account metadata branch, keyed by logical asset.
_Avoid_: collection, bundle, profile, tag group

**Account metadata**:
A branch of the account repo holding sets, tags, archived flags and descriptions as small files keyed by logical asset, merged by git like everything else. Never a file inside a skill directory.
_Avoid_: metadata file, manifest, frontmatter
