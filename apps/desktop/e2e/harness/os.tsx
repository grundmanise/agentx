// The modules that reach the operating system, as a screen calls them, with the app's Toaster so
// the toasts they raise show. Loaded on a page with no Tauri runtime, it is the app in a browser.
import "#/styles/index.css";
import { Toaster } from "#/components/ui/sonner.tsx";
import { openInEditor, saveEditor } from "#/lib/editor.ts";
import { pickSnapshot } from "#/lib/file-dialog.ts";
import { openInBrowser } from "#/lib/open-url.ts";
import { revealInFinder } from "#/lib/reveal.ts";

import { mount } from "./mount.tsx";

mount(<Toaster />);
window.os = {
  openInBrowser,
  openInEditor,
  pickSnapshot,
  revealInFinder,
  saveEditor,
};
