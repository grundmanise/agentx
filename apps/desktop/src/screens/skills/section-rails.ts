// The rails of the Skills list: one per group, and one per plugin in PLUGINS. A rail is the
// header's item in the list, placed by the list like any item. It holds a span that covers the
// whole section under the header, and the header, `position: sticky` inside the span, pins while
// the section shows; the next section pushes it out. The list places items absolutely, so the
// span takes its height from here: the list sizes the drawn spans from the offsets it already
// holds. The rail itself has no height, so sizing a span never resizes an item the list watches.
import type { Virtualizer } from "@tanstack/react-virtual";

/** Where an item is in the list: its offset from the top, and its end, in px. */
interface Measured {
  start: number;
  end: number;
}

/** The span of a rail, which holds its header. */
const spanOf = (rail: Element) => rail.firstElementChild;

/** Whether an element is a rail. */
export const isRail = (element: Element) =>
  element instanceof HTMLElement && element.dataset.rail !== undefined;

/** The height a rail takes in the list: its header's. */
export const railHeight = (rail: Element) =>
  Math.ceil(
    spanOf(rail)?.firstElementChild?.getBoundingClientRect().height ?? 0
  );

/**
 * The height of each drawn rail's span: from its header's offset to the end of its section's
 * last item. `sections` maps a header's index to the index of its section's last item.
 */
export const railSpans = (
  sections: ReadonlyMap<number, number>,
  measurements: readonly (Measured | undefined)[],
  drawn: readonly number[]
) => {
  const spans = new Map<number, number>();
  for (const index of drawn) {
    const last = sections.get(index);
    const header = measurements[index];
    const end = last === undefined ? undefined : measurements[last];
    if (header !== undefined && end !== undefined) {
      spans.set(index, end.end - header.start);
    }
  }
  return spans;
};

/** Sizes the drawn rails' spans to cover their sections; a span keeps a height that has not changed. */
export const sizeRails = (
  virtualizer: Virtualizer<HTMLElement, Element>,
  sections: ReadonlyMap<number, number>
) => {
  const drawn = virtualizer.getVirtualItems();
  const spans = railSpans(
    sections,
    virtualizer.measurementsCache,
    drawn.map((item) => item.index)
  );
  for (const item of drawn) {
    const height = spans.get(item.index);
    const rail = virtualizer.elementsCache.get(item.key);
    const span = rail === undefined ? null : spanOf(rail);
    if (
      height !== undefined &&
      span instanceof HTMLElement &&
      span.style.height !== `${height}px`
    ) {
      span.style.height = `${height}px`;
    }
  }
};

/**
 * A ref for a header in its rail: when the header changes height, such as when a control shows
 * in it, the list measures its rail again. The list watches the rail, which keeps no height.
 */
export const useRailHeader = (
  virtualizer: Virtualizer<HTMLElement, Element>
) => {
  // The compiler treats only a function that calls a hook as a hook; this one calls none.
  "use memo";
  const observer = new ResizeObserver((entries) => {
    for (const entry of entries) {
      const rail = entry.target.closest("[data-rail]");
      if (rail !== null) {
        virtualizer.measureElement(rail);
      }
    }
  });
  return (header: HTMLDivElement) => {
    observer.observe(header);
    return () => {
      observer.unobserve(header);
    };
  };
};
