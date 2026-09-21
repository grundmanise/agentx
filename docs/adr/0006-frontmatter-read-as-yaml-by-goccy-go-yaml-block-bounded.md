---
status: accepted
date: 2026-09-21
---

# Skill frontmatter is read as YAML by goccy/go-yaml, with the block bounded before it is parsed

The `---` block at the top of a `SKILL.md` carries `name` and `description`. Both are shown wherever a skill is listed, and both are the first two fields of the content hash in [the CLI contract](../spec/cli-contract.md). The hash is a skill's version identity across machines, and [ADR-0004](0004-no-local-database-git-holds-lineage-files-hold-machine-state.md) makes it what a recorded base version names. How those two strings are read is therefore a versioning decision, not a formatting one.

## Decision

The block is still split by hand, the lines between the first `---` and the next, and then decoded by [goccy/go-yaml](https://github.com/goccy/go-yaml) v1.19.2 into a map. `name` and `description` are taken in any scalar form the standard allows; a key whose value is a mapping or a sequence contributes nothing, so a `metadata:` block is skipped as before. The version is pinned and stays pinned.

A block is refused unread above 65536 bytes or 256 opened lists or mappings. The refusal is a warning that names the file, and the skill is then named after its directory, exactly as an unparsable block already was.

## Why

The hand-rolled parser treated a key with an empty value as a nested mapping and skipped the indented lines under it, so a plain scalar continued on the lines below its key was silently dropped. Published skills are written that way: two of the nine in `vercel-labs/agent-skills` had an empty description in every scan.

Measured over a corpus of 49 published `SKILL.md` files from four public repositories: 2 parses change, both of them previously wrong, and neither parser rejects a file the other accepts. Parsing costs about 30 microseconds per file, roughly 9 ms for 300 skills, against the quarter-second scan budget ADR-0004 assumes. goccy/go-yaml has no transitive dependencies of its own; the binary grows 7.7%.

The two hashes that change are hashes that were wrong. No command records a content hash as a base version yet, so nothing on disk depends on the old values. The window for changing this reading without a migration is now.

## Considered and rejected

`gopkg.in/yaml.v3`: archived and declared unmaintained in April 2025, so it cannot be the library a version identity depends on. `adrg/frontmatter`: it wraps the splitting of the block, which is the trivial part already written, while pulling in an old `yaml.v2` and an old `toml`; the split stays ours so that the warning messages and the line numbers stay ours. Extending the hand-rolled parser with the scalar forms it was missing: the silent drop above is what hand-rolling the scalar rules already produced, and each further form is another rule to get wrong in a value that is hashed.

## Consequences

- The content hash now depends on a third-party library's behaviour. A future version that decodes a scalar differently silently re-versions every affected skill, with no local edit and no upstream change to explain it. That is a real cost of this decision: pin the version, and read the release notes before bumping it.
- The bound on the block exists because of the library, not because of the format. Its parser builds the whole tree before any depth guard applies, and its memory grows faster than its input: a 200 KB frontmatter block ended the process with a runtime out-of-memory that no `recover` catches, and a 389 KB flat one took 2.5 seconds. A `SKILL.md` comes from any repository a user adds, so the block is untrusted input and the bounds are checked before the parser sees it. Both files are a 10 ms warning now. Published blocks run to a few hundred bytes and open a handful of collections, so nothing real is near the bound; if the library later parses incrementally under its own limits, the bound can go.
- A tab inside a plain scalar is dropped, fusing the words either side of it, where the old parser preserved it. No file in the corpus does this, and it is silent when it happens.
- A block that is not valid YAML now loses the name as well as the description. The old parser was lenient enough to accept an unquoted colon in a description, the likely human error, and keep both fields; go-yaml refuses the whole block, so the skill falls back to its directory name. A duplicate key is refused the same way. The warning names the line and says why, which the old one could not.
- The library quotes the text it choked on, which comes from the file and can carry anything a terminal obeys. That message is clipped and its control characters replaced before it reaches a warning.
