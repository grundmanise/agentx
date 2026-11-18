// The screens stay as the user left them: each screen visited stays mounted, hidden while
// another one shows, with its state, its selection and where each of its panes is scrolled: the
// browser keeps a hidden pane's scroll, WebKit as Chromium. A
// hidden screen keeps the route it last showed, so its selection stays, and the sidebar and the
// palette go back to that route.
import { Activity, useLayoutEffect } from "react";
import type { ReactNode } from "react";
import { Route, Routes, useLocation } from "react-router";
import type { Location } from "react-router";
import { create } from "zustand";
import { useShallow } from "zustand/react/shallow";

import { allScreens } from "#/app/screens.ts";

/** The screen whose path `pathname` is, or is under. */
export const screenOf = (pathname: string) =>
  allScreens.find(
    (s) => pathname === s.path || pathname.startsWith(`${s.path}/`)
  );

/** The location each screen last showed, by the screen's path: what a hidden screen keeps
 * showing, and what going back to it opens. */
const useLastLocations = create<Record<string, Location>>()(() => ({}));

const routeOf = (path: string, at: Location | undefined) =>
  at === undefined ? path : `${at.pathname}${at.search}`;

/** The route that opens the screen at `path` as the user left it: its own path until it shows. */
export const useLastRoute = (path: string) =>
  useLastLocations((locations) => routeOf(path, locations[path]));

/** The same, read outside React, as the palette does when an item runs. */
export const lastRoute = (path: string) =>
  routeOf(path, useLastLocations.getState()[path]);

/** Every screen visited so far, the current route's shown and the others hidden. */
export const KeptScreens = ({
  elementOf,
}: {
  /** The element of the screen at a path. */
  elementOf: (path: string) => ReactNode;
}) => {
  const location = useLocation();
  const current = screenOf(location.pathname);
  // The current screen shows the current location; the others, the one each last showed. Noting
  // the current screen's location changes none of these, so it renders nothing again.
  const kept = useLastLocations(
    useShallow((locations) =>
      allScreens.map((s) =>
        s.path === current?.path ? location : locations[s.path]
      )
    )
  );
  useLayoutEffect(() => {
    if (current !== undefined) {
      useLastLocations.setState({ [current.path]: location });
    }
  }, [current, location]);

  if (current === undefined) {
    return null;
  }
  return allScreens.map((s, i) => {
    const at = kept[i];
    return at === undefined ? null : (
      <Activity key={s.path} mode={s === current ? "visible" : "hidden"}>
        <Routes location={at}>
          <Route path={`${s.path}/*`} element={elementOf(s.path)} />
        </Routes>
      </Activity>
    );
  });
};
