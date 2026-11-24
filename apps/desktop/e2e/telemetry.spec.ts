// Usage data (docs/help-center/telemetry.mdx): the telemetry question, asked in a release build
// while the `telemetry` setting has no value, the Usage data switch in Settings, both bound to
// `config set telemetry`, and what the app sends to PostHog, which these tests intercept and
// decode. The mocked CLI reports a source build's version unless a test reports a release one.
import { gunzipSync } from "node:zlib";

import type { Page, Request } from "@playwright/test";

import type { Settings } from "#/cli/events.gen.ts";

import {
  cliVersion,
  expect,
  firstInventory,
  machineLabel,
  posthogHost,
  test,
} from "./harness/cli.ts";
import type { InlineEvent, MockCli } from "./harness/cli.ts";

const ANONYMOUS_ID = "5d1c0a9e7f3b4c2a8e6d4f1b3a5c7e9d";
const STABLE = "1.2.0";
const NIGHTLY = "1.3.0-nightly.20261124.7";

/**
 * The `settings` event of `config list`, with the `telemetry` setting as given, or none, and the
 * anonymous id while it is on, unless `id` is false, as under DO_NOT_TRACK.
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
    ? { anonymous_id: ANONYMOUS_ID, settings, type: "settings" }
    : { settings, type: "settings" };
};

/** The CLI reports `version`: a release build's, stable unless given, or another. */
const release = (cli: MockCli, version = STABLE) => {
  cli.register(["version"], {
    events: [{ cli_version: version, type: "version" }],
  });
};

/** A home whose `telemetry` setting has no value. */
const unanswered = (cli: MockCli) => {
  cli.register(["config", "list"], { events: [settingsEvent()] });
};

const dialog = (page: Page) =>
  page.getByRole("alertdialog", { name: "Help improve agentx" });

const usageSwitch = (page: Page) =>
  page.getByRole("switch", { name: "Send anonymous usage data" });

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
  await expect(page.getByRole("link", { name: "Settings" })).toBeVisible();
};

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

/** The events of a batch the SDK posted. */
const eventsOf = (request: Request): SentEvent[] => {
  const raw = request.postDataBuffer();
  if (raw === null) {
    return [];
  }
  // The SDK gzips its batches.
  const body: unknown = JSON.parse(
    (raw[0] === 0x1f ? gunzipSync(raw) : raw).toString()
  );
  if (typeof body !== "object" || body === null) {
    return [];
  }
  const batch: unknown = Reflect.get(body, "batch");
  return Array.isArray(batch) ? batch.filter(isSentEvent) : [];
};

/** Intercepts the telemetry host, answering 200, and records what reaches it. */
const intercept = async (page: Page): Promise<Sent> => {
  const sent: Sent = { events: [], paths: [] };
  await page.route(`${posthogHost}/**`, async (route) => {
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

/** Turns the setting on in Settings, which starts the SDK, or off. */
const flip = async (cli: MockCli, page: Page, value: "on" | "off") => {
  answers(cli, value);
  await usageSwitch(page).click();
  await expect(usageSwitch(page)).toBeChecked({ checked: value === "on" });
};

test.describe("the question", () => {
  test("a source build never asks", async ({ cli, page }) => {
    unanswered(cli);
    await page.goto("/");
    await isLaunched(page);
    await expect.poll(() => cli.commands.length).toBeGreaterThan(4);
    await expect(dialog(page)).toHaveCount(0);
  });

  test("a release build asks once it has started, while the setting has no value", async ({
    cli,
    page,
  }) => {
    release(cli);
    unanswered(cli);
    await page.goto("/");
    await expect(dialog(page)).toBeVisible();
    // Under it, the screens the first inventory filled, out of reach while it asks.
    await expect(page.locator("main")).toHaveAttribute("aria-busy", "false");

    await expect(dialog(page)).toContainText(
      "Send anonymous usage data to PostHog? We use it to find what breaks and to make agentx better for you."
    );
    const sent = dialog(page).getByRole("list", { name: "What may be sent" });
    await expect(sent.getByRole("listitem")).toHaveText([
      "the commands the app runs, as in the terminal",
      "the screens you open and the buttons you click, by name only",
      "errors and crashes, with every path removed",
      "the app version, release channel, OS, screen size, language and time zone",
      "a non-reversible, hashed anonymous id",
    ]);
    await expect(dialog(page)).toContainText(
      "It never sends paths, skill or server names, URLs, on-screen text or anything you type. Your IP address is not stored."
    );
    // "Don't allow" is the default: it has the focus.
    await expect(
      dialog(page).getByRole("button", { name: "Don't allow" })
    ).toBeFocused();

    await dialog(page).getByRole("button", { name: "Details" }).click();
    expect(cli.openedUrls).toEqual(["https://docs.agentx.wtf/telemetry"]);
  });

  test("a setting with a value asks nothing", async ({ cli, page }) => {
    release(cli);
    answers(cli, "off");
    await page.goto("/");
    await isLaunched(page);
    await expect.poll(() => cli.commands.length).toBeGreaterThan(4);
    await expect(dialog(page)).toHaveCount(0);
  });

  test("Allow anonymous data runs config set telemetry on, then a refresh, and asks no more", async ({
    cli,
    page,
  }) => {
    release(cli);
    unanswered(cli);
    cli.register(["config", "set", "telemetry", "on"], {
      events: [settingsEvent("on")],
    });
    await page.goto("/");
    await expect(dialog(page)).toBeVisible();
    const before = refreshes(cli);

    await dialog(page)
      .getByRole("button", {
        name: "Allow anonymous data",
      })
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

  test("Don't allow, and Escape, run config set telemetry off", async ({
    cli,
    page,
  }) => {
    release(cli);
    unanswered(cli);
    cli.register(["config", "set", "telemetry", "off"], {
      events: [settingsEvent("off")],
    });
    await page.goto("/");
    await dialog(page).getByRole("button", { name: "Don't allow" }).click();
    await expect
      .poll(() => telemetrySets(cli))
      .toEqual(["config set telemetry off --json"]);

    cli.restart();
    release(cli);
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

  test("the switch in Settings shows the setting and runs the same command", async ({
    cli,
    page,
  }) => {
    answers(cli, "off");
    answers(cli, "on");
    unanswered(cli);
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
});

/** What the app tells the Tauri shell of the setting, in a nightly build. */
const told = (on: boolean) => ({
  telemetry: {
    host: posthogHost,
    on,
    releaseChannel: "nightly",
    token: expect.stringMatching(/^phc_/u),
  },
});

/** Every event's properties, as one text, to look for what must never be sent. */
const allText = (sent: Sent) => JSON.stringify(sent.events);

test.describe("what the app sends", () => {
  test("app_open, screens by route and clicks by name, under the CLI's id and channel", async ({
    cli,
    page,
  }) => {
    const sent = await intercept(page);
    release(cli, NIGHTLY);
    cli.register(["config", "list"], { events: [settingsEvent("on")] });
    await page.goto("/");
    await isLaunched(page);
    await expect.poll(() => named(sent, "app_open").length).toBe(1);

    const [open] = named(sent, "app_open");
    expect(open.properties).toMatchObject({
      $process_person_profile: false,
      cli_version: NIGHTLY,
      distinct_id: ANONYMOUS_ID,
      release_channel: "nightly",
      surface: "app",
    });
    // The SDK's own device properties stay.
    expect(open.properties).toHaveProperty("$screen_width");
    expect(open.properties).toHaveProperty("$timezone");

    // A screen by its route, a skill's by its pattern: the skill's name is never sent.
    await page.goto("/#/skills/secret-skill");
    await page.getByRole("link", { name: "Settings" }).click();
    await expect
      .poll(() =>
        named(sent, "$pageview").map((e) => e.properties.$current_url)
      )
      .toEqual(expect.arrayContaining(["/skills/:id", "/settings"]));
    for (const view of named(sent, "$pageview")) {
      expect(view.properties.$pathname).toBe(view.properties.$current_url);
    }

    // A click carries its name, and neither the element's text nor its attributes.
    await page
      .getByRole("region", { exact: true, name: "Usage data" })
      .getByRole("button", { name: "What is sent" })
      .click();
    await expect
      .poll(() => named(sent, "$autocapture").map((e) => e.properties.action))
      .toContain("settings.usage-data-help");

    expect(allText(sent)).not.toContain("secret-skill");
    expect(allText(sent)).not.toContain("What is sent");
    expect(allText(sent)).not.toContain("localhost:1420");
    expect(new Set(sent.paths)).toEqual(new Set(["/e/"]));
    // The SDK keeps nothing on the machine: the only stored keys are the app's own.
    const stored = await page.evaluate(() => Object.keys(localStorage));
    expect(stored.filter((k) => !k.startsWith("agentx."))).toEqual([]);
    expect(await page.context().cookies()).toEqual([]);

    // A new launch sends its own app_open.
    cli.restart();
    release(cli, NIGHTLY);
    await page.reload();
    await expect.poll(() => named(sent, "app_open").length).toBe(2);
  });

  test("errors are sent with user paths removed", async ({ cli, page }) => {
    const sent = await intercept(page);
    release(cli);
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
    expect(allText(sent)).not.toContain("/Users/");
    expect(allText(sent)).not.toContain("/home/jane");
  });

  test("turning the setting off stops capture at once", async ({
    cli,
    page,
  }) => {
    const sent = await intercept(page);
    release(cli);
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
    expect(allText(sent)).not.toContain("while off");
    expect(allText(sent)).toContain("on again");
  });

  test("the Tauri shell hears each change of the setting, for its own crash reports", async ({
    cli,
    page,
  }) => {
    release(cli, NIGHTLY);
    cli.register(["config", "list"], { events: [settingsEvent("on")] });
    await page.goto("/#/settings");
    await expect.poll(() => cli.telemetryTold.at(-1)).toEqual(told(true));
    await flip(cli, page, "off");
    await expect.poll(() => cli.telemetryTold.at(-1)).toEqual(told(false));
  });

  test("off, not answered, without an anonymous id, or in a source build, the SDK never loads", async ({
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
    release(cli);
    cli.register(["config", "list"], { events: [settingsEvent("off")] });
    await page.goto("/#/settings");
    await expect(usageSwitch(page)).not.toBeChecked();

    // Not answered: the question shows, and the SDK waits for the answer.
    cli.restart();
    release(cli);
    unanswered(cli);
    await page.reload();
    await expect(dialog(page)).toBeVisible();

    // On, without the anonymous id, which the CLI leaves out while DO_NOT_TRACK is set.
    cli.restart();
    release(cli);
    cli.register(["config", "list"], { events: [settingsEvent("on", false)] });
    await page.reload();
    await expect(usageSwitch(page)).toBeChecked();

    // On, with the id, in a source build.
    cli.restart();
    release(cli, cliVersion);
    cli.register(["config", "list"], { events: [settingsEvent("on")] });
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
