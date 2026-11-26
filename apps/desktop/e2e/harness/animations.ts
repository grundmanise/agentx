// Waits for the animations an element runs, such as a sheet's or a popover's opening, to end, so
// its boxes are its own.
import type { Locator } from "@playwright/test";

export const settled = async (element: Locator) => {
  await element.evaluate(async (el) => {
    await Promise.all(
      el.getAnimations().map(async (a) => {
        await a.finished;
      })
    );
  });
};
