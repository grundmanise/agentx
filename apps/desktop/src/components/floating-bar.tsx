import { useState } from "react";
import type { ReactNode } from "react";

/** The thin rule between the groups of a floating bar. */
export const FloatingBarDivider = () => (
  <span aria-hidden className="bg-shell-divider h-6 w-px flex-none" />
);

const sameValues = <P extends object>(a: P, b: P) =>
  Object.keys(b).length === Object.keys(a).length &&
  Object.entries(b).every(([key, value]) =>
    Object.is(Reflect.get(a, key), value)
  );

/**
 * The floating action bar (DESIGN.md §2.2 "Overlays", §4 "Overlays"): a toolbar over the bottom
 * of the screen while `shown`, sliding up from below the window's edge, and through its slide
 * back down. While it leaves it draws what it last drew, from the `values` it last had, so a
 * count does not drop to 0 on the way out.
 */
export const FloatingBar = <P extends object>({
  shown,
  label,
  values,
  children,
}: {
  shown: boolean;
  /** The toolbar's name for a screen reader. */
  label: string;
  /** What the bar draws from. */
  values: P;
  children: (values: P) => ReactNode;
}) => {
  const [last, setLast] = useState(values);
  const [mounted, setMounted] = useState(shown);
  // Kept while shown, to draw on the way out. `values` is new each render, so compare what it
  // holds, or each render would set it again.
  if (shown && !sameValues(last, values)) {
    setLast(values);
  }
  if (shown && !mounted) {
    setMounted(true);
  }
  if (!mounted) {
    return null;
  }
  const leaving = !shown;
  return (
    <div
      role="toolbar"
      aria-label={label}
      data-leaving={leaving ? "" : undefined}
      onAnimationEnd={(event) => {
        // Its own slide, not one of its children's animations.
        if (leaving && event.target === event.currentTarget) {
          setMounted(false);
        }
      }}
      className="bottom-floating-bar animate-floating-bar data-leaving:animate-floating-bar-out rounded-floating-bar border-overlay-border bg-overlay-bg shadow-popover h-floating-bar absolute left-1/2 z-30 flex -translate-x-1/2 items-center gap-3 border pr-2 pl-4 data-leaving:pointer-events-none"
    >
      {children(leaving ? last : values)}
    </div>
  );
};
