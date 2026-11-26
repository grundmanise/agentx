// The Settings page: This machine (the label, saved with `config set label`, and the machine id),
// Comparing edits (`config set ignore_system_files`), Storage (the library's path and the home
// repo's size from `machine`) and Command line (the versions and Run checks). Every row reads the
// store, whose machine and settings come with the inventory, and every control runs exactly its
// command.
import type { Page } from "@playwright/test";

import {
  MACHINE_ID,
  inventoryWith,
  machine,
  scanned,
} from "./fixtures/settings.ts";
import {
  cliVersion,
  expect,
  libraryPath,
  loadLines,
  machineLabel,
  test,
} from "./harness/cli.ts";
import type { MockCli } from "./harness/cli.ts";

const launchLines = [
  ["version", "--json"],
  ["doctor", "--json"],
];

/** The read of the store and its size, which Settings runs each time it shows. */
const storeLines = [["machine", "--json"]];

const isLaunchOrLoad = (line: string[]) =>
  [...launchLines, ...loadLines, ...storeLines].some(
    (known) => known.join(" ") === line.join(" ")
  );

/** The command lines the page ran: every line but the launch's and the store's reads. */
const later = (cli: MockCli) => cli.commands.filter((l) => !isLaunchOrLoad(l));

/** How many times `args` ran. */
const runs = (cli: MockCli, args: string[]) =>
  cli.commands.filter((l) => l.join(" ") === [...args, "--json"].join(" "))
    .length;

const toasts = (page: Page) =>
  page.getByRole("region", { name: /^Notifications/u });
const section = (page: Page, name: string) =>
  page.getByRole("region", { exact: true, name });
const labelField = (page: Page) =>
  page.getByRole("textbox", { name: "Machine label" });
const ignoreSwitch = (page: Page) =>
  page.getByRole("switch", { name: "Ignore system files" });

const openSettings = async (cli: MockCli, page: Page, ignore = true) => {
  cli.onServeStart({ events: [inventoryWith({ ignore })] });
  cli.register(["machine"], { events: [machine()] });
  await page.goto("/#/settings");
  await expect(
    page.getByRole("heading", { level: 1, name: "Settings" })
  ).toBeVisible();
  await expect(labelField(page)).toHaveValue(machineLabel);
  await expect(ignoreSwitch(page)).toBeVisible();
};

test("each card shows what the store holds", async ({ cli, page }) => {
  await openSettings(cli, page);
  const cards = page.getByRole("main").getByRole("region");
  await expect(cards).toHaveText([
    /^This machine/u,
    /^Hub/u,
    /^Editor/u,
    /^Updates/u,
    /^Analytics and crash reporting/u,
    /^Troubleshooting/u,
    /^Snapshot/u,
  ]);

  const thisMachine = section(page, "This machine");
  await expect(thisMachine).toContainText(
    "Shown on skills you publish from here."
  );
  await expect(thisMachine).toContainText(MACHINE_ID);
  await expect(thisMachine).toContainText(`Library${libraryPath}`);
  await expect(thisMachine).toContainText("Store1.5 MiB");

  await expect(section(page, "Hub")).toContainText(
    ".DS_Store, Thumbs.db and editor backups never count as edits. A skill's own .gitignore always applies."
  );
  await expect(ignoreSwitch(page)).toBeChecked();

  // The inventory carries no `telemetry` value: nothing is sent, so the switch is off.
  const analytics = section(page, "Analytics and crash reporting");
  await expect(analytics).toContainText(
    "Help improve agentx with anonymous usage data. What is sent"
  );
  await expect(
    analytics.getByRole("switch", { name: "Allow anonymous usage data" })
  ).not.toBeChecked();

  await expect(section(page, "Troubleshooting")).toContainText(
    `agentx ${cliVersion} · git 2.43.0`
  );
});

test("a store not created yet says so", async ({ cli, page }) => {
  const { store: _, store_bytes: __, ...fresh } = machine();
  cli.register(["machine"], { events: [fresh] });
  await page.goto("/#/settings");
  await expect(section(page, "This machine")).toContainText(
    "StoreNot created yet"
  );
});

test("Enter with a new label runs config set label and toasts; the field shows the label of the next inventory", async ({
  cli,
  page,
}) => {
  await openSettings(cli, page);
  const set = cli.register(["config", "set", "label", "work laptop"], {
    events: [{ type: "settings" }],
    holdAfter: 0,
  });
  await labelField(page).fill("work laptop");
  await labelField(page).press("Enter");
  await set.started;
  await expect(labelField(page)).toHaveAttribute("aria-busy", "true");
  set.release();
  await expect(toasts(page).getByText("Label saved")).toBeVisible();
  // The field waits for the inventory after the command, which brings the label.
  await expect(labelField(page)).toHaveAttribute("aria-busy", "true");
  await scanned(cli, inventoryWith({ label: "work laptop", scanCounter: 2 }));
  await expect(labelField(page)).not.toHaveAttribute("aria-busy");
  await expect(labelField(page)).toHaveValue("work laptop");

  expect(later(cli)).toEqual([
    ["config", "set", "label", "work laptop", "--json"],
  ]);
});

test("losing focus with a changed label saves it", async ({ cli, page }) => {
  await openSettings(cli, page);
  cli.register(["config", "set", "label", "studio"], {
    events: [{ type: "settings" }],
  });
  await labelField(page).fill("studio");
  await labelField(page).blur();
  await expect(toasts(page).getByText("Label saved")).toBeVisible();
  await scanned(cli, inventoryWith({ label: "studio", scanCounter: 2 }));
  await expect(labelField(page)).toHaveValue("studio");
  expect(later(cli)).toEqual([["config", "set", "label", "studio", "--json"]]);
});

test("a label the CLI refuses shows its message under the field, sentence-cased", async ({
  cli,
  page,
}) => {
  await openSettings(cli, page);
  const message =
    "the label must be one non-empty line with no control character in it";
  cli.register(["config", "set", "label", " "], {
    events: [{ message, type: "error" }],
    exitCode: 1,
  });
  await labelField(page).fill(" ");
  await labelField(page).press("Enter");
  const thisMachine = section(page, "This machine");
  await expect(thisMachine.getByRole("alert")).toHaveText(
    "The label must be one non-empty line with no control character in it."
  );
  await expect(labelField(page)).toHaveAttribute("aria-invalid", "true");
  // The field keeps what was typed.
  await expect(labelField(page)).toHaveValue(" ");
  expect(later(cli)).toEqual([["config", "set", "label", " ", "--json"]]);
  await scanned(cli, inventoryWith({ scanCounter: 2 }));
  // Typing a new label and saving it takes the message away.
  cli.register(["config", "set", "label", "studio"], {
    events: [{ type: "settings" }],
  });
  await labelField(page).fill("studio");
  await labelField(page).press("Enter");
  await expect(toasts(page).getByText("Label saved")).toBeVisible();
  await expect(thisMachine.getByRole("alert")).toHaveCount(0);
});

test("a label refusal with a hint shows the message then the hint as one paragraph, the label keeping its case", async ({
  cli,
  page,
}) => {
  await openSettings(cli, page);
  cli.register(["config", "set", "label", "studio"], {
    events: [
      {
        code: "internal",
        hint: "check that you can write to agentx home",
        message: "studio could not be saved: settings.json is read-only",
        type: "error",
      },
    ],
    exitCode: 10,
  });
  await labelField(page).fill("studio");
  await labelField(page).press("Enter");
  const alert = section(page, "This machine").getByRole("alert");
  // One paragraph in one colour: the message, then the hint.
  await expect(alert).toHaveText(
    "studio could not be saved: settings.json is read-only. Check that you can write to agentx home."
  );
  await expect(alert).toHaveJSProperty("tagName", "P");
});

test("the switch runs config set ignore_system_files true, then false, each with its toast", async ({
  cli,
  page,
}) => {
  await openSettings(cli, page, false);
  await expect(ignoreSwitch(page)).not.toBeChecked();

  const on = cli.register(["config", "set", "ignore_system_files", "true"], {
    events: [{ type: "settings" }],
    holdAfter: 0,
  });
  await ignoreSwitch(page).click();
  await on.started;
  await expect(ignoreSwitch(page)).toHaveAttribute("aria-busy", "true");
  // The switch shows the setting as the inventory lists it until the next inventory.
  await expect(ignoreSwitch(page)).not.toBeChecked();
  on.release();
  await expect(
    toasts(page).getByText("System files are ignored")
  ).toBeVisible();
  await expect(ignoreSwitch(page)).toHaveAttribute("aria-busy", "true");
  await scanned(cli, inventoryWith({ ignore: true, scanCounter: 2 }));
  await expect(ignoreSwitch(page)).toBeChecked();
  await expect(ignoreSwitch(page)).not.toHaveAttribute("aria-busy");

  cli.register(["config", "set", "ignore_system_files", "false"], {
    events: [{ type: "settings" }],
  });
  // The whole row is the hit target.
  await section(page, "Hub")
    .getByText("Ignore system files", { exact: true })
    .click();
  await expect(
    toasts(page).getByText("System files count as edits now")
  ).toBeVisible();
  await scanned(cli, inventoryWith({ ignore: false, scanCounter: 3 }));
  await expect(ignoreSwitch(page)).not.toBeChecked();
  expect(later(cli)).toEqual([
    ["config", "set", "ignore_system_files", "true", "--json"],
    ["config", "set", "ignore_system_files", "false", "--json"],
  ]);
});

test("Run checks shows the checks in a dialog over Settings, with Copy report and Done", async ({
  cli,
  page,
}) => {
  await page.context().grantPermissions(["clipboard-read", "clipboard-write"]);
  await openSettings(cli, page);
  await section(page, "Troubleshooting")
    .getByRole("button", { name: "Run checks" })
    .click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByRole("heading")).toHaveText("You're all set");
  await expect(
    page.getByRole("region", { name: "Startup checks" })
  ).toHaveCount(0);
  await expect(
    dialog.getByRole("list", { name: "Checks" }).getByRole("listitem")
  ).toHaveCount(5);
  await dialog.getByRole("button", { name: "Copy report" }).click();
  await expect
    .poll(
      async () =>
        await page.evaluate(async () => await navigator.clipboard.readText())
    )
    .toContain("✓ git");
  await dialog.getByRole("button", { name: "Done" }).click();
  await expect(dialog).toHaveCount(0);
  await expect(
    page.getByRole("heading", { level: 1, name: "Settings" })
  ).toBeVisible();
  await expect(page).toHaveURL(/#\/settings$/u);
  // The checks ran again: `version` and `doctor`.
  expect(runs(cli, ["version"])).toBe(2);
  expect(runs(cli, ["doctor"])).toBe(2);
});
