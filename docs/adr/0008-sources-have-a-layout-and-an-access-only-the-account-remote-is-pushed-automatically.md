---
status: accepted
date: 2026-10-02
---

# Sources have a layout and an access; the account remote is a source, and the only one pushed to automatically

A source is a git repository added by URL, with a layout and an access. The **tree** layout holds skills in folders anywhere in the tree of one branch, and reading it installs a managed copy. The **fork** layout holds one branch per fork, `skills/<name>`, and reading it installs a fork. The account remote is the source of the fork layout that its settings entry marks with the account flag: forks publish to it, and it is the only source anything is ever pushed to without an explicit command, by auto-push. `agentx remote set`, `show` and `unset` stay as other names of `agentx source add --account`, the account remote's row of the source listing, and `agentx source remove` of the account remote. Whether this machine may write to a source is asked of git, with a dry run of a push, and recorded as `writable`, `read-only` or `unknown`.

Every source, the account remote included, is the git remote `src-<id>` of the account repo, `<id>` derived from its canonical URL. The account repo's remote `origin`, which an earlier agentx gave the account remote, is moved once into the settings and the remote of its source, by the first command that changes something, before that command reads anything.

## Why

One model for one thing. The account remote was already a repository added by URL, fetched by agentx and listed beside the sources; keeping it apart meant a second settings home, a second event, a second set of URL rules and a second fetch path. A layout says how a repository holds skills, which decides how it is read and written: a tree source is fetched without blobs, since only the `SKILL.md` files of one branch are read until a skill is installed, while a fork source is fetched whole, since its forks' history is what pull, publish and revert read. A fetch without blobs followed by a whole fetch leaves the older blobs of the fork branches missing, because the first fetch's commits are offered as already held, so a fork repository is never fetched as a tree source, and the one whole fetch after it was is made with `--refetch`, keyed on the source ref the tree fetch left.

The layout is declared, never guessed: an empty repository, or one whose default branch holds skills while `skills/*` branches exist, could be read either way, and a guess that read a fork repository as a tree would damage its history on this machine. A repository becomes the account remote only while no managed skill was installed from it as a tree source, since those copies would then be checked against a repository no tree fetch reaches.

`src-<id>` for every remote. The account remote's remote is named by its identity like every other, so that a later version can keep several fork sources and move the account flag between them without renaming a remote, and a user's global `remote.origin.*`, meant for their projects, can never redirect a push of agentx's. The account remote was never released under `origin`, so the move costs only the homes that ran a pre-release build, and it runs in its own hold of the lock, in an order where every step repeats safely and `origin` goes last, so a stop anywhere leaves a move the next command finishes; until then, commands that only read find the account remote where it is.

Access from git. Rights change on the server and are only known to it: a dry run of deleting a branch that cannot exist asks git, with the user's credentials and rewrites, whether a push would be let through, needs no object, sends no pack, and changes nothing even if the dry run were lost. `read-only` is recorded only for a denial a host is known to answer with, since the app greys out writing on it, and an answer that only needs the user to authorise a token must not read as a lack of rights; everything else is `unknown`. It is advice: no command refuses on a recorded access, and the push that follows is the truth.

Only the account remote is pushed to automatically. Writing anywhere else is a decision about someone else's repository and stays an explicit command.

## Considered and rejected

Keeping `origin` for the account remote beside `src-<id>` for the sources: two naming rules, a flag that could never move to another fork source, and a remote name the user's own global configuration can address. Guessing the layout from what the repository holds: the cases above where the guess is wrong, and a wrong guess costs the forks' history. A separate settings key for fork sources: it would split the one model, while an earlier agentx reading a fork entry as a tree source is self-healed on the next run of this version, which writes the fork remote again and fetches once with `--refetch`. A probe that writes a ref, or that pushes an object: it would leave something behind in a repository of someone else's on every add. Recording `read-only` for any refusal: an expired token or a missing organisation authorisation would then grey out writing for good.

## Consequences

The settings entry of a source gains `layout`, `account`, `push_url`, `access`, `access_checked` and `default_branch`; `schema_version` stays 1, all of them being additive, and an agentx that knows no layout reads a fork entry as a tree source, which is unsupported and named in the contract. The `remote` event is gone; the account remote is the `source` event with `account` true. `source fetch --all` fetches the account remote too. A fork source holds no skill to install by URL: `source skills` and `skill add` of it are refused with the commands that list and install its forks. Until a later version keeps several fork sources, the account remote is the only fork source, and replacing it with another URL forgets what the old one held, its forks staying on the machine and publishing to the new one.

## Amendment (2026-10-03, MVP)

- Auto-push is removed. Nothing is pushed automatically, to the account remote or to any other source: every push is an explicit publish. The title's "the only one pushed to automatically" and the sentences above that say the account remote is pushed to without an explicit command are superseded; the auto_push setting, the serve tick that pushed and `AGENTX_PUSH_QUIET` are gone.
