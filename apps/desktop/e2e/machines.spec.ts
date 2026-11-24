// The Machines screen: this machine from its inventory, the other machines and the cloud machine
// from what the hub holds (`machine list`, and serve's `fleet` event), the open machine's agents
// and skills, Remove report (`machine remove <id>`), and the line that stands in for the other
// machines while none can show.
import type { Page } from "@playwright/test";

import {
  fleet,
  fleetOfThisMachine,
  fleetWithoutWorkbench,
  inventory,
  thisMachine,
  WORKBENCH_ID,
} from "./fixtures/fleet.ts";
import { hub } from "./fixtures/hub.ts";
import { MACHINE_ID } from "./fixtures/settings.ts";
import { doctorWith, expect, test } from "./harness/cli.ts";
import type { InlineEvent, MockCli } from "./harness/cli.ts";

const list = (page: Page) =>
  page.getByRole("region", { exact: true, name: "Machines list" });
const row = (page: Page, name: string) =>
  list(page).getByRole("button", { exact: true, name });
const detail = (page: Page) =>
  page.getByRole("region", { exact: true, name: "Machine details" });
const toasts = (page: Page) =>
  page.getByRole("region", { name: /^Notifications/u });

/** The names of the rows in the list, top to bottom. */
const rowNames = async (page: Page) =>
  await list(page)
    .getByRole("listitem")
    .getByRole("button")
    .evaluateAll((buttons) =>
      buttons.map((b) => b.getAttribute("aria-label") ?? "")
    );

/** How many times `args` ran. */
const runs = (cli: MockCli, args: string[]) =>
  cli.commands.filter((l) => l.join(" ") === [...args, "--json"].join(" "))
    .length;

/** This machine, with a hub when `held` says what it holds, and with none otherwise. */
const launch = (cli: MockCli, held?: InlineEvent) => {
  cli.onServeStart({ events: [inventory] });
  cli.register(["machine"], { events: [thisMachine] });
  if (held !== undefined) {
    cli.register(["source", "list"], { events: [hub] });
    cli.register(["machine", "list"], { events: [held] });
  }
};

/** Opens the Machines screen and waits for this machine's row. */
const openMachines = async (cli: MockCli, page: Page, held?: InlineEvent) => {
  launch(cli, held);
  await page.goto("/#/machines");
  await expect(
    page.getByRole("heading", { level: 1, name: "Machines" })
  ).toBeVisible();
  await expect(row(page, "studio")).toBeVisible();
};

test("the list shows this machine, the other machines by label and the cloud machine, and a machine opens with its agents and skills", async ({
  cli,
  page,
}) => {
  await openMachines(cli, page, fleet);
  // The report this machine wrote is no second row.
  await expect
    .poll(async () => await rowNames(page))
    .toEqual(["studio", "atelier", "workbench", "Cloud machine"]);
  await expect(list(page).getByLabel("4 machines")).toHaveText("4");
  // A skill inside a project is not counted, as a report leaves it out.
  await expect(row(page, "studio")).toHaveAccessibleDescription(
    "This machine. 1 skill"
  );
  await expect(row(page, "atelier")).toHaveAccessibleDescription(
    "Changed 2 hours ago · 1 skill"
  );
  // A name counts once, however many agents see it: the four its details list.
  await expect(row(page, "workbench")).toHaveAccessibleDescription(
    "Changed Mar 9, 2025 · 4 skills"
  );
  await expect(row(page, "Cloud machine")).toHaveAccessibleDescription(
    "Changed 3 days ago · 2 skills"
  );
  await expect(row(page, "workbench").getByRole("img")).toHaveCount(3);

  // This machine is open at first, as it is now.
  await expect(row(page, "studio")).toHaveAttribute("aria-current", "true");
  await expect(detail(page)).toMatchAriaSnapshot(`
    - article "studio":
      - heading "studio" [level=1]
      - text: This machine
      - term: Machine id
      - definition: ${MACHINE_ID}
      - region "Claude Code":
        - heading "Claude Code 1" [level=2]
        - list:
          - listitem: changelog Library example/skills
      - region "Codex":
        - heading "Codex 1" [level=2]
        - list:
          - listitem: changelog Library example/skills
  `);

  // Another machine: its facts, then what each agent sees and where each skill comes from.
  await row(page, "workbench").click();
  await expect(detail(page)).toMatchAriaSnapshot(`
    - article "workbench":
      - heading "workbench" [level=1]
      - button "Remove report"
      - term: Machine id
      - definition: ${WORKBENCH_ID}
      - term: OS
      - definition: macOS
      - term: agentx
      - definition: 1.4.0
      - term: Last change
      - definition: Mar 9, 2025, 10:00 AM
      - region "Claude Code":
        - heading "Claude Code 4" [level=2]
        - list:
          - listitem: changelog Modified Library example/skills
          - listitem: release-notes Library Hub
          - listitem: lint review plugin
          - listitem: scratch Skills directory
      - region "Codex":
        - heading "Codex 2" [level=2]
        - list:
          - listitem: changelog Modified Library example/skills
          - listitem: release-notes Library Hub
      - region "Cursor":
        - heading "Cursor 1 Disabled" [level=2]
        - list:
          - listitem: release-notes Library Hub
  `);
  await expect(
    detail(page).getByText("example/skills").first()
  ).toHaveAttribute(
    "title",
    "https://github.com/example/skills/skills/changelog"
  );

  // A group collapses to its header.
  const claude = detail(page).getByRole("region", { name: "Claude Code" });
  await claude.getByRole("button", { name: /^Claude Code/u }).click();
  await expect(claude.getByRole("listitem")).toHaveCount(0);
});

test("the cloud machine shows what each cloud session is given", async ({
  cli,
  page,
}) => {
  await openMachines(cli, page, fleet);
  await row(page, "Cloud machine").click();
  await expect(detail(page)).toMatchAriaSnapshot(`
    - article "Cloud machine":
      - heading "Cloud machine" [level=1]
      - paragraph: Each cloud session is given these skills.
      - term: Last change
      - region "Claude Code":
        - heading "Claude Code 1" [level=2]
        - list:
          - listitem: changelog Library example/skills
      - region "Codex":
        - heading "Codex 2" [level=2]
        - list:
          - listitem: changelog Library example/skills
          - listitem: release-notes Library Hub
  `);
});

test("the hub is read each time the screen opens, a failed read keeps the list, and what serve finds on the hub replaces it", async ({
  cli,
  page,
}) => {
  await openMachines(cli, page, fleet);
  await expect(row(page, "workbench")).toBeVisible();
  expect(runs(cli, ["machine", "list"])).toBe(1);

  const nav = page.getByRole("navigation", { name: "Main" });
  await nav.getByRole("link", { name: "Agents" }).click();
  await expect(
    page.getByRole("heading", { level: 1, name: "Agents" })
  ).toBeVisible();
  // A hub that cannot be reached says so, and the machines read before stay.
  cli.register(["machine", "list"], {
    events: [
      {
        code: "source",
        message: "cannot reach ssh://git@github.com/me/hub: timed out",
        type: "error",
      },
    ],
    exitCode: 3,
  });
  await nav.getByRole("link", { name: "Machines" }).click();
  await expect(toasts(page)).toContainText(
    "Cannot reach ssh://git@github.com/me/hub: timed out"
  );
  expect(runs(cli, ["machine", "list"])).toBe(2);
  await expect(row(page, "workbench")).toBeVisible();

  await cli.serveEmit([fleetWithoutWorkbench]);
  await expect
    .poll(async () => await rowNames(page))
    .toEqual(["studio", "atelier", "Cloud machine"]);
});

test("Remove report asks first, then runs machine remove and reads the hub again", async ({
  cli,
  page,
}) => {
  await openMachines(cli, page, fleet);
  await row(page, "workbench").click();
  const remove = detail(page).getByRole("button", { name: "Remove report" });
  const dialog = page.getByRole("alertdialog");

  // Cancel runs nothing.
  await remove.click();
  await expect(dialog).toMatchAriaSnapshot(`
    - alertdialog "Remove the report of workbench?":
      - heading "Remove the report of workbench?" [level=2]
      - paragraph: Its inventory report is deleted from the hub. A machine that still reports shows here again.
      - button "Cancel"
      - button "Remove"
  `);
  await dialog.getByRole("button", { name: "Cancel" }).click();
  await expect(dialog).toHaveCount(0);
  expect(runs(cli, ["machine", "remove", WORKBENCH_ID])).toBe(0);

  const removal = cli.register(["machine", "remove", WORKBENCH_ID], {
    holdAfter: 0,
  });
  await remove.click();
  await dialog.getByRole("button", { name: "Remove" }).click();
  await removal.started;
  await expect(dialog).toHaveCount(0);
  await expect(remove).toHaveAttribute("aria-busy", "true");
  // The machine stays, its button pending, until the hub is read again.
  const read = cli.register(["machine", "list"], {
    events: [fleetWithoutWorkbench],
    holdAfter: 0,
  });
  removal.release();
  await expect(
    toasts(page).getByText("Removed the report of workbench")
  ).toBeVisible();
  await read.started;
  await expect(row(page, "workbench")).toBeVisible();
  await expect(remove).toHaveAttribute("aria-busy", "true");
  read.release();
  await expect
    .poll(async () => await rowNames(page))
    .toEqual(["studio", "atelier", "Cloud machine"]);
  expect(runs(cli, ["machine", "list"])).toBe(2);
  // With its machine gone, the pane shows this machine.
  await expect(
    detail(page).getByRole("heading", { level: 1, name: "studio" })
  ).toBeVisible();
});

test("without a hub, one line says to set one and opens Settings", async ({
  cli,
  page,
}) => {
  await openMachines(cli, page);
  await expect.poll(async () => await rowNames(page)).toEqual(["studio"]);
  await expect(list(page)).toContainText(
    "Set a hub to see your other machines here."
  );
  await list(page).getByRole("button", { name: "Open Settings" }).click();
  await expect(page).toHaveURL(/#\/settings$/u);
});

test("with a hub that holds no other machine, one line says how a machine shows", async ({
  cli,
  page,
}) => {
  await openMachines(cli, page, fleetOfThisMachine);
  await expect(list(page)).toContainText(
    "A machine shows here once Report inventory is on there, or after agentx machine report."
  );
  expect(await rowNames(page)).toEqual(["studio"]);
});

test("a later launch shows the machines the hub held, and reads the hub once the checks pass", async ({
  cli,
  page,
}) => {
  await openMachines(cli, page, fleet);
  await expect(row(page, "workbench")).toBeVisible();
  await expect
    .poll(
      async () =>
        await page.evaluate(() => localStorage.getItem("agentx.launch"))
    )
    .toContain(WORKBENCH_ID);

  const doctor = cli.register(["doctor"], {
    events: doctorWith(),
    holdAfter: 0,
  });
  cli.restart();
  await page.reload();
  await doctor.started;
  await expect(page.getByRole("main")).toHaveAttribute("aria-busy", "true");
  expect(await rowNames(page)).toEqual([
    "studio",
    "atelier",
    "workbench",
    "Cloud machine",
  ]);
  expect(runs(cli, ["machine", "list"])).toBe(1);

  doctor.release();
  await expect.poll(() => runs(cli, ["machine", "list"])).toBe(2);
});
