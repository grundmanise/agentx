// Renders the app store's applied inventory, freshness and pending refreshes, and the activity
// log's lines, into the page, and puts the command runner on window. No screen shows the inventory yet, so the tests
// read the store and run commands through this module. The dev server serves the app's own store
// module at the URL the app uses, so this is the app's store.
import { useActivityLog } from "#/store/activity-log.ts";
import { refresh } from "#/store/feed.ts";
import { cancelCommand, agentx } from "#/store/runner.ts";
import type { CommandOptions } from "#/store/runner.ts";
import { useAppStore } from "#/store/store.ts";
import type { AppState } from "#/store/store.ts";

import { mount } from "./mount.tsx";

declare global {
  interface Window {
    store: {
      run: (args: string[], options: CommandOptions) => void;
      cancel: (label: string) => Promise<void>;
      refresh: () => void;
      /** The store's state now: for a test that checks that something did not change. */
      state: () => AppState;
    };
  }
}

const StoreView = () => {
  const { applied, freshness, inventory, pending, refreshes } = useAppStore();
  const { lines } = useActivityLog();
  const rows: [string, string][] = [
    ["Instance", applied?.instanceId ?? ""],
    ["Counter", String(applied?.counter ?? "")],
    ["Agents", String(inventory?.configurations.length ?? "")],
    ["Freshness", freshness.kind],
    ["Pending refreshes", String(Object.keys(refreshes).length)],
    ["Running", Object.keys(pending).join(", ")],
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
      <ol aria-label="Activity log">
        {lines.map((line) => (
          <li key={line.id} data-tone={line.tone}>
            {line.text}
          </li>
        ))}
      </ol>
    </>
  );
};

mount(<StoreView />);

window.store = {
  cancel: cancelCommand,
  refresh: () => {
    void refresh();
  },
  run: (args, options) => {
    void agentx(args, options);
  },
  state: () => useAppStore.getState(),
};
