// Runs the frontend in a browser against `agentx serve --listen` on this machine (ADR 0016).
//
//   node scripts/real.ts dev       the source, on Vite's development server
//   node scripts/real.ts preview   the built bundle, so that a bug only the build has shows too
//
// It starts Vite on the first free port from 1430 (dev) or 1431 (preview), so that several
// worktrees can run at once, then starts serve with that port's origins and reads the address
// and token from serve's `listening` event. A plugin answers GET /agentx-serve.json with them.
// The page asks for them when it loads, so the bundle holds no token and one build works with
// any serve.
// The CLI is built from apps/cli, the commit the frontend's generated event types come from;
// AGENTX_CLI names another executable instead. Serve uses this shell's environment, so
// AGENTX_HOME and HOME decide which home it serves.
import { spawn, spawnSync } from "node:child_process";
import { once } from "node:events";
import { mkdirSync } from "node:fs";
import path from "node:path";
import { createInterface } from "node:readline";

import { build, createServer, preview } from "vite";
import type { Connect, Plugin } from "vite";

/** Where the page asks for the address and token of serve's WebSocket door. */
const doorPath = "/agentx-serve.json";

/** What serve's `listening` event says, and what the page is answered. */
interface Door {
  url: string;
  token: string;
}

const ports = { dev: 1430, preview: 1431 } as const;
type Mode = keyof typeof ports;

const isMode = (value: string | undefined): value is Mode =>
  value === "dev" || value === "preview";

const appDir = path.join(import.meta.dirname, "..");
const cliDir = path.join(appDir, "..", "cli");

const fail = (message: string): never => {
  process.stderr.write(`real: ${message}\n`);
  process.exit(1);
};

/** The agentx to serve with: AGENTX_CLI, or one built from apps/cli. */
const cliPath = (): string => {
  const given = process.env.AGENTX_CLI;
  if (given !== undefined && given !== "") {
    return given;
  }
  const out = path.join(appDir, "node_modules", ".cache", "agentx", "agentx");
  mkdirSync(path.dirname(out), { recursive: true });
  const built = spawnSync("go", ["build", "-o", out, "."], {
    cwd: cliDir,
    stdio: "inherit",
  });
  if (built.error !== undefined) {
    fail(
      `cannot run go to build the CLI (${built.error.message}); set AGENTX_CLI to an agentx`
    );
  }
  if (built.status !== 0) {
    fail("go build of apps/cli failed");
  }
  return out;
};

/** A string field of an event, or "" when it has none. */
const field = (event: object, key: string): string => {
  const value: unknown = Reflect.get(event, key);
  return typeof value === "string" ? value : "";
};

/**
 * The door a line of serve's stdout names, when it is the `listening` event. An `error` event,
 * which serve writes when it refuses to start, is shown, since with --json serve says why on
 * stdout alone.
 */
const parseDoor = (line: string): Door | null => {
  let event: unknown = null;
  try {
    event = JSON.parse(line);
  } catch {
    return null;
  }
  if (typeof event !== "object" || event === null) {
    return null;
  }
  const type = field(event, "type");
  if (type === "error") {
    const hint = field(event, "hint");
    process.stderr.write(
      `agentx serve: ${field(event, "message")}\n${hint === "" ? "" : `  ${hint}\n`}`
    );
  }
  const [url, token] = [field(event, "url"), field(event, "token")];
  return type === "listening" && url !== "" && token !== ""
    ? { token, url }
    : null;
};

/**
 * Answers the page's request for the door, before Vite's fallback answers it with index.html.
 * Vite starts before serve, since serve needs Vite's port, so the door is set once serve listens.
 */
const doorPlugin = (): { plugin: Plugin; open: (door: Door) => void } => {
  let body = "";
  const answer: Connect.NextHandleFunction = (req, res, next) => {
    if (req.method !== "GET" || req.url?.split("?")[0] !== doorPath) {
      next();
      return;
    }
    res.setHeader("Cache-Control", "no-store");
    if (body === "") {
      res.statusCode = 503;
      res.end();
      return;
    }
    res.setHeader("Content-Type", "application/json");
    res.end(body);
  };
  return {
    open: (door) => {
      body = JSON.stringify(door);
    },
    plugin: {
      configurePreviewServer: (server) => {
        server.middlewares.use(answer);
      },
      configureServer: (server) => {
        server.middlewares.use(answer);
      },
      name: "agentx-serve-door",
    },
  };
};

const mode = process.argv.at(2);
if (!isMode(mode)) {
  process.stderr.write("usage: node scripts/real.ts dev|preview\n");
  process.exit(2);
}
const cli = cliPath();
if (mode === "preview") {
  await build({ root: appDir });
}

const door = doorPlugin();
const startServer = async () => {
  const options = { plugins: [door.plugin], root: appDir };
  const port = ports[mode];
  if (mode === "preview") {
    return await preview({
      ...options,
      preview: { port, strictPort: false },
    });
  }
  const dev = await createServer({
    ...options,
    server: { port, strictPort: false },
  });
  return await dev.listen();
};
const server = await startServer();
const address = server.httpServer?.address();
const port =
  typeof address === "object" && address !== null
    ? address.port
    : fail("Vite did not say which port it listens on");
const origins = [`http://localhost:${port}`, `http://127.0.0.1:${port}`];

// Serve ends when its standard input closes, so the pipe stays open while this script runs.
const serve = spawn(
  cli,
  [
    "serve",
    "--json",
    "--listen",
    "127.0.0.1:0",
    ...origins.flatMap((origin) => ["--origin", origin]),
  ],
  { stdio: ["pipe", "pipe", "inherit"] }
);
serve.on("error", (error) => {
  fail(`cannot start ${cli}: ${error.message}`);
});
const exited = once(serve, "exit");
const serveExit = async (): Promise<number> => {
  await exited;
  return serve.exitCode ?? 128;
};

// Serve writes the `listening` event before any other. Once it has, the lines after it are
// dropped unread, so that a full pipe never holds serve up.
const readDoor = async (): Promise<Door | null> => {
  for await (const line of createInterface({ input: serve.stdout })) {
    const found = parseDoor(line);
    if (found !== null) {
      return found;
    }
  }
  return null;
};
const listening = await readDoor();
if (listening === null) {
  // Exit code 6: another serve, such as the desktop app's, already runs for this home.
  await server.close();
  process.exit(await serveExit());
}
door.open(listening);
serve.stdout.resume();

server.printUrls();

let stopping = false;
const stop = async (code: number) => {
  if (stopping) {
    return;
  }
  stopping = true;
  await server.close();
  serve.stdin.end();
  process.exit(Math.max(code, await serveExit()));
};
process.on("SIGINT", () => {
  void stop(0);
});
process.on("SIGTERM", () => {
  void stop(0);
});

// Serve ending on its own leaves the page with nothing to talk to.
const code = await serveExit();
if (!stopping) {
  process.stderr.write(`real: agentx serve exited with code ${code}\n`);
  await stop(code === 0 ? 1 : code);
}
