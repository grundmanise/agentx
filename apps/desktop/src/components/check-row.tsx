import { cva } from "class-variance-authority";
import type { KeyboardEvent } from "react";

import { CopyIcon } from "#/components/icons/copy.tsx";
import type { Check, CheckStatus } from "#/startup/checks.ts";

const statusNames: Record<CheckStatus, string> = {
  fail: "Failed",
  pass: "Passed",
  running: "Running",
  waiting: "Waiting",
  warn: "Passed with a warning",
};

const markVariants = cva("size-check-mark block flex-none", {
  variants: {
    status: {
      fail: "fill-check-fail-bg stroke-check-fail-mark",
      pass: "fill-check-pass-bg stroke-check-pass-mark",
      running: "animate-check-spin",
      waiting: "fill-check-wait-bg",
      warn: "fill-check-pass-bg stroke-check-pass-mark",
    },
  },
});

/**
 * The status circle, one SVG: ✓ for pass and warn, "!" for fail, a dot while waiting, and a ring
 * that turns while running. Screen readers hear the status in words, before the label.
 */
const StatusMark = ({ status }: { status: CheckStatus }) => (
  <svg
    viewBox="0 0 20 20"
    aria-hidden
    focusable="false"
    className={markVariants({ status })}
  >
    {status === "running" ? (
      <>
        <circle
          cx="10"
          cy="10"
          r="9"
          fill="none"
          strokeWidth="1.5"
          className="stroke-check-wait-bg"
        />
        <path
          d="M10 1a9 9 0 0 1 9 9"
          fill="none"
          strokeWidth="1.5"
          strokeLinecap="round"
          className="stroke-check-pass-mark"
        />
      </>
    ) : (
      <circle cx="10" cy="10" r="10" stroke="none" />
    )}
    {(status === "pass" || status === "warn") && (
      <path
        d="M6.5 10.25l2.5 2.5 4.5-5"
        fill="none"
        strokeWidth="1.75"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    )}
    {status === "fail" && (
      <path
        d="M10 5.75v5M10 14.25v.01"
        fill="none"
        strokeWidth="2"
        strokeLinecap="round"
      />
    )}
    {status === "waiting" && (
      <circle cx="10" cy="10" r="2" className="fill-check-wait-mark" />
    )}
  </svg>
);

/**
 * The CLI's hint for a failed or warning check, exactly as it wrote it. The chip is a button that
 * copies it; with focus on it, ⌘C / Ctrl+C copies it too.
 */
const FixChip = ({
  hint,
  onCopy,
}: {
  hint: string;
  onCopy: (text: string) => void;
}) => (
  <button
    type="button"
    aria-label={`Copy fix: ${hint}`}
    onClick={() => {
      onCopy(hint);
    }}
    onKeyDown={(e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "c") {
        e.preventDefault();
        onCopy(hint);
      }
    }}
    className="rounded-check-fix border-check-fix-border bg-check-fix-bg text-text-secondary hover:text-text-primary mt-1.5 flex items-start gap-2 border py-1.5 pr-2 pl-2.5 text-left"
  >
    <code className="type-check-detail min-w-0 flex-1 wrap-break-word">
      {hint}
    </code>
    <span className="text-text-hint flex h-4.5 flex-none items-center">
      <CopyIcon size="xs" />
    </span>
  </button>
);

/** One startup check: its status circle, label, detail lines and the CLI's fix for each. */
export const CheckRow = ({
  check,
  onCopy,
}: {
  check: Check;
  onCopy: (text: string) => void;
}) => {
  const settled = check.status !== "waiting" && check.status !== "running";
  return (
    <li className="gap-check-mark flex">
      <StatusMark status={check.status} />
      <div className="flex min-w-0 flex-1 flex-col">
        <span
          className={
            settled
              ? "type-row-name text-check-label"
              : "type-row-name text-check-label-pending"
          }
        >
          <span className="sr-only">{statusNames[check.status]}: </span>
          {check.label}
        </span>
        {check.lines.map((line) => (
          <div key={line.text} className="mt-0.5 flex flex-col">
            <span
              className={
                check.status === "warn"
                  ? "type-check-detail text-tone-attention-fg wrap-break-word"
                  : "type-check-detail text-check-detail wrap-break-word"
              }
            >
              {line.text}
            </span>
            {line.hint !== undefined && settled && (
              <FixChip hint={line.hint} onCopy={onCopy} />
            )}
          </div>
        ))}
      </div>
    </li>
  );
};
