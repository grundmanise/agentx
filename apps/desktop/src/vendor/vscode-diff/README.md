# VS Code's diff

The diff VS Code's diff editor runs (`DefaultLinesDiffComputer`), copied from [microsoft/vscode](https://github.com/microsoft/vscode) so that the app depends on no package for it. The Code pane (`src/components/code-pane.tsx`) calls it through `index.ts` to mark the changed text of a change, as VS Code marks it.

- **Source:** `microsoft/vscode` at commit `a3cabfed1ada486415b1d3b4fff66bb2d47771ac` (2026-11-16), the files under `src/` at the same paths here, below `vs/`.
- **License:** MIT, Microsoft Corporation, in `LICENSE.txt`. Every file keeps its copyright header.
- **Checks:** `pnpm check` runs tsc, which checks these files. The lint and the formatter skip `src/vendor`, so that each file stays comparable with its source.

## Files

Copied as they are:

- `vs/base/common/charCode.ts`
- `vs/editor/common/core/position.ts`, `range.ts`, `ranges/lineRange.ts`, `ranges/offsetRange.ts`, `text/textLength.ts`
- `vs/editor/common/diff/linesDiffComputer.ts`
- `vs/editor/common/diff/defaultLinesDiffComputer/`: `algorithms/diffAlgorithm.ts`, `algorithms/dynamicProgrammingDiffing.ts`, `algorithms/myersDiffAlgorithm.ts`, `lineSequence.ts`, `linesSliceCharSequence.ts`, `utils.ts`

Changed, each change marked with an `agentx:` comment:

| File | Change |
| --- | --- |
| `vs/base/common/arrays.ts`, `arraysFind.ts`, `assert.ts` | Only the functions the diff calls, each as it is upstream. |
| `vs/base/common/errors.ts` | `BugIndicatingError` as it is upstream. `onUnexpectedError` is VS Code's default handler alone: it rethrows the error on a later task, without listeners or telemetry. |
| `vs/editor/common/core/text/abstractText.ts` | `AbstractText` keeps `getValueOfRange`, `length` and `getLineLength`; `LineBasedText` and `ArrayText` as upstream, without the `assert` of the line count. |
| `vs/editor/common/diff/rangeMapping.ts` | Removed the members built on `TextEdit` and on the legacy diff's `IChange`, which the diff never calls. |
| `vs/editor/common/diff/defaultLinesDiffComputer/defaultLinesDiffComputer.ts` | Removed move detection (`computeMovedLines.ts`). `options.computeMoves` has no effect, and `moves` is always empty. |
| `vs/editor/common/diff/defaultLinesDiffComputer/heuristicSequenceOptimizations.ts` | Renamed two parameters of `removeShortMatches` that it never reads, for this repo's `noUnusedParameters`. |

## Updating

1. Take the files above from a newer commit of `microsoft/vscode`, at the same paths.
2. Apply the changes in the table again, and keep the `agentx:` comments.
3. Run `npx tsc --noEmit -p .` and `pnpm test` in `apps/desktop`, and update the commit above.
