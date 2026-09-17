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
A bundle of skills, MCP servers, hooks and subagents installed from a marketplace into an agent client.
_Avoid_: extension, package

**Machine**:
One developer computer that has been scanned. Root of one snapshot.
_Avoid_: device, host, node

### Identity

**Logical asset**:
The fleet-wide identity of a skill or MCP server: the upstream for a skill, the address or package for a server. Copies on any machine, in any version, share one logical asset. An unmanaged skill is identified by its content instead.

**Physical asset**:
One logical asset on one machine in one version, the version being the content hash for a skill and the tool signature for a server. The unit drift is evaluated on and machine views count.

**Occurrence**:
One placement of a physical asset inside one agent configuration.

**Snapshot**:
The complete result of one scan of one machine. Replaced whole on every rescan, never edited.

### Skill lifecycle

**Source**:
A git repository containing one or more skills, added by URL, from which skills are installed. The account remote is a source. A third-party repo is a source.
_Avoid_: registry, marketplace, catalog, remote

**Upstream**:
The specific place a skill was installed or forked from: a source, a subpath inside it, and the version last taken. A published fork is an upstream for every other machine.
_Avoid_: origin, parent, remote

**Managed skill**:
A skill whose upstream and base version agentx knows, so it can be updated and reverted.

**Unmanaged skill**:
A skill found on disk whose upstream agentx cannot determine. Inventoried, never updated or reverted.

**Fork**:
A skill derived from an upstream skill and edited by the user, keeping the upstream name unless renamed. A fork supersedes the skill it was forked from in the agent configuration. Its lineage record keeps the third-party upstream so later upstream versions can be merged in. Lives in the account repo; local until published.
_Avoid_: copy, variant, override

**Greenfield skill**:
A skill created from scratch in agentx with no upstream. Behaves as a fork with nothing to merge from.
_Avoid_: custom skill, new skill

**Account repo**:
The one git repository per account that holds every fork and greenfield skill, one subdirectory per skill. Each machine works in its own clone in agentx home; a machine without an account has the clone before the remote exists. Which machine has which fork placed is metadata, not repository layout.
_Avoid_: cloud repo, library repo, fork repo

**Account remote**:
The git remote per account that account repos push to and fetch from once the machine is signed in. Hosted by agentx, or a repository the user supplies.
_Avoid_: cloud, server, origin

**Publish**:
An explicit user action that pushes a fork or greenfield skill from the account repo to the account remote. After publishing it is an upstream like any other and reaches other machines through install and update. Local commits happen on their own; publishing does not.
_Avoid_: sync, share, upload

**Modified skill**:
A managed skill whose on-disk content no longer matches its base version because it was edited by hand outside agentx. Shown as drift, local to one machine, never synced. Can be reverted or converted to a fork.
_Avoid_: dirty, drifted, changed

**Lineage record**:
The record that ties a fork, greenfield or managed skill to its upstream, meaning a source, a subpath and a version, and to the content hash of its base. Lives in the local database; for forks it is also written to the manifest.
_Avoid_: metadata, provenance

**Manifest**:
The file committed at the root of the account repo, outside every skill directory, with one entry per fork: its third-party upstream coordinates and base content hash. Reaches every clone by fetch, so a fork's provenance travels with the fork.
_Avoid_: lock file, metadata file, manifest.json

**Library**:
One machine's canonical skills directory at ~/.agents/skills, where every skill that machine has lives exactly once. Each machine owns its own library; nothing moves between libraries except through install and update.
_Avoid_: store, cache, vault

**Placement**:
The path inside one agent configuration's skills directory through which that client sees a library skill. A symlink by default, a copy when a client needs one.
_Avoid_: install, link, copy

**Base version**:
The upstream content a skill was installed, forked or last updated from, named by its content hash. The common ancestor in every three-way merge. For a managed skill it lives in the base version cache; for a fork it is a commit in the account repo that every machine reproduces identically.
_Avoid_: original, parent, snapshot

**Agentx home**:
The ~/.agentx directory holding the local database, machine identity, the base version cache and the account repo clone. Agents read forks through symlinks from the library into the account repo; nothing else in it is read by agents.

**Enabled configuration**:
An agent configuration on a machine that agentx installs into by default. The user chooses which configurations are enabled per machine.
_Avoid_: active, target, selected

### Sync

**Account**:
One user's identity on the sync server. Optional; the app is complete without one.
_Avoid_: user, workspace, team

**Operation**:
A user-requested change to one machine (install, remove, update), queued until that machine's agentx executes it. Has a status: pending, applied, failed. A bulk install is a batch of install operations, not a new kind. There is no move; a move is a remove on one machine and an install on another.
_Avoid_: task, job, command, intent, move
