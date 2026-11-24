// A component that throws while it renders, under the app's error boundary, which hands the error
// to the app's crash report (src/lib/telemetry.ts) as the app's own boundary does. Its message
// names a user path, which the report must not carry.
import { ErrorBoundary } from "#/components/error-boundary.tsx";
import { reportError } from "#/lib/telemetry.ts";

import { mount } from "./mount.tsx";

export const thrownMessage =
  "open /Users/jane/.agentx/settings.json: permission denied";

const Thrower = () => {
  throw new Error(thrownMessage);
};

mount(
  <ErrorBoundary
    onError={(error) => {
      void reportError(error);
    }}
  >
    <Thrower />
  </ErrorBoundary>
);
