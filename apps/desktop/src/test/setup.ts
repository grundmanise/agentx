import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach, vi } from "vitest";

afterEach(cleanup);

// jsdom lacks these; the palette list uses them.
globalThis.ResizeObserver ??= class {
  observe = vi.fn();
  unobserve = vi.fn();
  disconnect = vi.fn();
};
Element.prototype.scrollIntoView = () => {
  // jsdom does no layout, so there is nothing to scroll.
};
