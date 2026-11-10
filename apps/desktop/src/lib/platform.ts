// The platform the app runs on, as the webview reports it: the one place that asks. The command
// field's key hint (⌘K or Ctrl K) and the file manager's name (Finder or the file manager) follow
// it. The tests' harness runs as Linux unless a test asks for macOS.

/** Whether the app runs on macOS. */
export const isMac =
  typeof navigator !== "undefined" && navigator.platform.includes("Mac");
