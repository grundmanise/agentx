---
status: accepted
date: 2026-10-29
---

# Adoption installs the skill from its source and replaces what the other tool left in the library

`agentx adopt` reads what another tool recorded about a skill it installed, the source, the subpath and the ref, and installs the skill from that source exactly as `agentx skill add` does, at the version the source holds at its pin. Whatever that tool left under the skill's name in the library gives way to that version: the directory it copied, edited since or not, or the symlink Skillshare leaves there. The base is the version installed, so the skill is managed and current the moment it is adopted, and nothing on disk is ever recorded as if it came from upstream. This supersedes ADR-0009 and ADR-0010: no folder hash is looked up in the history of the source, no route establishes a version from the directory, and no file is restored, since the whole directory is the version. It also makes a second tool cheap to support: Skillshare's records are read into the same shape as the Vercel lock file's, and the directory Skillshare keeps in its own folder is never copied, since the library gets the version from the source.

## Considered and rejected

Recording the version the other tool installed as the base and keeping the directory, with the files the tool leaves out restored: it kept the user's edits as edits against that version, which is what ADR-0009 and ADR-0010 bought, at the cost of a history search per skill, a per-tool list of the files the tool leaves out, a route that establishes the version from the directory alone, an explicit `--base` for the skills no route reached, and for Skillshare a copy from one folder into another. An edit is the user's to keep by forking the skill before adopting it, which is one command. Copying Skillshare's directory into the library and establishing its base from the commit its store records: it is that cost again for the second tool, and the version the source holds now is what an update would move the skill to anyway.
