// Which group header, and which plugin's header, the Skills list pins at its top for a scroll
// offset, and how far the next header pushes them up. A pure function over the items' kinds and
// the offsets the list places them at, so the list reads it on every scroll.

/** What the pin reads of an item: whether it is a group's header, a plugin's header, or a row. */
export interface PinItem {
  kind: "header" | "subheader" | "row";
}

/** Where the list places an item: its offset from the top of the list, and its height, in px. */
export interface Span {
  start: number;
  size: number;
}

/** The heights of the pinned headers, in px: a group's, and a plugin's under it. */
export interface PinHeights {
  group: number;
  subgroup: number;
}

/** The headers pinned at the top of the list, by their index among the items. */
export interface Pin {
  header: number;
  /** The pinned plugin's header, in a group of plugins. */
  subheader?: number;
  /** How far the next group's header pushes the pinned headers up, in px: 0 or less. */
  groupShift: number;
  /** How far the next plugin's header pushes the pinned plugin's header up, in px: 0 or less. */
  subgroupShift: number;
}

/**
 * The headers pinned at the scroll `offset`: the last group header, and in a group of plugins the
 * last plugin header, that reached its pinned place. A header reaches it when its bottom meets the
 * bottom of the pinned header it replaces, so its caption lands where the pinned caption shows.
 * The next header pushes the pinned ones up as it comes under them. Nothing is pinned while the
 * first group's header is where the list places it.
 */
export const pinAt = (
  items: readonly PinItem[],
  spans: readonly (Span | undefined)[],
  offset: number,
  heights: PinHeights
): Pin | undefined => {
  let header = -1;
  let subheader = -1;
  let nextSubheader: number | undefined;
  let nextHeader: number | undefined;
  for (const [index, item] of items.entries()) {
    const span = spans[index];
    if (span === undefined || item.kind === "row") {
      continue;
    }
    const bottom = span.start + span.size;
    if (item.kind === "header") {
      if (header !== -1 && bottom - heights.group > offset) {
        nextHeader = span.start;
        break;
      }
      header = index;
      subheader = -1;
      nextSubheader = undefined;
    } else if (
      subheader === -1 ||
      (nextSubheader === undefined &&
        bottom - heights.group - heights.subgroup <= offset)
    ) {
      subheader = index;
    } else {
      nextSubheader ??= span.start;
    }
  }
  const first = spans[header];
  if (
    first === undefined ||
    offset <= first.start + first.size - heights.group
  ) {
    return undefined;
  }
  const height = heights.group + (subheader === -1 ? 0 : heights.subgroup);
  const push = (next: number | undefined) =>
    next === undefined ? 0 : Math.min(0, next - offset - height);
  return {
    groupShift: push(nextHeader),
    header,
    subgroupShift: push(nextSubheader),
    ...(subheader === -1 ? {} : { subheader }),
  };
};
