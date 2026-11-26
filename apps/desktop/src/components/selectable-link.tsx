// A link whose text can be selected (DESIGN.md §6 "Values"): a click opens it, while a drag
// across it selects its text and the release that ends the drag opens nothing.
import { useRef } from "react";
import type { ReactNode } from "react";

import { trackAttribute } from "#/lib/track.ts";
import type { Tracked } from "#/lib/track.ts";

/**
 * Its look: `caption` as the origin beside a skill's name, `muted` under a source's name,
 * `secondary` as a value of Details, `inherit` in the colours of the text round it.
 */
const TONES = {
  caption: "text-text-caption hover:text-text-secondary hover:underline",
  inherit: "hover:underline",
  muted: "text-text-muted hover:underline",
  secondary: "text-text-secondary hover:text-text-primary hover:underline",
} as const;

/** How far the pointer may move between press and release for the release to count as a click. */
const CLICK_SLOP = 4;

export const SelectableLink = ({
  href,
  title,
  tone,
  onOpen,
  children,
  track,
}: Tracked & {
  href: string;
  title?: string;
  tone: keyof typeof TONES;
  /** Opens the link; the browser never follows `href`. */
  onOpen: () => void;
  children: ReactNode;
}) => {
  const pressedAt = useRef<{ x: number; y: number } | null>(null);
  return (
    <a
      href={href}
      {...trackAttribute(track)}
      title={title}
      draggable={false}
      className={TONES[tone]}
      onPointerDown={(e) => {
        pressedAt.current = { x: e.clientX, y: e.clientY };
      }}
      onClick={(e) => {
        e.preventDefault();
        const from = pressedAt.current;
        pressedAt.current = null;
        const dragged =
          from !== null &&
          Math.hypot(e.clientX - from.x, e.clientY - from.y) > CLICK_SLOP;
        if (!dragged) {
          onOpen();
        }
      }}
    >
      {children}
    </a>
  );
};
