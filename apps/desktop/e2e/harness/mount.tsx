// Modules in this folder are loaded into the dev server's page by the tests
// (page.addScriptTag), never bundled. Each one renders or does one thing on load.
import type { ReactNode } from "react";
import { createRoot } from "react-dom/client";

/** Renders `node` on its own, outside the app shell, in an element with test id "harness". */
export const mount = (node: ReactNode) => {
  const container = document.createElement("div");
  container.dataset.testid = "harness";
  document.body.append(container);
  createRoot(container).render(node);
};
