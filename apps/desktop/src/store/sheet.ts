// The width the user dragged each sheet to, kept per sheet in localStorage, so a sheet opens at
// that width after the app restarts. A sheet with no kept width opens at `sheet.width`. The widths
// are read once, when the module loads, so a sheet opens at its width from the first frame.
// Anything unreadable is ignored: the sheet then opens at its default.

/** The sheets, each of which keeps its own width. */
export type SheetName = "activity-log" | "history" | "whats-new";

const KEY = "agentx.sheet-widths";

const read = (): Partial<Record<SheetName, number>> => {
  try {
    const parsed: unknown = JSON.parse(localStorage.getItem(KEY) ?? "{}");
    if (typeof parsed !== "object" || parsed === null) {
      return {};
    }
    return Object.fromEntries(
      Object.entries(parsed).filter(
        ([, width]) => typeof width === "number" && width > 0
      )
    );
  } catch {
    return {};
  }
};

const widths = read();

/** The width the user left `sheet` at, or null for its default. */
export const sheetWidth = (sheet: SheetName) => widths[sheet] ?? null;

/** Keeps the width the user dragged `sheet` to, or its default for null. */
export const keepSheetWidth = (sheet: SheetName, width: number | null) => {
  // JSON leaves out a sheet whose width is undefined.
  widths[sheet] = width ?? undefined;
  try {
    localStorage.setItem(KEY, JSON.stringify(widths));
  } catch {
    // Storage is full or blocked: the width holds until the app quits.
  }
};
