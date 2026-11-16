// The width of the sheet at the right edge. Every sheet (History, What's new, Activity log) opens
// at the width the user last dragged any of them to, kept in memory until the app quits.
import { create } from "zustand";

interface SheetWidth {
  /** The width the user dragged a sheet to, in pixels; null for `sheet.width`. */
  width: number | null;
}

export const useSheetWidth = create<SheetWidth>()(() => ({ width: null }));

/** Keeps the width the user dragged a sheet to, or its default for null. */
export const setSheetWidth = (width: number | null) => {
  useSheetWidth.setState({ width });
};
