// Usage data (docs/help-center/telemetry.mdx): the telemetry dialog, asked while the `telemetry`
// setting has no value, the Usage data switch in Settings, both bound to `config set telemetry`,
// and the app's own events and crash reports. It runs in two projects (playwright.config.ts):
// `chromium`, whose dev server has no telemetry host, and `telemetry`, whose dev server has one,
// which these tests intercept and record.
import { gunzipSync } from "node:zlib";

import type { Page, Request } from "@playwright/test";

import type { Settings } from "#/cli/events.gen.ts";

import {
  appInfo,
  cliVersion,
  expect,
  firstInventory,
  machineLabel,
  test,
} from "./harness/cli.ts";
import type { InlineEvent, MockCli } from "./harness/cli.ts";
import { telemetryHost, telemetryKey } from "./harness/telemetry-host.ts";

const TELEMETRY_ID = "5d1c0a9e7f3b4c2a8e6d4f1b3a5c7e9d";

/**
 * The `settings` event of `config list`, with the `telemetry` setting as given, or none, and the
 * telemetry id while it is on, unless `id` is false, as under DO_NOT_TRACK.
 */
const settingsEvent = (telemetry?: "on" | "off", id = true): InlineEvent => {
  const settings: Settings = {
    accept_operations: false,
    copy_configurations: [],
    copy_mode: {},
    disabled_configurations: [],
    ignore_system_files: true,
    label: machineLabel,
    schema_version: 1,
    sources: [],
    ...(telemetry === undefined ? {} : { telemetry }),
  };
  return telemetry === "on" && id
    ? { settings, telemetry_id: TELEMETRY_ID, type: "settings" }
    : { settings, type: "settings" };
};

/** A home whose `telemetry` setting has no value. */
const unanswered = (cli: MockCli) => {
  cli.register(["config", "list"], { events: [settingsEvent()] });
};

const dialog = (page: Page) =>
  page.getByRole("alertdialog", { name: "Send anonymous usage data?" });

const usageSwitch = (page: Page) =>
  page.getByRole("switch", { name: "Send usage data" });

/** Registers `config set telemetry <value>`, and the settings it leaves. */
const answers = (cli: MockCli, value: "on" | "off") => {
  cli.register(["config", "set", "telemetry", value], {
    events: [settingsEvent(value)],
  });
  cli.register(["config", "list"], { events: [settingsEvent(value)] });
};

/** The `config set telemetry` lines the app ran, in order. */
const telemetrySets = (cli: MockCli) =>
  cli.commands
    .filter((l) => l.slice(0, 3).join(" ") === "config set telemetry")
    .map((l) => l.join(" "));

/** How many refresh requests the app wrote to serve's stdin. */
const refreshes = (cli: MockCli) =>
  cli.serveRequests.filter((r) => r.includes('"refresh"')).length;

const isLaunched = async (page: Page) => {
  await expect(page.getByRole("main")).toHaveAttribute("aria-busy", "false");
};

test.describe("a build without a host", () => {
  test.beforeEach(() => {
    test.skip(test.info().project.name !== "chromium");
  });

  test("never asks, with the setting unanswered", async ({ cli, page }) => {
    unanswered(cli);
    await page.goto("/");
    await isLaunched(page);
    await expect.poll(() => cli.commands.length).toBeGreaterThan(4);
    await expect(page.getByRole("link", { name: "Settings" })).toBeVisible();
    await expect(dialog(page)).toHaveCount(0);
  });

  test("the switch in Settings shows the setting and runs the same command", async ({
    cli,
    page,
  }) => {
    answers(cli, "off");
    answers(cli, "on");
    cli.register(["config", "list"], { events: [settingsEvent()] });
    await page.goto("/#/settings");
    const region = page.getByRole("region", {
      exact: true,
      name: "Usage data",
    });
    await expect(region).toBeVisible();
    // No value yet: nothing is sent, so the switch is off.
    await expect(usageSwitch(page)).not.toBeChecked();

    cli.register(["config", "list"], { events: [settingsEvent("on")] });
    await usageSwitch(page).click();
    await expect(usageSwitch(page)).toBeChecked();

    cli.register(["config", "list"], { events: [settingsEvent("off")] });
    await usageSwitch(page).click();
    await expect(usageSwitch(page)).not.toBeChecked();
    expect(telemetrySets(cli)).toEqual([
      "config set telemetry on --json",
      "config set telemetry off --json",
    ]);
    // The app writes no file: every call went to serve, the supervisor or the opener.
    expect(
      cli.calls.filter((c) => c.startsWith("plugin:fs") || c.includes("write"))
    ).toEqual([]);
  });

  test("a build without a host never loads the SDK, with the setting on", async ({
    cli,
    page,
  }) => {
    const elsewhere: string[] = [];
    page.on("request", (r) => {
      if (
        !r.url().startsWith("http://localhost:1420/") ||
        r.url().includes("posthog-js")
      ) {
        elsewhere.push(r.url());
      }
    });
    cli.register(["config", "list"], { events: [settingsEvent("on")] });
    await page.goto("/");
    await isLaunched(page);
    await expect.poll(() => cli.commands.length).toBeGreaterThan(4);
    await page.getByRole("link", { name: "Settings" }).click();
    await expect(usageSwitch(page)).toBeChecked();
    expect(elsewhere).toEqual([]);
  });
});

/** One event as the SDK sends it. */
interface SentEvent {
  event: string;
  properties: Record<string, unknown>;
}

/** What reached the telemetry host: each request's path and its events. */
interface Sent {
  paths: string[];
  events: SentEvent[];
}

const isSentEvent = (value: unknown): value is SentEvent =>
  typeof value === "object" &&
  value !== null &&
  typeof Reflect.get(value, "event") === "string" &&
  typeof Reflect.get(value, "properties") === "object";

/** The events of a batch the SDK posted with the test's key. */
const eventsOf = (request: Request): SentEvent[] => {
  const raw = request.postDataBuffer();
  if (raw === null) {
    return [];
  }
  // The SDK gzips its batches.
  const body: unknown = JSON.parse(
    (raw[0] === 0x1f ? gunzipSync(raw) : raw).toString()
  );
  if (
    typeof body !== "object" ||
    body === null ||
    Reflect.get(body, "api_key") !== telemetryKey
  ) {
    return [];
  }
  const batch: unknown = Reflect.get(body, "batch");
  return Array.isArray(batch) ? batch.filter(isSentEvent) : [];
};

/** Intercepts the telemetry host, answering 200, and records what reaches it. */
const intercept = async (page: Page): Promise<Sent> => {
  const sent: Sent = { events: [], paths: [] };
  await page.route(`${telemetryHost}/**`, async (route) => {
    const request = route.request();
    sent.paths.push(new URL(request.url()).pathname);
    sent.events.push(...eventsOf(request));
    await route.fulfill({ body: "{}", contentType: "application/json" });
  });
  return sent;
};

/** The values of an exception event's exceptions, joined. */
const exceptionValue = (event: SentEvent): string => {
  const list: unknown = event.properties.$exception_list;
  return Array.isArray(list)
    ? list
        .map((x: unknown) =>
          typeof x === "object" && x !== null
            ? String(Reflect.get(x, "value"))
            : ""
        )
        .join(",")
    : "";
};

const named = (sent: Sent, name: string) =>
  sent.events.filter((e) => e.event === name);

/** The properties the app sets itself, which do not start with `$`, by name. */
const ownProperties = (event: SentEvent) => {
  const own = Object.keys(event.properties).filter(
    (k) => !k.startsWith("$") && k !== "distinct_id" && k !== "token"
  );
  own.sort((a, b) => a.localeCompare(b));
  return own;
};

/** Turns the setting on in Settings, which starts the SDK, or off. */
const flip = async (cli: MockCli, page: Page, value: "on" | "off") => {
  answers(cli, value);
  await usageSwitch(page).click();
  await expect(usageSwitch(page)).toBeChecked({ checked: value === "on" });
};

test.describe("the question", () => {
  test.beforeEach(() => {
    test.skip(test.info().project.name !== "telemetry");
  });

  test("asks once the app has started, while the setting has no value", async ({
    cli,
    page,
  }) => {
    unanswered(cli);
    await page.goto("/");
    await expect(dialog(page)).toBeVisible();
    // Under it, the screens the first inventory filled, out of reach while it asks.
    await expect(page.locator("main")).toHaveAttribute("aria-busy", "false");

    const fields = dialog(page).getByRole("list", { name: "What is sent" });
    await expect(fields.getByRole("listitem")).toHaveText([
      /^telemetry_id /u,
      /^cli_version /u,
      /^schema_version /u,
      /^os, arch /u,
      /^command the command words only, without arguments or flag values$/u,
      /^exit_code /u,
      /^duration_ms /u,
      /^json /u,
      /^at /u,
    ]);
    await expect(dialog(page)).toContainText(
      "A crash sends the error type, its message and a stack trace, with paths removed."
    );
    // "Don't send" is the default: it has the focus.
    await expect(
      dialog(page).getByRole("button", { name: "Don't send" })
    ).toBeFocused();

    await dialog(page).getByRole("button", { name: "What is sent" }).click();
    expect(cli.openedUrls).toEqual(["https://docs.agentx.wtf/telemetry"]);
  });

  test("a setting with a value asks nothing", async ({ cli, page }) => {
    answers(cli, "off");
    await page.goto("/");
    await isLaunched(page);
    await expect.poll(() => cli.commands.length).toBeGreaterThan(4);
    await expect(page.getByRole("link", { name: "Settings" })).toBeVisible();
    await expect(dialog(page)).toHaveCount(0);
  });

  test("Send runs config set telemetry on, then a refresh, and asks no more", async ({
    cli,
    page,
  }) => {
    unanswered(cli);
    cli.register(["config", "set", "telemetry", "on"], {
      events: [settingsEvent("on")],
    });
    await page.goto("/");
    await expect(dialog(page)).toBeVisible();
    const before = refreshes(cli);

    await dialog(page)
      .getByRole("button", { exact: true, name: "Send" })
      .click();
    await expect(dialog(page)).toHaveCount(0);
    await expect
      .poll(() => telemetrySets(cli))
      .toEqual(["config set telemetry on --json"]);
    await expect.poll(() => refreshes(cli)).toBe(before + 1);

    // The refresh still finds no value, as if the command failed: the answer stands this session.
    await cli.serveEmit([{ ...firstInventory(), scan_counter: 2 }]);
    await expect(dialog(page)).toHaveCount(0);
  });

  test("Don't send, and Escape, run config set telemetry off", async ({
    cli,
    page,
  }) => {
    unanswered(cli);
    cli.register(["config", "set", "telemetry", "off"], {
      events: [settingsEvent("off")],
    });
    await page.goto("/");
    await dialog(page).getByRole("button", { name: "Don't send" }).click();
    await expect
      .poll(() => telemetrySets(cli))
      .toEqual(["config set telemetry off --json"]);

    cli.restart();
    unanswered(cli);
    await page.reload();
    await expect(dialog(page)).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(dialog(page)).toHaveCount(0);
    await expect
      .poll(() => telemetrySets(cli))
      .toEqual([
        "config set telemetry off --json",
        "config set telemetry off --json",
      ]);
  });
});

test.describe("what the app sends", () => {
  test.beforeEach(() => {
    test.skip(test.info().project.name !== "telemetry");
  });

  test("one app_open per launch, under the CLI's id, with the app's fields alone", async ({
    cli,
    page,
  }) => {
    const sent = await intercept(page);
    cli.register(["config", "list"], { events: [settingsEvent("on")] });
    await page.goto("/");
    await isLaunched(page);
    await expect.poll(() => named(sent, "app_open").length).toBe(1);

    const [open] = named(sent, "app_open");
    expect(ownProperties(open)).toEqual([
      "app_version",
      "arch",
      "cli_version",
      "os",
    ]);
    expect(open.properties).toMatchObject({
      app_version: appInfo.version,
      arch: appInfo.arch,
      cli_version: cliVersion,
      distinct_id: TELEMETRY_ID,
      os: appInfo.os,
    });

    // Clicks and screens capture nothing: the next batch holds only an error thrown after them.
    await page.getByRole("link", { name: "Settings" }).click();
    await page.getByRole("link", { name: "Skills" }).click();
    await page.evaluate(() => {
      setTimeout(() => {
        throw new Error("after the clicks");
      }, 0);
    });
    await expect.poll(() => named(sent, "$exception").length).toBe(1);
    expect(sent.events.map((e) => e.event)).toEqual(["app_open", "$exception"]);
    // No feature flag, configuration or recording request: only batches of events.
    expect(new Set(sent.paths)).toEqual(new Set(["/e/"]));
    for (const event of sent.events) {
      for (const page_property of [
        "$current_url",
        "$host",
        "$pathname",
        "$referrer",
      ]) {
        expect(event.properties).not.toHaveProperty(page_property);
      }
      expect(event.properties.$process_person_profile).toBe(false);
    }
    // The SDK keeps nothing on the machine: the only stored keys are the app's own.
    const stored = await page.evaluate(() => Object.keys(localStorage));
    expect(stored.filter((k) => !k.startsWith("agentx."))).toEqual([]);
    expect(await page.context().cookies()).toEqual([]);

    // A new launch sends its own app_open.
    cli.restart();
    await page.reload();
    await expect.poll(() => named(sent, "app_open").length).toBe(2);
  });

  test("errors are sent with user paths removed", async ({ cli, page }) => {
    const sent = await intercept(page);
    cli.register(["config", "list"], { events: [settingsEvent("on")] });
    await page.goto("/");
    await expect.poll(() => named(sent, "app_open").length).toBe(1);

    await page.addScriptTag({
      type: "module",
      url: "/e2e/harness/throw-in-boundary.tsx",
    });
    await page.evaluate(() => {
      void Promise.reject(new Error("read /home/jane/notes failed"));
    });
    await expect.poll(() => named(sent, "$exception").length).toBe(2);

    const values = named(sent, "$exception").map(exceptionValue);
    values.sort((a, b) => a.localeCompare(b));
    expect(values).toEqual([
      "open <path>: permission denied",
      "read <path> failed",
    ]);
    const bodies = JSON.stringify(sent.events);
    expect(bodies).not.toContain("/Users/");
    expect(bodies).not.toContain("/home/jane");
  });

  test("turning the setting off stops capture at once", async ({
    cli,
    page,
  }) => {
    const sent = await intercept(page);
    cli.register(["config", "list"], { events: [settingsEvent("on")] });
    await page.goto("/#/settings");
    await expect.poll(() => named(sent, "app_open").length).toBe(1);

    await flip(cli, page, "off");
    // Turning it off stores nothing, so a later launch with the setting on sends again.
    const stored = await page.evaluate(() => Object.keys(localStorage));
    expect(stored.filter((k) => !k.startsWith("agentx."))).toEqual([]);
    await page.evaluate(() => {
      setTimeout(() => {
        throw new Error("while off");
      }, 0);
    });
    // On again: an error after it is sent, and the one thrown while off never was.
    await flip(cli, page, "on");
    await page.evaluate(() => {
      setTimeout(() => {
        throw new Error("on again");
      }, 0);
    });
    await expect.poll(() => named(sent, "$exception").length).toBe(1);
    expect(JSON.stringify(sent.events)).not.toContain("while off");
    expect(JSON.stringify(sent.events)).toContain("on again");
  });

  test("with the setting off, not answered, or without a telemetry id, the SDK never loads", async ({
    cli,
    page,
  }) => {
    const sent = await intercept(page);
    const loaded: string[] = [];
    page.on("request", (r) => {
      if (r.url().includes("posthog-js")) {
        loaded.push(r.url());
      }
    });
    cli.register(["config", "list"], { events: [settingsEvent("off")] });
    await page.goto("/#/settings");
    await expect(usageSwitch(page)).not.toBeChecked();

    // Not answered: the question shows, and the SDK waits for the answer.
    cli.restart();
    unanswered(cli);
    await page.reload();
    await expect(dialog(page)).toBeVisible();

    // On, without the telemetry id, which the CLI leaves out while DO_NOT_TRACK is set.
    cli.restart();
    cli.register(["config", "list"], { events: [settingsEvent("on", false)] });
    await page.reload();
    await expect(usageSwitch(page)).toBeChecked();
    await page.evaluate(() => {
      setTimeout(() => {
        throw new Error("not on");
      }, 0);
    });
    expect(loaded).toEqual([]);
    expect(sent.paths).toEqual([]);
  });
});
