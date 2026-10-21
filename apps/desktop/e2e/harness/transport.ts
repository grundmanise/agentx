// Calls the app's transport module and writes what it receives into the page, one list item per
// line, result or serve exit, as JSON. Nothing in the app renders CLI data yet, so the tests drive
// the transport through the functions this module puts on window.
import type { ServeRequest } from "#/cli/events.gen.ts";
import { cancel, run, serve } from "#/cli/transport.ts";
import type { Run, RunLine } from "#/cli/transport.ts";

declare global {
  interface Window {
    transport: {
      run: (args: string[]) => string;
      runAndStopReading: (args: string[]) => string;
      cancel: (runId: string) => Promise<boolean>;
      serveStart: () => Promise<number>;
      serveRequest: (request: ServeRequest) => Promise<void>;
    };
  }
}

const received = document.createElement("ol");
received.setAttribute("aria-label", "Received");
document.body.append(received);

const write = (what: string, value: unknown) => {
  const item = document.createElement("li");
  item.textContent = `${what} ${JSON.stringify(value)}`;
  received.append(item);
};

const pump = async (reader: ReadableStreamDefaultReader<RunLine>) => {
  const { done, value } = await reader.read();
  if (!done) {
    write("line", value);
    await pump(reader);
  }
};

/** Writes each line of the run, then its result or why it failed. */
const follow = async (r: Run) => {
  try {
    const [, result] = await Promise.all([pump(r.lines.getReader()), r.result]);
    write("result", result);
  } catch (error) {
    write("error", String(error));
  }
};

/** Reads the first line of the run, cancels its stream, then writes its result. */
const stopReading = async (r: Run) => {
  try {
    const reader = r.lines.getReader();
    const first = await reader.read();
    write("line", first.value);
    await reader.cancel();
    write("result", await r.result);
  } catch (error) {
    write("error", String(error));
  }
};

const followed: Promise<void>[] = [];

await serve.onEvent((line) => {
  write("serve", line);
});
await serve.onExit((exit) => {
  write("exit", exit);
});

window.transport = {
  cancel,
  run: (args) => {
    const r = run(args);
    followed.push(follow(r));
    return r.runId;
  },
  runAndStopReading: (args) => {
    const r = run(args);
    followed.push(stopReading(r));
    return r.runId;
  },
  serveRequest: serve.request,
  serveStart: serve.start,
};
