# Security policy

agentx reads your agent clients' configuration, installs skills from git repositories you add, and can
start the MCP servers your clients declare. A flaw in it can reach your files, your credentials and the
code on your machine, so we take security reports seriously.

## Report a vulnerability

Don't report a vulnerability in a public issue, pull request or discussion.

[Report it privately](https://github.com/grundmanise/agentx/security/advisories/new) through GitHub.
Only the maintainers see the report, and we can work on a fix and an advisory with you in the same place.
If you can't use GitHub, email [hey@agentx.wtf](mailto:hey@agentx.wtf) instead.

Include as much of this as you can:

- what an attacker can do, and what they need to do it
- the steps or a proof of concept that reproduces it
- the output of `agentx version`
- your operating system and the agent clients involved

## What to expect

- We acknowledge your report within 7 days.
- We keep you updated while we investigate and fix it.
- Once a fix is released, we publish a security advisory and credit you, unless you'd rather stay
  anonymous.

Please give us a reasonable time to release a fix before you disclose the vulnerability publicly.

## Supported versions

agentx is in early development and has no releases yet. Security fixes land on `main`, so build from
the latest `main` to get them.

## Scope

In scope is the code in this repository: the `agentx` command-line tool and its documentation. For
example:

- a skill, source or configuration file that makes agentx write outside the paths it manages
- input that makes agentx run a command it shouldn't
- a way for a skill's content to crash agentx or exhaust its memory
- credentials or tokens leaking into output, logs or exports

Out of scope:

- vulnerabilities in the agent clients themselves, in MCP servers or in `git`; report those to their
  maintainers
- the content of skills in repositories you choose to add; agentx installs what those repositories
  publish
- MCP servers doing harm when you run `agentx scan --handshake`, which starts the servers your clients
  declare, the way those clients do
