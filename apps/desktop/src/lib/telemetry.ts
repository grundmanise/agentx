// The app's and the web UI's own usage data and crash reports, sent to PostHog under the
// `telemetry` setting the CLI keeps (src/store/telemetry.ts, docs/help-center/telemetry.mdx).
// Only a release build sends: one whose CLI reports a stable or nightly version. Once the setting
// is on, the PostHog SDK starts once per launch, under the CLI's anonymous id, and sends what its
// own hooks capture, and a `$pageview` for each screen, by its route alone; each click, with every
// element's text and attributes masked, named by its `track` (src/lib/track.ts); and each uncaught
// error and unhandled rejection, plus what the app's error boundary catches, with every user path
// replaced by `<path>`. Every event carries the release channel, the surface (app or web) and the
// CLI's version, beside the SDK's own device properties. It keeps nothing on the machine, records
// no session, asks for no feature flag or survey, and makes no person profile. When the setting
// turns off, every event is dropped before it leaves, and the next launch does not start the SDK.
// The SDK's own opt-out is not used: it stores itself in local storage, which a later launch with
// the setting on would read. In the app, the Tauri shell hears the same answer (`set_telemetry`)
// for its own crash reports.
import { invoke } from "@tauri-apps/api/core";
import type {
  PostHog,
  PostHogConfig,
} from "posthog-js/dist/module.slim.no-external.js";

import { screenOf } from "#/app/kept-screens.tsx";
import { inApp } from "#/lib/platform.ts";

/** The agentx project's public token, which only lets a client send events. */
const POSTHOG_TOKEN = "phc_xQuv9oeNiExAgBEey2FvxwbgSK3ZNyrDPVwbaugWgQHn";
/** PostHog's EU cloud. */
const POSTHOG_HOST = "https://eu.i.posthog.com";

export type ReleaseChannel = "nightly" | "stable";

/**
 * The release channel of a CLI version, as `agentx update` tells them apart: nightly when it has
 * `-nightly.`. A development or source build's version, such as `dev`, has none: it never sends.
 */
export const releaseChannel = (
  cliVersion: string | undefined
): ReleaseChannel | undefined => {
  if (cliVersion === undefined || !/^\d+\.\d+\.\d+(?:-|$)/u.test(cliVersion)) {
    return undefined;
  }
  return cliVersion.includes("-nightly.") ? "nightly" : "stable";
};

/**
 * An absolute path under a user's or the system's temporary directories: up to the next space,
 * quote, bracket or colon, so a stack frame keeps its line and column.
 */
const USER_PATH =
  /(?:\/Users|\/home|\/root|\/private|\/tmp|\/var\/folders)(?:\/[^\s"'`<>()[\]{},;:]*|(?![\w.-]))/gu;

/** `text` with every user path in it replaced by `<path>`. */
export const redactPaths = (text: string) =>
  text.replaceAll(USER_PATH, "<path>");

/** `value` with every string in it, however deep, passed through `redactPaths`. */
const redactAll = (value: unknown): unknown => {
  if (typeof value === "string") {
    return redactPaths(value);
  }
  if (Array.isArray(value)) {
    return value.map(redactAll);
  }
  if (typeof value === "object" && value !== null) {
    return Object.fromEntries(
      Object.entries(value).map(([k, v]) => [k, redactAll(v)])
    );
  }
  return value;
};

/**
 * The route of an address of the app, by its pattern: the screen's path, then `/:id` for anything
 * under it, such as `/skills/:id` for a skill. The rest, a skill's name or a search among them, is
 * never sent.
 */
export const routeOf = (address: string) => {
  let hash = "";
  try {
    ({ hash } = new URL(address));
  } catch {
    return "/";
  }
  const pathname = hash.replace(/^#/u, "").split("?")[0] ?? "";
  const screen = screenOf(pathname);
  if (screen === undefined) {
    return "/";
  }
  return pathname === screen.path ? screen.path : `${screen.path}/:id`;
};

/** A property that holds an address: the page's, where it came from, or where the session began. */
const isAddress = (name: string) => /(?:url|referrer)$/u.test(name);

/** A property's value as it is sent: an address by its route, an exception without user paths. */
const cleaned = (name: string, value: unknown) => {
  if (isAddress(name) && typeof value === "string" && value !== "$direct") {
    return routeOf(value);
  }
  return name.startsWith("$exception") ? redactAll(value) : value;
};

/** Whether the setting is on: events captured while it is off are dropped. */
let sending = false;
/** The route a page view was last sent for: a route shown again at once sends none. */
let viewed = "";
/** The SDK, once started this launch. */
let started: Promise<PostHog> | undefined;

const beforeSend: NonNullable<PostHogConfig["before_send"]> = (event) => {
  if (event === null || !sending) {
    return null;
  }
  const properties: Record<string, unknown> = Object.fromEntries(
    Object.entries(event.properties)
      // The window's host: the app's own, or the machine that serves the web UI.
      .filter(([name]) => !name.endsWith("host"))
      .map(([name, value]) => [name, cleaned(name, value)])
  );
  if (typeof properties.$current_url === "string") {
    properties.$pathname = properties.$current_url;
  }
  return { ...event, properties };
};

const start = async (
  anonymousId: string,
  cliVersion: string,
  channel: ReleaseChannel
) => {
  const [
    { default: client },
    { AnalyticsExtensions, ErrorTrackingExtensions },
  ] = await Promise.all([
    import("posthog-js/dist/module.slim.no-external.js"),
    import("posthog-js/dist/extension-bundles.js"),
    // What catches uncaught errors and rejections, bundled: the SDK loads no script of its own.
    import("posthog-js/dist/exception-autocapture.js"),
  ]);
  client.init(POSTHOG_TOKEN, {
    // The slim build holds no capture hook of its own: clicks come with the analytics bundle,
    // errors with the error tracking one.
    __extensionClasses: { ...AnalyticsExtensions, ...ErrorTrackingExtensions },
    advanced_disable_flags: true,
    api_host: POSTHOG_HOST,
    before_send: beforeSend,
    bootstrap: { distinctID: anonymousId },
    capture_dead_clicks: false,
    capture_exceptions: true,
    capture_heatmaps: false,
    capture_pageleave: false,
    // The app routes by the address's hash, which the SDK's own page views do not follow: the
    // app sends one for each screen it shows (viewScreen).
    capture_pageview: false,
    capture_performance: false,
    disable_external_dependency_loading: true,
    disable_session_recording: true,
    disable_surveys: true,
    mask_all_element_attributes: true,
    mask_all_text: true,
    // The filter drops what a website's crawlers send; the app's window has none, and the
    // webview of a test looks like one.
    opt_out_useragent_filter: true,
    persistence: "memory",
    person_profiles: "identified_only",
  });
  client.register({
    cli_version: cliVersion,
    release_channel: channel,
    surface: inApp ? "app" : "web",
  });
  client.capture("app_open");
  viewed = location.hash.replace(/^#/u, "").split("?")[0] ?? "";
  client.capture("$pageview");
  return client;
};

/**
 * Tells the Tauri shell whether to send its own crash reports, and where. A shell without the
 * command, an older one, is not an error.
 */
const tellShell = async (telemetry: Record<string, unknown>) => {
  try {
    await invoke("set_telemetry", { telemetry });
  } catch {
    // Nothing to tell.
  }
};

/** What the Tauri shell was last told, so it hears each change once. */
let told = "";

/**
 * Follows the `telemetry` setting: starts the SDK the first time it is on in this launch of a
 * release build, under the anonymous id the inventory carries, and stops sending while it is
 * off. In the app, it tells the Tauri shell each change.
 */
export const followTelemetry = (
  on: boolean,
  anonymousId: string | undefined,
  cliVersion: string | undefined
) => {
  const channel = releaseChannel(cliVersion);
  const ready =
    on && anonymousId !== undefined && cliVersion !== undefined
      ? channel
      : undefined;
  const sends = ready !== undefined;
  // The shell sends its crash reports with no person, so it needs no id.
  const telemetry = {
    host: POSTHOG_HOST,
    on: sends,
    releaseChannel: channel,
    token: POSTHOG_TOKEN,
  };
  if (inApp && JSON.stringify(telemetry) !== told) {
    told = JSON.stringify(telemetry);
    void tellShell(telemetry);
  }
  sending = sends;
  if (
    ready !== undefined &&
    started === undefined &&
    anonymousId !== undefined &&
    cliVersion !== undefined
  ) {
    started = start(anonymousId, cliVersion, ready);
  }
};

/** Sends a page view of the screen at `pathname`, while the setting is on. */
export const viewScreen = async (pathname: string) => {
  if (sending && started !== undefined && pathname !== viewed) {
    viewed = pathname;
    const client = await started;
    client.capture("$pageview");
  }
};

/** Sends an error the app's error boundary caught, while the setting is on. */
export const reportError = async (error: unknown) => {
  if (sending && started !== undefined) {
    const client = await started;
    client.captureException(error);
  }
};
