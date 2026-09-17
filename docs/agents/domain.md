# Domain Docs

How the engineering skills should consume this repo's domain documentation when exploring the codebase.

## Before exploring, read these

- **`CONTEXT.md`** at the repo root: the glossary for the whole product.
- **`docs/adr/`**: read ADRs that touch the area you're about to work in.
- **`docs/spec/`**: read the spec that covers the area.

If any of these files don't exist, **proceed silently**. Don't flag their absence; don't suggest creating them upfront. The `/domain-modeling` skill (reached via `/grill-with-docs` and `/improve-codebase-architecture`) creates them lazily when terms or decisions actually get resolved.

## File structure

Single-context repo:

```
/
├── CONTEXT.md
├── docs/
│   ├── adr/
│   │   ├── 0001-go-cli-owns-machine-and-account-tauri-renders.md
│   │   ├── 0002-per-machine-libraries-one-account-repo-content-moves-by-install.md
│   │   └── 0003-account-repo-one-branch-per-fork-checked-out-as-worktrees.md
│   └── spec/
└── apps/
```

## Public and private docs

`agentx-private` repo holds business decisions, private specs (e.g. asset-graph spec), vendor choices, pricing and sync-engine internals. Never link to it, quote it or paraphrase its contents from `CONTEXT.md`, anything else under `docs/`, or a GitHub issue. If a public doc needs a decision that lives there, state the decision in product terms without the vendor or the price.

## Use the glossary's vocabulary

When your output names a domain concept (in an issue title, a refactor proposal, a hypothesis, a test name), use the term as defined in `CONTEXT.md`. Don't drift to synonyms the glossary explicitly avoids.

If the concept you need isn't in the glossary yet, that's a signal: either you're inventing language the project doesn't use (reconsider) or there's a real gap (note it for `/domain-modeling`).

## Flag ADR conflicts

If your output contradicts an existing ADR, surface it explicitly rather than silently overriding:

> _Contradicts ADR-0003 (per-machine libraries), but worth reopening because…_
