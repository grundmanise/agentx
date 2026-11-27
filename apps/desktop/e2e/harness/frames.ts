// What the page painted, frame by frame: for a test that checks that parts of the screen change in
// the same frame.
import type { Page } from "@playwright/test";

declare global {
  interface Window {
    paintedFrames?: string[];
  }
}

/**
 * Starts recording, at each frame the page paints, the text of the first element each selector of
 * `parts` matches, empty when none does, with `|` between them. A frame like the one before it is
 * left out.
 */
export const recordFrames = async (page: Page, parts: readonly string[]) => {
  await page.evaluate((selectors) => {
    const frames: string[] = [];
    window.paintedFrames = frames;
    const watch = () => {
      const frame = selectors
        .map((s) => document.querySelector(s)?.textContent.trim() ?? "")
        .join("|");
      if (frames.at(-1) !== frame) {
        frames.push(frame);
      }
      requestAnimationFrame(watch);
    };
    watch();
  }, parts);
};

/** The frames recorded so far. */
export const paintedFrames = async (page: Page) =>
  await page.evaluate(() => window.paintedFrames ?? []);
