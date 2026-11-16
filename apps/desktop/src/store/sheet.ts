// The width the user dragged each sheet to, kept per sheet in localStorage, so a sheet opens at
// that width after the app restarts. A sheet with no kept width opens at `sheet.width`. The read is
// synchronous, so a sheet opens at its width from the first frame. Anything unreadable is ignored:
// the sheet then opens at its default.
import { create } from "zustand";

/** The sheets, each of which keeps its own width. */
export type SheetName = "activity-log" | "history" | "whats-new";

type Widths = Partial<Record<SheetName, number>>;

const KEY = "agentx.sheet-widths";

const read = (): Widths => {
  try {
    const item = localStorage.getItem(KEY);
    const parsed: unknown = item === null ? null : JSON.parse(item);
    if (typeof parsed !== "object" || parsed === null) {
      return {};
    }
    return Object.fromEntries(
      Object.entries(parsed).filter(
        ([, width]) => typeof width === "number" && Number.isFinite(width)
      )
    );
  } catch {
    return {};
  }
};

const write = (widths: Widths) => {
  try {
    localStorage.setItem(KEY, JSON.stringify(widths));
  } catch {
    // Storage is full or blocked: the width holds until the app quits.
  }
};

export const useSheetWidths = create<Widths>()(() => read());

/** Keeps the width the user dragged `sheet` to, or its default for null. */
export const setSheetWidth = (sheet: SheetName, width: number | null) => {
  const { [sheet]: _, ...rest } = useSheetWidths.getState();
  const next = width === null ? rest : { ...rest, [sheet]: width };
  useSheetWidths.setState(next, true);
  write(next);
};
