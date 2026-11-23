<!-- NB: Follow the PR writing skill from this repo -->

<!--
PR Title: use the Conventional Commits format, such as `feat(skill): check managed skills for upstream updates`.
See CONTRIBUTING.md for the full conventions.
-->

## What has changed

<!-- In an imperative style, summarize what changed for users and why. Describe the implementation where it's needed to understand the change.
Optionally, link the issue it resolves, like "Closes #123". Skip all preambles and keep prose brief. -->

## Why

<!-- functional/non-functional requirement, e.g. business reason -->

## Evidence

<!-- Concrete evidence that the change works. Show it before and after with screenshots, a recording, or—for an execution-based change—the console output. -->

- **Before:** <!-- screenshot/output -->
- **After:** <!-- screenshot/output -->

## Merge danger
  
**Blast radius:** <!-- one-word description -->
<!-- The blast radius is the potential impact or scope of the changes introduced by this PR. Consider all possibilities. Examples are layout shift, breakages for consumers, mobile responsiveness, etc. -->

## Checklist

- [ ] Tests cover the change in behavior
- [ ] Outdated tests removed
- [ ] Help Center in `docs/help-center` reflects the change
- [ ] New terms match `GLOSSARY.md`, and any conflict with an ADR is explained above
