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
The fleet-wide identity of a skill or MCP server. For a managed skill its upstream: source and subpath. For a fork or greenfield skill its root commit in the account repo. For an unmanaged skill its content. For a server its address or package. Copies on any machine, in any version, share one logical asset, and a rename changes nothing.
_Avoid_: skill id, key

**Physical asset**:
One logical asset on one machine in one version, the version being the content hash for a skill and the tool signature for a server. The unit drift is evaluated on and machine views count.

**Occurrence**:
One placement of a physical asset inside one agent configuration.

**Snapshot**:
The complete result of one scan of one machine. Replaced whole on every rescan, never edited.

### Skill lifecycle

**Source**:
A git repository containing one or more skills, added by URL, from which skills are installed. The account remote is a source. A third-party repo is a source. Stored by its canonical URL, never with an embedded user or token.
_Avoid_: registry, marketplace, catalog, remote

**Source alias**:
A second URL for a source that moved, mapped to the canonical URL before any identity is derived.
_Avoid_: mirror, redirect

**Upstream**:
The specific place a skill was installed or forked from: a source, a subpath inside it, and the version last taken. A published fork is an upstream for every other machine.
_Avoid_: origin, parent, remote

**Managed skill**:
A skill whose upstream and base version agentx knows, so it can be updated and reverted. Its base version is the import commit on its import branch.

**Upstream-removed skill**:
A managed skill whose subpath no longer exists in its source. Kept as it is, never updated, shown with this state.
_Avoid_: orphaned, dead

**Unmanaged skill**:
A skill found on disk whose upstream agentx cannot determine. Inventoried, never updated or reverted.

**Fork**:
A skill derived from an upstream skill and edited by the user, keeping the upstream name unless renamed. A fork supersedes the skill it was forked from in the agent configuration. Its lineage record keeps the third-party upstream so later upstream versions can be merged in. Lives in the account repo; local until published.
_Avoid_: copy, variant, override

**Greenfield skill**:
A skill created from scratch in agentx with no upstream. Behaves as a fork with nothing to merge from.
_Avoid_: custom skill, new skill

**Account repo**:
The one git repository per account that holds every fork and greenfield skill as one branch per skill, every managed skill's base version as an import branch, and the last fetched state of each source. Each machine has its own clone in agentx home and checks out only the forks placed on it, one worktree per fork; import branches have no worktree; a machine without an account has the clone before the remote exists. Ancestry between imported upstream versions is reconstructed locally with replace refs.
_Avoid_: cloud repo, library repo, fork repo

**Import commit**:
A commit with no parent whose tree is one upstream version of a skill and whose id is a pure function of that version and its coordinates, so every machine produces the same commit for the same version. The first commit of a fork, and every upstream version merged into it later.
_Avoid_: base commit, snapshot commit, root

**Import branch**:
The branch `managed/<name>` in the account repo that points at a managed skill's current import commit. Never checked out; the library holds the real directory. Renamed into the fork namespace when the skill becomes a fork.
_Avoid_: managed branch, shadow branch, cache branch

**Adopt into fork**:
Installing a published fork over a directory that already exists in the library under that name, keeping the directory's content as pending changes on the fork.
_Avoid_: overwrite, take over

**Unfork**:
Retiring a fork in favour of an upstream version: the fork's branch is archived and the library gets a managed skill again.
_Avoid_: delete fork, downgrade

**Account remote**:
The git remote per account that account repos push to and fetch from once the machine is signed in. Hosted by agentx, or a repository the user supplies.
_Avoid_: cloud, server, origin

**Publish**:
An explicit user action that pushes one fork's or greenfield skill's branch from the account repo to the account remote. After publishing it is an upstream like any other and reaches other machines through install and update. Local commits happen on their own; publishing does not.
_Avoid_: sync, share, upload

**Modified skill**:
A managed skill whose on-disk content no longer matches its base version because it was edited by hand outside agentx. Shown as drift, local to one machine, never synced. Can be reverted or converted to a fork.
_Avoid_: dirty, drifted, changed

**Lineage record**:
What ties a fork, greenfield or managed skill to its upstream, meaning a source, a subpath and a version, and to its base. Read from the account repo: the lineage trailers on a fork's branch or on a managed skill's import branch. There is no separate copy.
_Avoid_: metadata

**Lineage trailers**:
The `Agentx-` commit trailers in the account repo: source, path, upstream commit and content hash on every imported upstream version, and the id of the current base on every merge agentx makes. How a fork's provenance reaches every clone by fetch, with no file in the branch tree.
_Avoid_: manifest, metadata file, provenance file

**Library**:
One machine's canonical skills directory at ~/.agents/skills, where every skill that machine has lives exactly once. Each machine owns its own library; nothing moves between libraries except through install and update.
_Avoid_: store, cache, vault

**Placement**:
The path inside one agent configuration's skills directory through which that client sees a library skill. A symlink by default, a copy when a client needs one.
_Avoid_: install, link, copy

**Base version**:
The upstream content a skill was installed, forked or last updated from, named by its content hash. The common ancestor in every three-way merge. Always an import commit in the account repo that every machine reproduces identically: the tip of a managed skill's import branch, or the last imported version on a fork's branch.
_Avoid_: original, parent, snapshot

**Agentx home**:
The ~/.agentx directory holding the account repo clone with its worktrees, the machine settings and in-flight operation records. There is no database. Agents read forks through symlinks from the library into those worktrees; nothing else in it is read by agents.

**Machine settings**:
The one file in agentx home holding what only this machine decides: its label, enabled configurations, sources and their pins, copy modes and the install generation. Never leaves the machine; `agentx export` copies it.
_Avoid_: config, preferences, local state

**Enabled configuration**:
An agent configuration on a machine that agentx installs into by default. The user chooses which configurations are enabled per machine.
_Avoid_: active, target, selected

### Sync

**Account**:
One user's identity on the account service. Optional; the app is complete without one.
_Avoid_: user, workspace, team

**Account service**:
The hosted service that keeps the account's machine records, snapshots and operations once signed in, and tells a machine when something changed. Never carries skill content.
_Avoid_: sync server, backend, cloud, sync engine

**Install generation**:
A counter in machine settings that increases on every fresh install of agentx on a machine, so the account service can tell a reinstall from a continuation.
_Avoid_: epoch, session

**Operation**:
A user-requested change to one machine (install, remove, update), queued until that machine's agentx executes it. Has a state: pending, claimed, applied, failed, skipped, cancelled or needs-input. Delivering an operation twice changes nothing. A bulk install is a batch of install operations, not a new kind. There is no move; a move is a remove on one machine and an install on another.
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
