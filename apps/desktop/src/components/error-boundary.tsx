// The app's error boundary: an error a component throws while it renders goes to `onError`, which
// sends it as a crash report while the `telemetry` setting is on (src/lib/telemetry.ts), and the
// window then shows nothing, as it did before the boundary, when React took the whole tree down.
import { Component } from "react";
import type { ReactNode } from "react";

interface ErrorBoundaryProps {
  onError: (error: unknown) => void;
  children: ReactNode;
}

interface ErrorBoundaryState {
  failed: boolean;
}

export class ErrorBoundary extends Component<
  ErrorBoundaryProps,
  ErrorBoundaryState
> {
  constructor(props: ErrorBoundaryProps) {
    super(props);
    this.state = { failed: false };
  }

  static getDerivedStateFromError(): ErrorBoundaryState {
    return { failed: true };
  }

  override componentDidCatch(error: unknown) {
    this.props.onError(error);
  }

  override render(): ReactNode {
    return this.state.failed ? null : this.props.children;
  }
}
