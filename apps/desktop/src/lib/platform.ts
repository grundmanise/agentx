// The platform the app runs on, as the webview reports it: the one place that asks. The command
// field's key hint (⌘K or Ctrl K) and the file manager's name (Finder or the file manager) follow
// it. The tests' harness runs as Linux unless a test asks for macOS.

/** Whether the app runs on macOS. */
export const isMac =
  typeof navigator !== "undefined" && navigator.platform.includes("Mac");

/**
 * Whether the page runs inside the app's Tauri window, or under the end-to-end tests' mocked Rust
 * shell, rather than in a browser on serve's WebSocket door (ADR 0016). The modules that reach the
 * operating system (the transport, links, Finder, the editor, the open dialog) read it; a screen
 * never does.
 */
export const inApp = "__TAURI_INTERNALS__" in globalThis;
