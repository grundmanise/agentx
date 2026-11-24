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
One server declared in an agent configuration. What it exposed to a handshake, its tool signature, is one of its versions, recorded on it and never part of its identity.

**Plugin**:
A bundle of skills, MCP servers, hooks and subagents installed from a marketplace into an agent client. Identified across machines by its marketplace and plugin name.
_Avoid_: extension, package

**Machine**:
One developer computer that has been scanned, root of one inventory; or the cloud machine.
_Avoid_: device, host, node

**Machine id**:
The stable identifier of a machine, derived from the computer's platform identity, so agentx reinstalled on the same computer is the same machine. A random id is used only where no platform identity exists.
_Avoid_: device id, installation id

**Anonymous id**:
The id usage data is sent under: a keyed hash of the machine id that can't be turned back into it. Exists only while the user has agreed to send usage data.
_Avoid_: telemetry id, user id, distinct id

### Identity

**Logical asset**:
The fleet-wide identity of a skill or MCP server. For a skill of a shared source its source and subpath. For a hub skill its fork id, wherever it came from. For an unmanaged skill its content. For a server its address or package. Copies on any machine, in any version, share one logical asset, and a rename changes nothing.
_Avoid_: skill id, key

**Fork id**:
The permanent identity assigned when a hub skill is created, by `agentx skill new` or `agentx skill fork`, or by adopting a skill with its files as they are with `agentx skill import`, carried by the commit that creates it, which is never amended. Installing that published skill elsewhere preserves it, and so does renaming it; independently creating another skill assigns a different identity, even from the same upstream version.

**Physical asset**:
One logical asset on one machine: for a skill in one version, its content hash; for a server whatever it exposed, its versions, the tool signatures its declarations answered with, recorded on it. The unit drift is evaluated on and machine views count.

**Occurrence**:
One placement of a physical asset inside one agent configuration.

**Inventory**:
The complete result of one scan of one machine: its agent configurations with their skills, MCP servers and plugins. Replaced whole on every rescan, never edited. A snapshot is an inventory saved with the machine settings, which, unlike a scan's, may be partial.
_Avoid_: scan result, state

**Kept inventory**:
The last inventory the desktop app applied, kept in the app's own storage with the settings, machine, sources and adoption preview it loaded. A launch shows it under a frosted pane with a spinner until the startup checks pass; no command runs against it, and the serve child's first inventory replaces it.
_Avoid_: cached snapshot, last session, offline inventory

**Kept read**:
What one of the desktop app's reads returned, kept in the app's own storage for the next launch: a skill's files, history or update diff, a source's skill list, a SKILL.md, or your skills on the hub. A read of a skill's files or update diff, or of a SKILL.md, at the version a kept read names shows it and runs no command, since that version names their content; a skill's history, a source's skill list and your skills show while their command runs again. At most about 25 MB, the least recently used dropped first; another build of the app drops them all.
_Avoid_: cache entry, offline data

**Snapshot**:
An inventory saved with the machine settings, in a file or on the hub, where each machine has at most one. Leaves out what only describes the machine it was taken on: absolute paths, state and drift, scan counters and physical ids. `agentx snapshot save` saves it, and `agentx snapshot apply` applies it: it installs and places the skills it lists, each at its version or, for an entry with no version, which follows, at the latest; it skips a skill it cannot install, such as an unmanaged skill, and ignores MCP servers and plugins for now. A computer takes its own as a backup, with every entry exact; the cloud machine's is written by `skill place` and `skill remove` naming it, from the desktop app or the CLI, or by the web app, and may be partial. A scan's inventory is always complete.
_Avoid_: machine snapshot, machine document, export file, profile, manifest, pinned (for an entry's version)

**Cloud machine**:
The one machine that stands for every agent client the user runs in the cloud, such as Claude Code on the web, Codex cloud or Cursor's cloud agents. It has no computer of its own: its snapshot is its only state. Its skills are turned on and off with the same `skill place` and `skill remove` as a computer's, naming the machine, which commit to its snapshot instead of changing a disk. Each session applies it when it starts and never writes it.
_Avoid_: cloud, cloud environment, virtual machine, remote machine

### Skill lifecycle

**Source**:
A git repository added by URL, with an access. The hub is a source. A third-party repo is a source. Stored by its canonical URL, never with an embedded user or token. A managed skill's source is where it is published to: the shared source it was installed from, or the hub for hub skills, which have none until a hub is set.
_Avoid_: registry, marketplace, catalog, remote

**Shared source**:
Any source but the hub. It holds skills in folders on one branch and is fetched without blobs; installing one of its skills reads a managed copy. One repository is never both a shared source and the hub.
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
Where a forked skill came from, which its later versions come from: a source, a subpath inside it, and the version last taken. Only a skill made by `agentx skill fork` of a skill of a shared source, or of a forked skill that has an upstream, has one; its source is the hub. A skill of a shared source has no upstream: its newer versions come from its source.
_Avoid_: origin, parent, remote

**Kind**:
Whether agentx keeps a skill: managed or unmanaged, and nothing else. Where a managed skill is published to is its source, and where a forked one came from its upstream, neither of them a kind.
_Avoid_: fork, from-scratch skill, account skill (as kinds)

**Managed skill**:
A skill agentx keeps a branch of in the store, so it knows its source and can update it. Either a skill of a shared source, installed with `agentx skill add <source>` or adopted from another tool's record of it, whose base version is the import commit on its import branch; or a hub skill, made with `agentx skill new` or `agentx skill fork`, adopted as it is with `agentx skill import`, or installed from the hub, whose source is the hub and whose branch holds its history.

**Upstream-removed skill**:
A managed skill whose subpath no longer holds a skill in its source, as the last update check found it: no directory there, or one without a SKILL.md. Kept as it is, never updated, shown with this state until a check finds it in the source again.
_Avoid_: orphaned, dead

**Not published**:
A hub skill with no branch on the hub, under its name or a name it was renamed from, as the last fetch left the remote-tracking branches: a skill never published, one another machine removed, and every one while no hub is set. Derived on every read, never recorded. A former name's branch counts whatever skill it holds; only a bare publish checks that it holds the same skill, by its fork id, and leaves the skill out when it does not. A bare publish leaves it out and names it; publishing it by name creates the branch, which is how a skill is first published and how one another machine removed is put back.
_Avoid_: unpushed, orphaned

**Source-removed skill**:
A skill whose source is gone from this machine. For a managed skill, the canonical URL its lineage records names no source in machine settings; it is kept as it is, coordinates and placements included, and shown with this state until the source is added again, derived on every read and never recorded. A fork's source is the hub it is published to, not the source of its third-party upstream, so removing that third-party source never marks a fork; a fork whose branch is deleted from the hub is not published instead, and other machines keep its branch and worktree.
_Avoid_: orphaned, detached

**Unmanaged skill**:
A skill found on disk that agentx keeps no branch of, so it knows neither a source nor an upstream for it: a library entry no branch records, or a directory in an agent client's own skills directory. Inventoried, never updated. `agentx skill import` adopts it: from its source when another tool recorded one agentx can install from, and otherwise as a hub skill.

**Adopt**:
An action, not a kind: `agentx skill import` brings an unmanaged skill under agentx's management, in one of two ways, which the skill decides and no flag does. A skill another tool recorded as installed from a source agentx can install from is installed from that source, at the version it holds now, and becomes a managed skill of that shared source; what the tool left in the library gives way to that version, an edit included. Any other unmanaged skill, one no tool recorded or one whose record names nowhere agentx can install it from, such as a local folder, becomes a hub skill with its files as they are: a fork with no upstream, whose first commit holds them. One in the library is adopted in its place; a directory in an agent client's own skills directory moves into the library, and a symlink to the library takes its place. A skill that cannot be adopted, such as a symlink or a folder that is a Git repository of its own, stays unmanaged, and agentx says why and what to do. An adoption makes a new managed skill, where an adopt candidate is taken into a fork that already exists; the `candidate` state of an `adoption` event, a record that can be adopted, is not an adopt candidate either. The CLI (`agentx skill import`) and the desktop app (Import skills, Import all, Imported) call the action import, the word users know; this glossary and the spec keep Adopt as the name of the concept.
_Avoid_: migrate, claim, convert; import is fine for this action, but do not confuse it with applying a snapshot

**Fork**:
An action, not a kind: `agentx skill fork` makes a hub skill from another skill, a managed skill whose source is the hub and whose upstream is where it came from, keeping that skill's name unless given another. In the store and in this glossary, fork also names the branch and worktree that hold any hub skill, one made with `agentx skill new` or adopted as it is with `agentx skill import` included. A fork of a managed skill under its own name supersedes that skill in the agent configuration; a fork under a new name, and any fork of a plugin's skill or of a fork, sits beside the skill it came from instead, so an agent with the plugin turned on sees both under one name when the fork keeps it. Skills of a shared source, plugin-owned skills and forks can all be forked with `agentx skill fork`; an unmanaged skill is adopted instead, with `agentx skill import`. Renaming one with `agentx skill rename` keeps its fork id and history under the new name on this machine; its first publish under the new name creates the new name's branch on the hub and deletes the old one's when it is the same skill and the renamed skill holds all of it. Only that publish does: an old branch it keeps or cannot delete is never retried, and `agentx skill remove <old> --remote` deletes it. Its lineage record keeps the third-party upstream so later upstream versions can be merged in. Lives in the store; its edits stay on this machine until it is published; a publish, or an update, fork or rename of it, records them on its branch, and only a publish pushes them.
_Avoid_: copy, variant, override (as names of the action or of the skill it makes; the files a fork starts from are still a copy)

**Store**:
The git repository in agentx home, `~/.agentx/store.git`, that holds every hub skill as one branch per skill, every managed skill's base version as an import branch, and the last fetched state of each source, the hub's included. Each machine has its own and checks out only the forks placed on it, one worktree per fork; import branches have no worktree; a machine without a hub has one all the same. Shared upstream versions establish lineage, separately from each fork's identity.
_Avoid_: account repo, home repo (old names), cloud repo, library repo, fork repo, cache

**Import commit**:
A commit with no parent whose tree is one upstream version of a skill and whose id is a pure function of that version and its coordinates, so every machine produces the same commit for the same version. The first commit of a fork, and every upstream version merged into it later.
_Avoid_: base commit, snapshot commit, root

**Import branch**:
The branch `managed/<name>` in the store that points at a managed skill's current import commit. Never checked out; the library holds the real directory. When the skill is forked in its place, the fork's branch starts from its import commit and the import branch is deleted, so the import commit becomes the fork's base.
_Avoid_: managed branch, shadow branch, cache branch

**Adopt candidate**:
Something of the user's where a fork placed on this machine belongs: a directory at the fork's worktree that Git does not register, or a directory or a symlink of the user's at its library entry. Reported and left as it is; `skill place --force` adopts it, every file kept as an unpublished edit of the fork.
_Avoid_: orphan, stray directory

**Hub**:
The source whose settings entry carries the hub flag, added with `agentx source add <url> --hub`: the repository stores push forks to and fetch them from. In the MVP it is a Git repository: the user's own, one the user's machines share, or one someone else keeps to distribute skills, which may be read-only; later, hosted by agentx once the machine is signed in. Only hub skills' branches and snapshots travel through it.
_Avoid_: account remote (old name), personal, own, cloud, server, origin, fork source

**Hub skill**:
A managed skill whose source is the hub, and whose branch in the store, a fork, holds its history: one made with `agentx skill new` or `agentx skill fork`, adopted as it is with `agentx skill import`, or installed from the hub. Not a kind: where a managed skill is published to is its source.
_Avoid_: own skill, personal skill, the user's own skills, account skill

**Publish**:
An explicit user action, `agentx skill publish`, that records the edits of a hub skill as one commit on its branch in the store, then pushes the branch to its source, the hub. There is no separate commit step: edits stay on this machine until they are published. It never merges: when another machine published there first, it records and pushes nothing, and the update's hub step takes that in before the next publish. With a message, it folds what the hub lacks, the edits an update recorded included, into one commit under that message, unless a rename or an update from upstream lies between. A bare publish only updates branches the hub holds: it never creates one, and leaves out each skill not published, naming it. Publishing skills by name, one or several in one run, creates the branch of each one not published. Another machine with the same remote installs it from there, and takes later versions in with an update, a plain Git merge. Publishing a managed skill of a shared source, which only `agentx skill publish <name>` does, never a bare publish, records its edits as one commit on the branch of that source it is installed from, changing the skill's folder alone, under the user's Git identity and with no fork data, and pushes it there; the published version becomes its base. It never merges either: when the source changed the skill since it was installed, it pushes nothing, and an update takes the change in first.
_Avoid_: sync, share, upload

**Sync status**:
Whether this machine and the hub hold the same versions of hub skills, as of the last update check: skills to publish, edited or never published, skills incoming from other machines, or synced when there are neither. Shown at the right of the desktop app's title bar. A state, never an action: publishing and updating stay the user's explicit actions.
_Avoid_: published, up to date

**Installable skill**:
A hub skill that the hub holds and this machine's store has no branch of, as the last fetch of the remote found it. Listed by `skill list --remote` and installed by `skill add` with no source, `--name <name>` or `--all`, which creates the local branch at the remote's commit: the same skill, with its fork id and its history. A managed copy of its upstream that still holds its base version gives way to it; any other directory of that name in the library is refused.
_Avoid_: remote skill, available fork, installable fork

**Update check**:
Fetching the sources the managed skills came from, a forked skill's upstream included, and comparing each skill's base version with what its source holds now, by tree id; a fork's base version is the import commit its history names, whatever its own commits changed. It records what it found and never applies anything: a newer version becomes the skill's update candidate. Run by hand with `agentx skill check-updates`, and by the desktop app once when it launches, where the same pass also fetches every other added source, so that browsing and search see what the sources hold now. While the app runs, it is run again every 10 minutes for the hub alone, which fetches only when the remote's branches differ from the last fetch and fetches no other source. With a hub set, it also fetches the hub and lists hub skills placed here that another machine published to.
_Avoid_: sync, poll

**Update candidate**:
The import commit of the newer upstream version an update check found for a managed skill, a forked one included, pinned in the store until an update applies it, a later check finds another version or none, or the skill is removed. The same commit an install of that version writes.
_Avoid_: pending update, available version

**Update**:
Applying a managed skill's update candidate, only ever at the user's request: the import branch moves to the candidate, which becomes the skill's base version, the library directory takes the new content, keeping the files git ignores there, and copy placements that held the old version are refreshed while ones edited in place are kept. A skill that is not modified updates by replacement; a modified skill updates by a three-way merge of its edits with the candidate, over the base version, which applies when it is clean and leaves a pending merge when it conflicts; the next update applies a pending merge once it is resolved. The skill keeps its library name and its placements, whatever the newer version calls it. A fork always updates by a merge of its tip with the candidate, with its base version as the merge base, committed on its branch, whether or not it has commits of its own, its unpublished edits recorded on the branch first and kept, still unpublished. With a hub set, an update of a hub skill first takes in what another machine published of it there, its hub step: a plain Git merge of the two histories, a fast-forward when this machine has nothing of its own, whose conflict becomes a pending merge like any other; a branch of the same name that is another fork, by its fork id, is refused. For a skill with no upstream that is the whole update.
_Avoid_: upgrade, pull, sync

**Pending merge**:
The merge an update of a modified skill or a fork leaves when the edits and the update candidate conflict, or the hub step of an update when a fork's commits and the hub's conflict: an ordinary Git merge in progress in a Git worktree of the store under agentx home, resolved with Git, or by editing its files and running `skill update --continue`. The library directory, or a fork's worktree and branch, and so every agent, keeps its content until the next update applies the resolved merge, `skill update --theirs` drops the edits and applies the update candidate instead, or the merge is given up with `skill update --abort`. While it exists the skill is not removed. Survives restarts.
_Avoid_: merge ref, conflict state

**Modified skill**:
A managed skill holding edits not yet published, whatever its source: one of a shared source whose on-disk content differs from its base version, the version it was installed at or last published to that source, or a fork holding edits not yet published to the hub. A managed skill was edited outside agentx, by hand or by any other tool. Decided as git decides for a work tree: git records the directory over an index loaded from that version, and the skill is modified when the tree it writes differs from the version's, so a changed file mode or symlink counts as an edit and a file git ignores does not. A fork, a hub skill, is measured against the hub: it is modified while its skill directory differs from its branch tip, as git status in its worktree says, or that tip is not on the hub's branch as last fetched, or, for a fork not published, while its branch holds commits since the one that created it. So edits an update or a fork recorded on its branch stay modified, and in skill diff, until a publish pushes them. Shown as drift, local to one machine, never synced. A managed skill can be updated by merging its edits with the update, or converted to a fork.
_Avoid_: dirty, drifted, changed

**Lineage record**:
What ties a managed skill to its source or, for a forked one, its upstream, meaning a source, a subpath and a version, and to its base. Read from the store: the lineage trailers on a fork's branch or on a managed skill's import branch. There is no separate copy.
_Avoid_: metadata

**Lineage trailers**:
The `Agentx-` commit trailers in the store: source, path, upstream commit and content hash on every imported upstream version, the id of the current base on every merge agentx makes, the fork id on the commit that creates a fork, and the machine on every commit agentx writes on a fork's branch. On a machine's branch, the operation each commit made: its id, its kind, the skill and the configurations. How a fork's provenance reaches every clone by fetch, with no file in the branch tree.
_Avoid_: manifest, metadata file, provenance file

**Foreign commit**:
A commit on a fork's branch that agentx did not write: one made with git directly in the fork's worktree, by the user or an agent. It carries no `Agentx-Machine` trailer. agentx never rewrites it, and shows it as foreign in the fork's history.
_Avoid_: external commit, manual commit

**Library**:
One machine's canonical directory for skills managed by agentx at ~/.agents/skills. Installing there makes a skill available to every agent client that reads this universal location, regardless of placement choices.
_Avoid_: cache, vault

**Placement**:
The path inside one agent configuration's skills directory through which that client sees a library skill. The library itself for a universal client; otherwise a symlink, or a copy where the configuration's placement mode is copy or `--copy` asks for one.
_Avoid_: install, link, copy

**Placement mode**:
What agentx places into one agent configuration on this machine: a symlink to the library, the default, or a copy, chosen with `agentx config placement` and kept in the machine settings. It decides the placements made from then on and changes none already on disk. A universal client has none to choose: the library entry is its placement.
_Avoid_: copy mode (the per-skill record of copies), link mode

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
An agent client that reads the library as one of its own skills directories, such as Codex, Cursor, Gemini CLI, GitHub Copilot (while `COPILOT_HOME` is not set), OpenCode and Cline, or one of whose skills directories is the library through a symlink: Claude Code is one once its skills directory is linked to the library. It sees every library skill whether or not its agent configuration is enabled and whatever the placements, since the library entry is its placement. A skill leaves it only by leaving the library, which takes the skill from every agent client.
_Avoid_: library client, library reader

**Base version**:
The upstream content a skill was installed, forked, last updated from, or, for a skill of a shared source, last published to that source, named by its content hash. The recorded upstream version used to distinguish local edits from upstream changes. Always an import commit in the store that every machine reproduces identically: the tip of a managed skill's import branch, which an install, an update and a publish to a shared source move, or the last imported version on a fork's branch.
_Avoid_: original, parent, snapshot

**Agentx home**:
The ~/.agentx directory holding the store clone with its worktrees, the machine settings, local mutation journals and in-flight operation records. There is no database. Agents read forks through symlinks from the library into those worktrees; nothing else in it is read by agents.

**Machine settings**:
The one file in agentx home holding what only this machine decides: its label, enabled configurations, the placement mode of each configuration, sources and their pins, copy modes, and whether to send usage data. Leaves the machine only inside a snapshot, without the answer about usage data.
_Avoid_: config, preferences, local state

**App preference**:
A choice the desktop app alone makes on this machine, such as the editor a skill opens in. The app keeps it in its own config directory; it is never a machine setting, the CLI never reads it, and a snapshot does not carry it.
_Avoid_: app setting, machine setting

**Activity log**:
The desktop app's list of every agentx command it ran since it opened, whoever started it, a click or the app itself, each with its command line, start time, duration, exit code and output, and of what the serve child reported. Kept in memory, newest first, and gone when the app quits. It never holds a credential or an inventory's content.
_Avoid_: debug log, console, command history, journal

**Enabled configuration**:
An agent configuration selected by default for explicit placements. This choice does not disable an agent client's own discovery of the universal library.
_Avoid_: active, target, selected

### Sync

**Account**:
One user's identity on the account service. Optional; the app is complete without one.
_Avoid_: user, workspace, team

**Account service**:
The hosted service that keeps the account's machine records, inventories and operations once signed in, and tells a machine when something changed. Never carries skill content.
_Avoid_: sync server, backend, cloud, sync engine

**Install generation**:
The registered incarnation of a machine on the account service. A replacement installation gets a new generation so uploads from the previous installation cannot replace its state; restarting a process keeps the same generation.
_Avoid_: epoch, session

**Operation**:
A user-requested change to one machine (install, place, remove, update), the same commands the user runs on that machine, queued until that machine's agentx executes it. The cloud machine takes none, since nothing runs between its sessions to execute one: its snapshot is changed directly. Has a state: pending, claimed, applied, failed, skipped, cancelled or needs-input. Delivering an operation twice changes nothing. A bulk install is a batch of install operations, not a new kind. There is no separate move operation; a move installs on the destination first, then removes from the source after the destination confirms the requested content and placements.
_Avoid_: task, job, command, intent, move

**Operation record**:
The file in agentx home that holds one in-flight operation's state on the target machine until the account service has acknowledged the final state.
_Avoid_: queue entry, outbox row

**Set**:
A named group of skills the user defines, to view or install together. Kept in the account metadata branch, keyed by logical asset.
_Avoid_: collection, bundle, profile, tag group

**Account metadata**:
A branch of the store holding sets, tags, archived flags and descriptions as small files keyed by logical asset, merged by git like everything else. Never a file inside a skill directory.
_Avoid_: metadata file, manifest, frontmatter
