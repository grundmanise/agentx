// Navigation from outside React, as the command runner needs on exit code 4. The app shell hands
// its navigate function here when it mounts.
import type { NavigateFunction } from "react-router";

let navigate: NavigateFunction | undefined;

export const setNavigate = (fn: NavigateFunction) => {
  navigate = fn;
};

/** Goes to `path`, with the route's `state` when given, once the app shell has mounted. */
export const navigateTo = (path: string, state?: unknown) => {
  void navigate?.(path, state === undefined ? undefined : { state });
};
