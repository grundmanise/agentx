import { useEffect, useState } from "react";

import { CheckIcon } from "#/components/icons/check.tsx";
import { CopyIcon } from "#/components/icons/copy.tsx";
import { notifyFailure } from "#/components/toast.tsx";
import { Button } from "#/components/ui/button.tsx";
import type { Tracked } from "#/lib/track.ts";

/** How long the button shows its check after a copy, in ms. */
const COPIED_MS = 2000;

/**
 * A ghost button that copies text to the clipboard (DESIGN.md §6 "Copy button"): the copy icon,
 * then its label. Once the text is copied, the icon is a check in `tone.primary.fg` for 2s, and a
 * screen reader hears Copied; no toast shows. A copy that fails is an error toast.
 */
export const CopyButton = ({
  label,
  text,
  size = "default",
  disabled = false,
  track,
}: Tracked & {
  label: string;
  /** The text to copy, read when the button is clicked. */
  text: () => string;
  size?: "default" | "sm";
  disabled?: boolean;
}) => {
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    const timer = copied
      ? setTimeout(() => {
          setCopied(false);
        }, COPIED_MS)
      : undefined;
    return () => {
      clearTimeout(timer);
    };
  }, [copied]);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text());
      setCopied(true);
    } catch {
      notifyFailure("Couldn't copy to the clipboard");
    }
  };
  return (
    <>
      <Button
        variant="ghost"
        size={size}
        disabled={disabled}
        track={track}
        onClick={() => {
          void copy();
        }}
      >
        {copied ? (
          <span className="text-tone-primary-fg">
            <CheckIcon />
          </span>
        ) : (
          <CopyIcon />
        )}
        {label}
      </Button>
      <output className="sr-only">{copied ? "Copied" : ""}</output>
    </>
  );
};
