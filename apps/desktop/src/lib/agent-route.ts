// Opening a screen filtered to one agent, as the counts of the Agents page do. The route's state
// names the agent; the screen sets its agent filter to it once for each such navigation.
import { useState } from "react";
import { useLocation } from "react-router";

import { navigateTo } from "#/lib/navigate.ts";

interface AgentState {
  agent: string;
}

/** The agent the route's state names, or undefined. */
const agentOf = (state: unknown): string | undefined =>
  typeof state === "object" &&
  state !== null &&
  "agent" in state &&
  typeof state.agent === "string"
    ? state.agent
    : undefined;

/** Shows the screen at `path` filtered to `agent`: a configuration id, or the Skills screen's Universal. */
export const openForAgent = (path: string, agent: string) => {
  navigateTo(path, { agent } satisfies AgentState);
};

/** Calls `apply` with the agent the route names, once for each navigation that names one. */
export const useRouteAgent = (apply: (agent: string) => void) => {
  const location = useLocation();
  const [seen, setSeen] = useState<string>();
  if (location.key !== seen) {
    setSeen(location.key);
    const agent = agentOf(location.state);
    if (agent !== undefined) {
      apply(agent);
    }
  }
};
