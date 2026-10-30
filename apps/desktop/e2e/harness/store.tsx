// Renders the app store's applied inventory, freshness, pending refreshes and activity into the
// page, and puts the command runner on window. No screen shows the inventory yet, so the tests
// read the store and run commands through this module. The dev server serves the app's own store
// module at the URL the app uses, so this is the app's store.
import { refresh } from "#/store/feed.ts";
import { cancelCommand, runCommand } from "#/store/runner.ts";
import type { CommandOptions } from "#/store/runner.ts";
import { useAppStore } from "#/store/store.ts";
import type { AppState } from "#/store/store.ts";

import { mount } from "./mount.tsx";

declare global {
  interface Window {
    store: {
      run: (args: string[], options: CommandOptions) => void;
      cancel: (label: string) => Promise<void>;
      refresh: () => Promise<void>;
      /** The store's state now: for a test that checks that something did not change. */
      state: () => AppState;
    };
  }
}

const StoreView = () => {
  const { activity, applied, freshness, inventory, refreshes } = useAppStore();
  const rows: [string, string][] = [
    ["Instance", applied?.instanceId ?? ""],
    ["Counter", String(applied?.counter ?? "")],
    ["Agents", String(inventory?.configurations.length ?? "")],
    ["Freshness", freshness.kind],
    ["Pending refreshes", String(Object.keys(refreshes).length)],
  ];
  return (
    <>
      <dl aria-label="Store">
        {rows.map(([term, value]) => (
          <div key={term}>
            <dt>{term}</dt>
            <dd aria-label={term}>{value}</dd>
          </div>
        ))}
      </dl>
      <ol aria-label="Activity">
        {activity.map((entry) => (
          <li key={entry.id} data-tone={entry.tone}>
            {entry.message}
          </li>
        ))}
      </ol>
    </>
  );
};

mount(<StoreView />);

window.store = {
  cancel: cancelCommand,
  refresh,
  run: (args, options) => {
    void runCommand(args, options);
  },
  state: () => useAppStore.getState(),
};
