import { expect, test } from "@playwright/test";
import type { Page, WebSocketRoute } from "@playwright/test";

// The transport in a browser: no Tauri runtime, so it talks JSON-RPC to serve's WebSocket door
// (docs/spec/cli-contract.md, The WebSocket door). This file imports Playwright's own `test`, so no
// mocked Rust shell is installed; the test stands in for serve with a routed WebSocket, and for
// `pnpm dev:serve` by answering /agentx-serve.json. The page loads only the harness module that
// calls the transport and writes what it receives, as in transport.spec.ts.

const DOOR = { token: "t0k/en", url: "ws://127.0.0.1:4999/" };

/** What serve was sent, and the connections it was sent on, in order. */
interface Serve {
  connections: WebSocketRoute[];
  urls: string[];
  received: Record<string, unknown>[];
}

const startServe = async (
  page: Page,
  onOpen?: (ws: WebSocketRoute) => void
) => {
  const serve: Serve = { connections: [], received: [], urls: [] };
  await page.route("/agentx-serve.json", async (route) => {
    await route.fulfill({ json: DOOR });
  });
  await page.routeWebSocket(/^ws:\/\/127\.0\.0\.1:4999\//u, (ws) => {
    serve.connections.push(ws);
    serve.urls.push(ws.url());
    ws.onMessage((data) => {
      if (typeof data === "string") {
        const message: unknown = JSON.parse(data);
        if (typeof message === "object" && message !== null) {
          serve.received.push({ ...message });
        }
      }
    });
    onOpen?.(ws);
  });
  return serve;
};

const sendEvent = (ws: WebSocketRoute, params: Record<string, unknown>) => {
  ws.send(JSON.stringify({ jsonrpc: "2.0", method: "event", params }));
};

const answer = (ws: WebSocketRoute, id: unknown, result: unknown) => {
  ws.send(JSON.stringify({ id, jsonrpc: "2.0", result }));
};

const inventory = (counter: number) => ({
  instance_id: "i1",
  mcp_servers: [],
  plugins: [],
  scan_counter: counter,
  schema_version: 5,
  skills: [],
  type: "inventory",
});

const received = (page: Page) =>
  page.getByRole("list", { name: "Received" }).getByRole("listitem");

/** What the page received, each item as its kind and its parsed JSON. */
const items = async (page: Page) => {
  const texts = await received(page).allTextContents();
  return texts.map((text) => {
    const space = text.indexOf(" ");
    const value: unknown = JSON.parse(text.slice(space + 1));
    return { value, what: text.slice(0, space) };
  });
};

/** Opens a page that holds only the harness module, so the app does not launch. */
const openHarness = async (page: Page) => {
  await page.route("/", async (route) => {
    await route.fulfill({
      body: '<!doctype html><script type="module" src="/e2e/harness/transport.ts"></script>',
      contentType: "text/html",
    });
  });
  await page.goto("/");
  await page.waitForFunction(() => "transport" in window);
};

/** The requests serve received with this method. */
const sent = (serve: Serve, method: string) =>
  serve.received.filter((m) => m.method === method);

test("a run is a run request whose events stream before its result", async ({
  page,
}) => {
  const serve = await startServe(page);
  await openHarness(page);
  const runId = await page.evaluate(() => window.transport.run(["version"]));

  await expect.poll(() => sent(serve, "run")).toHaveLength(1);
  expect(serve.urls).toEqual([
    `${DOOR.url}?token=${encodeURIComponent(DOOR.token)}`,
  ]);
  expect(sent(serve, "run")).toEqual([
    {
      id: runId,
      jsonrpc: "2.0",
      method: "run",
      params: { args: ["version"], deadline_ms: null },
    },
  ]);
  const [ws] = serve.connections;
  if (ws === undefined) {
    throw new Error("no connection");
  }
  sendEvent(ws, {
    event: { cli_version: "1.2.0", schema_version: 5, type: "version" },
    id: runId,
    stream: "stdout",
  });
  await expect(received(page)).toHaveCount(1);

  sendEvent(ws, {
    event: { level: "info", message: "hi", schema_version: 5, type: "log" },
    id: runId,
    stream: "stderr",
  });
  sendEvent(ws, { id: runId, line: "not json", stream: "stdout" });
  answer(ws, runId, { cancelled: false, exit_code: 0, timed_out: false });

  await expect(received(page)).toHaveCount(4);
  expect(await items(page)).toEqual([
    {
      value: {
        event: { cli_version: "1.2.0", schema_version: 5, type: "version" },
        kind: "event",
      },
      what: "line",
    },
    {
      value: {
        event: { level: "info", message: "hi", schema_version: 5, type: "log" },
        kind: "log",
      },
      what: "line",
    },
    {
      value: { kind: "invalid", line: "not json", stream: "stdout" },
      what: "line",
    },
    {
      value: {
        cancelled: false,
        events: [{ cli_version: "1.2.0", schema_version: 5, type: "version" }],
        exitCode: 0,
        invalid: [{ line: "not json", stream: "stdout" }],
        logs: [
          { level: "info", message: "hi", schema_version: 5, type: "log" },
        ],
        timedOut: false,
      },
      what: "result",
    },
  ]);
});

test("cancel sends $/cancelRequest with the run's id", async ({ page }) => {
  const serve = await startServe(page);
  await openHarness(page);
  const runId = await page.evaluate(() => window.transport.run(["doctor"]));
  await expect.poll(() => sent(serve, "run")).toHaveLength(1);

  expect(
    await page.evaluate(async (id) => await window.transport.cancel(id), runId)
  ).toBe(true);
  await expect
    .poll(() => sent(serve, "$/cancelRequest"))
    .toEqual([
      { jsonrpc: "2.0", method: "$/cancelRequest", params: { id: runId } },
    ]);
  const [ws] = serve.connections;
  answer(ws, runId, {
    cancelled: true,
    exit_code: 9,
    timed_out: false,
  });
  await expect(received(page)).toHaveCount(1);
  expect(await items(page)).toEqual([
    {
      value: {
        cancelled: true,
        events: [],
        exitCode: 9,
        invalid: [],
        logs: [],
        timedOut: false,
      },
      what: "result",
    },
  ]);
  // A run that ended is no longer going.
  expect(
    await page.evaluate(async (id) => await window.transport.cancel(id), runId)
  ).toBe(false);
});

test("serve's start passes on its inventory, and refresh, search and listing answer as events", async ({
  page,
}) => {
  const serve = await startServe(page, (ws) => {
    // Serve's last inventory is the first message of every connection.
    sendEvent(ws, { event: inventory(3), stream: "stdout" });
  });
  await openHarness(page);

  expect(
    await page.evaluate(async () => await window.transport.serveStart())
  ).toBe(1);
  await expect(received(page)).toHaveCount(1);

  await page.evaluate(async () => {
    await window.transport.serveRequest({ request_id: "r1", type: "refresh" });
    await window.transport.serveRequest({
      query: "pdf",
      request_id: "s1",
      type: "search",
    });
    await window.transport.serveRequest({
      request_id: "l1",
      source: "https://github.com/acme/skills",
      type: "listing",
    });
    await window.transport.serveRequest({
      request_id: "l2",
      source: "https://github.com/acme/none",
      type: "listing",
    });
  });
  await expect
    .poll(() => serve.received)
    .toEqual([
      { id: "r1", jsonrpc: "2.0", method: "refresh" },
      { id: "s1", jsonrpc: "2.0", method: "search", params: { query: "pdf" } },
      {
        id: "l1",
        jsonrpc: "2.0",
        method: "listing",
        params: { source: "https://github.com/acme/skills" },
      },
      {
        id: "l2",
        jsonrpc: "2.0",
        method: "listing",
        params: { source: "https://github.com/acme/none" },
      },
    ]);
  const [ws] = serve.connections;
  if (ws === undefined) {
    throw new Error("no connection");
  }
  // A changed inventory reaches the client before the answer that names it.
  sendEvent(ws, { event: inventory(4), stream: "stdout" });
  answer(ws, "r1", { instance_id: "i1", scan_counter: 4 });
  answer(ws, "s1", { instance_id: "i1", query: "pdf", results: [] });
  answer(ws, "l1", {
    commit: "3f2a9c1",
    instance_id: "i1",
    skills: [],
    source: "https://github.com/acme/skills",
  });
  // Serve holds no listing of a URL that is no source: the event carries no skills.
  ws.send(
    JSON.stringify({
      error: { code: -32_000, message: "no listing" },
      id: "l2",
      jsonrpc: "2.0",
    })
  );
  sendEvent(ws, {
    event: {
      level: "warn",
      message: "careful",
      schema_version: 5,
      type: "log",
    },
    stream: "stderr",
  });

  await expect(received(page)).toHaveCount(7);
  expect(await items(page)).toEqual([
    {
      value: { childId: 1, event: inventory(3), kind: "event" },
      what: "serve",
    },
    {
      value: { childId: 1, event: inventory(4), kind: "event" },
      what: "serve",
    },
    {
      value: {
        childId: 1,
        event: {
          instance_id: "i1",
          ok: true,
          request_id: "r1",
          scan_counter: 4,
          schema_version: 5,
          type: "refresh_complete",
        },
        kind: "event",
      },
      what: "serve",
    },
    {
      value: {
        childId: 1,
        event: {
          instance_id: "i1",
          query: "pdf",
          request_id: "s1",
          results: [],
          schema_version: 5,
          type: "search",
        },
        kind: "event",
      },
      what: "serve",
    },
    {
      value: {
        childId: 1,
        event: {
          commit: "3f2a9c1",
          instance_id: "i1",
          request_id: "l1",
          schema_version: 5,
          skills: [],
          source: "https://github.com/acme/skills",
          type: "listing",
        },
        kind: "event",
      },
      what: "serve",
    },
    {
      value: {
        childId: 1,
        event: {
          instance_id: "i1",
          request_id: "l2",
          schema_version: 5,
          source: "https://github.com/acme/none",
          type: "listing",
        },
        kind: "event",
      },
      what: "serve",
    },
    {
      value: {
        childId: 1,
        event: {
          level: "warn",
          message: "careful",
          schema_version: 5,
          type: "log",
        },
        kind: "log",
      },
      what: "serve",
    },
  ]);
});

test("a refresh whose scan failed is a refresh_complete that is not ok", async ({
  page,
}) => {
  const serve = await startServe(page, (ws) => {
    sendEvent(ws, { event: inventory(1), stream: "stdout" });
  });
  await openHarness(page);
  await page.evaluate(async () => {
    await window.transport.serveStart();
    await window.transport.serveRequest({ request_id: "r1", type: "refresh" });
  });
  await expect.poll(() => sent(serve, "refresh")).toHaveLength(1);
  const [ws] = serve.connections;
  ws?.send(
    JSON.stringify({
      error: { code: -32_000, message: "the scan failed: disk full" },
      id: "r1",
      jsonrpc: "2.0",
    })
  );

  await expect(received(page)).toHaveCount(2);
  const [, failed] = await items(page);
  expect(failed?.value).toEqual({
    childId: 1,
    event: {
      error: "the scan failed: disk full",
      instance_id: "i1",
      ok: false,
      request_id: "r1",
      schema_version: 5,
      type: "refresh_complete",
    },
    kind: "event",
  });
});

test("a closed connection is a serve exit, and the next connection is a new child", async ({
  page,
}) => {
  const serve = await startServe(page, (ws) => {
    sendEvent(ws, { event: inventory(1), stream: "stdout" });
  });
  await openHarness(page);
  await page.evaluate(async () => await window.transport.serveStart());
  await expect(received(page)).toHaveCount(1);
  const runId = await page.evaluate(() => window.transport.run(["doctor"]));
  await expect.poll(() => sent(serve, "run")).toHaveLength(1);

  await serve.connections[0]?.close({ code: 1011 });

  // The run's connection closed, so serve stopped the command: the run did not end on its own.
  await expect(received(page)).toHaveCount(4);
  const [first, ...rest] = await items(page);
  expect(first).toMatchObject({ value: { childId: 1 }, what: "serve" });
  expect(rest).toEqual(
    expect.arrayContaining([
      {
        value:
          "Error: the connection to agentx serve closed with code 1011, which stopped the command",
        what: "error",
      },
      {
        value: {
          childId: 1,
          exitCode: null,
          restartInMs: 500,
          restarting: true,
          stderr: ["the connection to agentx serve closed with code 1011"],
        },
        what: "exit",
      },
      {
        value: { childId: 2, event: inventory(1), kind: "event" },
        what: "serve",
      },
    ])
  );
  expect(serve.connections).toHaveLength(2);
  expect(runId).not.toBe("");
});

test("with no serve for the page, a run fails to start", async ({ page }) => {
  await page.route("/agentx-serve.json", async (route) => {
    await route.fulfill({ status: 404 });
  });
  await openHarness(page);
  await page.evaluate(() => window.transport.run(["version"]));

  await expect(received(page)).toHaveCount(1);
  expect(await received(page).textContent()).toContain(
    "no agentx serve answers this page"
  );
});
