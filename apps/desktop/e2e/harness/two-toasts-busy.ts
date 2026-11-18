// Two toasts in a row while the page is busy: every timer set meanwhile fires 50ms late, as on a
// loaded machine, so the next animation frame comes before them. notify() must still replace the
// first toast with the second.
import { notify } from "#/components/toast.tsx";

const onTime = window.setTimeout;
const late = (handler: TimerHandler, timeout = 0, ...args: unknown[]) =>
  onTime(handler, timeout + 50, ...args);
Object.assign(window, { setTimeout: late });
notify("Added pdf to Cursor");
notify("Removed pdf from Cursor");
Object.assign(window, { setTimeout: onTime });
