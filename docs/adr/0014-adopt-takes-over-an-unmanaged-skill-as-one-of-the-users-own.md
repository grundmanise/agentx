---
status: accepted
date: 2026-11-15
---

# Adopt takes over an unmanaged skill as one of the user's own skills, and fork no longer accepts one

`agentx adopt --skill <name>` takes over an unmanaged skill that no other tool recorded, as one of the user's own skills, with its files as they are. It is a fork in the skill's place with nothing to fork from: a branch of its own in the account repo, `skills/<name>`, whose first commit holds the skill as it is and carries a new fork id, the skill's directory moved into that branch's worktree, and the library symlink to it. Nothing is fetched, and the skill has no upstream. The skill is looked for in the library first: an entry no branch records is adopted in its place, and every placement is left as it is, since placements name the library path. When the library holds no skill of that name, the skill is a real directory in an agent client's own skills directory: it moves into the library, and a symlink to the library takes its place, as a placement, except in the directory of a universal client, which reads the library and needs none. An identical directory of that name in another agent configuration is replaced the same way, and directories that differ refuse the adoption, since only one of them can become the skill. Either way the directory is moved, in one journaled mutation.

`agentx skill fork` no longer accepts an unmanaged skill, in its place or under a new name: it refuses, and names the adopt command. A fork is made of a skill agentx manages or a plugin provides, and adopt is the one command for a skill agentx does not manage yet, whether or not another tool recorded it. The hints that sent an unmanaged skill to fork, those of a publish, a history, a diff with `--commit`, a rename, a placement on the cloud machine and an install over a name the library holds, send it to adopt.

With `--as-is`, `agentx adopt --skill <name>` takes a skill another tool recorded over the same way, with its files as they are, and does not install it from its source. This amends [ADR 0012](0012-adopt-installs-from-the-source-and-replaces-what-the-other-tool-left.md), which kept an edit by forking the skill before adopting it: the directory the other tool left is unmanaged, so that fork is now refused, and `--as-is` is the one command that keeps the edit. Without the flag, a recorded skill is adopted as ADR 0012 decided, from its source, and what the other tool left gives way.

The preview, `agentx adopt` with no flag, still lists what the other tools recorded and nothing else, and ends with the command that takes over an unmanaged skill. Taking one over reports the skill as the library now holds it, and no `adoption` event, which is one record of another tool.

## Considered and rejected

Keeping `skill fork` for unmanaged skills beside the new adopt: two commands would do one job, every hint would have to choose between them, and the user would have to know whether another tool recorded a skill before knowing which to run. Fork also only ever reached a library entry, never a directory in an agent client's own skills directory. Listing the skills no tool recorded in the preview: an `adoption` event is one record, with the tool and the file it was read from, so a skill without a record would need a change to the output schema, for a list that `agentx skill list` and the inventory already give. Copying the directory out of the agent client's skills directory instead of moving it: the client would keep reading its own copy, which agentx does not manage, beside the adopted skill, and an edit made there would never reach the skill; a move leaves one copy, and a symlink where the client looks for it.

## Consequences

A skill adopted out of an agent client's own skills directory becomes visible to every universal client, since they read the library, where before only that client saw it. No other agent configuration gets a placement by the adoption; `agentx skill place` makes them.

After `--as-is`, the other tool's record stays, since agentx never writes that tool's files. Every later preview lists the skill under `Cannot adopt`, as a skill of the account remote on this machine, and `agentx adopt --all` counts it among the skills it could not adopt. A skill Skillshare linked into the library is a symlink into Skillshare's folder, which `--as-is` refuses as it refuses any library entry that is a symlink.

An unmanaged skill whose name breaks the Agent Skills name grammar cannot be adopted as it is: adopt has no flag to give it another name, so the user renames the directory by hand first. `agentx skill fork <name> --name <new>` used to do that for a library entry.

An adoption out of an agent client's own skills directory that is interrupted after the directory moved leaves that client without the skill until the journal is recovered, which the next agentx command that changes anything does.
