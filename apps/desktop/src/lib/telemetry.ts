// The app's own usage data and crash reports, behind the `telemetry` setting the CLI keeps
// (src/store/telemetry.ts, docs/help-center/telemetry.mdx). A release build names the host and
// key it sends to, in VITE_TELEMETRY_HOST and VITE_TELEMETRY_KEY; a development or source build
// names none, and then the SDK is never loaded. Otherwise, once the setting is on, the SDK starts
// once per launch, under the CLI's anonymous telemetry id, and sends one `app_open` event and an
// `$exception` event for each uncaught error, unhandled rejection or error the app's error
// boundary catches. It keeps nothing on the machine, captures no click, page view or session, asks
// for no feature flag or survey, makes no person profile, and every event leaves with the app's
// own fields and the SDK's name alone, and with every user path in an error replaced by `<path>`,
// as the CLI does. When the setting turns off, every event is dropped before it leaves, and the
// next launch does not start the SDK. The SDK's own opt-out is not used: it stores itself in local
// storage, which a later launch with the setting on would read.
import { invoke } from "@tauri-apps/api/core";
import type {
  PostHog as Client,
  PostHogConfig as ClientConfig,
} from "posthog-js/dist/module.slim.no-external.js";

import { inApp } from "#/lib/platform.ts";

const host = import.meta.env.VITE_TELEMETRY_HOST ?? "";
const key = import.meta.env.VITE_TELEMETRY_KEY ?? "";

/** Whether this build names a host: one that names none never sends, nor asks. */
export const telemetryBuild = host !== "";

/** What `app_info` reports: the app's version, and the platform by the names the CLI uses. */
interface AppInfo {
  version: string;
  os: string;
  arch: string;
}

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
 * The properties an event keeps: the app's own, the anonymous id, what the SDK names itself and
 * the event by, and an exception's. The SDK adds more of its own, the page's address and the
 * device's user agent, screen, time zone and language among them, which are dropped.
 */
const KEPT = new Set([
  "app_version",
  "cli_version",
  "os",
  "arch",
  "distinct_id",
  "token",
  "$lib",
  "$lib_version",
  "$insert_id",
  "$time",
  "$process_person_profile",
]);

const kept = (name: string) => KEPT.has(name) || name.startsWith("$exception");

/** Whether the setting is on: events captured while it is off are dropped. */
let sending = false;
/** The SDK, once started this launch. */
let started: Promise<Client> | undefined;

const beforeSend: NonNullable<ClientConfig["before_send"]> = (event) => {
  if (event === null || !sending) {
    return null;
  }
  const exception = event.event === "$exception";
  const properties = Object.fromEntries(
    Object.entries(event.properties)
      .filter(([name]) => kept(name))
      .map(([name, value]) => [name, exception ? redactAll(value) : value])
  );
  return { ...event, properties };
};

const start = async (telemetryId: string, cliVersion: string) => {
  const [{ default: client }, { ErrorTrackingExtensions }, info] =
    await Promise.all([
      import("posthog-js/dist/module.slim.no-external.js"),
      import("posthog-js/dist/extension-bundles.js"),
      invoke<AppInfo>("app_info"),
      // What catches uncaught errors and rejections, bundled: the SDK loads no script of its own.
      import("posthog-js/dist/exception-autocapture.js"),
    ]);
  client.init(key, {
    __extensionClasses: ErrorTrackingExtensions,
    advanced_disable_flags: true,
    api_host: host,
    autocapture: false,
    before_send: beforeSend,
    bootstrap: { distinctID: telemetryId },
    capture_exceptions: true,
    capture_pageleave: false,
    capture_pageview: false,
    disable_external_dependency_loading: true,
    disable_session_recording: true,
    disable_surveys: true,
    // The filter drops what a website's crawlers send; the app's window has none, and the
    // webview of a test looks like one.
    opt_out_useragent_filter: true,
    persistence: "memory",
    person_profiles: "identified_only",
  });
  client.capture("app_open", {
    app_version: info.version,
    arch: info.arch,
    cli_version: cliVersion,
    os: info.os,
  });
  return client;
};

/**
 * Follows the `telemetry` setting: starts the SDK the first time it is on in this launch, with the
 * telemetry id of the `settings` event, and stops sending while it is off. A build without a host,
 * and the frontend in a browser, never start it.
 */
export const followTelemetry = (
  on: boolean,
  telemetryId: string | undefined,
  cliVersion: string | undefined
) => {
  if (!telemetryBuild || !inApp || on === sending) {
    return;
  }
  if (on && started === undefined) {
    if (telemetryId !== undefined && cliVersion !== undefined) {
      sending = true;
      started = start(telemetryId, cliVersion);
    }
    return;
  }
  sending = on;
};

/** Sends an error the app's error boundary caught, while the setting is on. */
export const reportError = async (error: unknown) => {
  if (sending && started !== undefined) {
    const client = await started;
    client.captureException(error);
  }
};
