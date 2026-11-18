// The screens stay as the user left them: each screen visited stays mounted, hidden while
// another one shows, with its state, its selection and where each of its panes is scrolled: the
// browser keeps a hidden pane's scroll, WebKit as Chromium. A
// hidden screen keeps the route it last showed, so its selection stays, and the sidebar and the
// palette go back to that route.
import { Activity, useLayoutEffect, useState } from "react";
import type { ReactNode } from "react";
import { Route, Routes, useLocation } from "react-router";
import type { Location } from "react-router";
import { create } from "zustand";

import { allScreens } from "#/app/screens.ts";

/** The screen whose path `pathname` is, or is under. */
export const screenOf = (pathname: string) =>
  allScreens.find(
    (s) => pathname === s.path || pathname.startsWith(`${s.path}/`)
  );

/** The route each screen last showed, by the screen's path: what going back to it opens. */
const useLastRoutes = create<Record<string, string>>()(() => ({}));

/** The route that opens the screen at `path` as the user left it: its own path until it shows. */
export const useLastRoute = (path: string) =>
  useLastRoutes((routes) => routes[path] ?? path);

/** The same, read outside React, as the palette does when an item runs. */
export const lastRoute = (path: string) =>
  useLastRoutes.getState()[path] ?? path;

/** The state of a navigation to a screen's last route: it goes back to the screen as the user left it. */
export const backState = { back: true };

/** Whether `location` goes back to its screen as the user left it, not to a route asked for anew. */
export const isBack = (location: Location) => {
  const state: unknown = location.state;
  return typeof state === "object" && state !== null && "back" in state;
};

/** Every screen visited so far, the current route's shown and the others hidden. */
export const KeptScreens = ({
  elementOf,
}: {
  /** The element of the screen at a path. */
  elementOf: (path: string) => ReactNode;
}) => {
  const location = useLocation();
  const current = screenOf(location.pathname);
  // The route each screen visited last showed, set while rendering so the screen shows with it.
  const [kept, setKept] = useState<Record<string, Location>>({});
  if (current !== undefined && kept[current.path] !== location) {
    setKept({ ...kept, [current.path]: location });
  }
  useLayoutEffect(() => {
    if (current !== undefined) {
      useLastRoutes.setState({
        [current.path]: `${location.pathname}${location.search}`,
      });
    }
  }, [current, location]);

  if (current === undefined) {
    return null;
  }
  return allScreens.map((s) => {
    const at = s.path === current.path ? location : kept[s.path];
    return at === undefined ? null : (
      <Activity key={s.path} mode={s === current ? "visible" : "hidden"}>
        <Routes location={at}>
          <Route path={`${s.path}/*`} element={elementOf(s.path)} />
        </Routes>
      </Activity>
    );
  });
};
