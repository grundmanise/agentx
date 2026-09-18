# Fork updates and account pulls

Status: accepted, 2026-09-19. This replaces inferred import ancestry in ADR 0003.

## Required behavior

Accepting upstream versions without custom content must not produce text merge conflicts between machines. Version or name choices are allowed when intentions are incompatible or source ancestry cannot be established. Do not manufacture ancestry from timestamps, import order or a missing-history fallback.

Before applying a change, verify fork identity and canonical source/subpath, capture pending edits and follow the mutation-safety contract. Ordinary pull refuses a different fork identity. Greenfield skills have no upstream base and use ordinary account history; they do not qualify for upstream version selection.

## Classifying content

Content is uncustomized only when its exact tree equals the recorded accepted upstream tree after applying the recorded agentx name override. The override changes only the name field through a defined, reproducible transformation. Other frontmatter edits, file modes, additions and deletions count as custom content. Clean Git status, no unpushed commits, or equality with some other upstream version are insufficient.

Record explicit name overrides and upstream rollback or branch-selection intent durably in branch history. They must survive publication, clone and restart and must not be inferred from the current bytes. Missing or ambiguous metadata disables automatic version selection and requires a version/name choice or metadata repair; it must not turn upstream-only differences into hunk conflicts.

## Updating from upstream

For uncustomized content, materialize the selected upstream version with the recorded name override. No text merge is needed, including for a deliberately selected older version or branch switch. Record that selection and its intent.

For custom content, merge using the recorded accepted upstream import as the explicit base. Preserve edits or surface real text conflicts. Do not use an arbitrary base from an account pull. Record the newly accepted import as the base after successful completion.

## Pulling the same fork from the account remote

Honor genuine account-history fast-forwards first, including explicit rollbacks. For divergent branches where both sides are uncustomized:

- Retain the same accepted upstream version when both sides name it.
- Select a proven descendant upstream version only when no competing rollback or branch-selection intent exists.
- For incomparable versions, unavailable ancestry or competing selection intent, ask which version to retain. Do not silently combine two upstream versions into new custom content.
- Preserve a compatible recorded name override. Ask which name to retain when explicit choices conflict.

Record the result as an ordinary two-parent account commit with the selected base, resolved name override and selection intent. A resolved choice must be recognized on later pulls rather than repeatedly requested. Both parent histories remain reachable.

When either side has actual custom content, use ordinary Git account ancestry and preserve customizations or report conflicts. Resolve the accepted upstream base separately: use proven ancestry and recorded selection intent, and require a version choice when those do not identify one base. A clean content merge is not proof that a later timestamp identifies the correct base. Keep custom merge results during this choice; selecting a base is not permission to replace them with upstream bytes.

## Implementation acceptance gates

The behavior is decided; the prototype is not production validation. Define versioned trailer encoding for explicit override absence, names, selection intent and resolved choices. Verify reconstructing those records from a fresh clone, including second-parent selections and repeated pulls. Validate exact name projection, source aliases, cross-subpath refusal, force-pushed history, file modes, binary files and symlinks. Revalidate live content before applying any choice. No automatic path may discard a customization or undo a recorded rollback silently.
