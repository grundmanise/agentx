import { problemSentence } from "#/lib/problem-text.ts";
import type { Problem } from "#/store/store.ts";

/**
 * A CLI refusal under a field (DESIGN.md §6 "Refusal under a field"): its message and hint as
 * one sentence-cased paragraph in `type.notice` `tone.danger.fg`, announced as an alert.
 */
export const ProblemText = ({ problem }: { problem: Problem }) => (
  <p role="alert" className="type-notice text-tone-danger-fg m-0">
    {problemSentence(problem)}
  </p>
);
