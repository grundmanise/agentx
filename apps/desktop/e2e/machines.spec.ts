// The Machines screen: this machine from its inventory, the other machines and the cloud machine
// from what the hub holds (`machine list`, and serve's `fleet` event), the open machine's agents
// and skills, the By skill view of every skill across the machines, Remove report
// (`machine remove <id>`), and the line that stands in for the other machines while none can show.
import type { Page } from "@playwright/test";

import {
  fleet,
  fleetOfSameSkills,
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
const bySkill = (page: Page) =>
  page.getByRole("region", { exact: true, name: "Machines by skill" });
/** The switch between the screen's views, under its title. */
const views = (page: Page) =>
  page.getByRole("radiogroup", { name: "Machines" });
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

/** The skills the By skill table lists, top to bottom. */
const skillNames = async (page: Page) => {
  const cells = await bySkill(page).getByRole("rowheader").allInnerTexts();
  return cells.map((text) => text.split("\n")[0]);
};

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
    "This machine. 2 skills"
  );
  await expect(row(page, "atelier")).toHaveAccessibleDescription(
    "Changed 2 hours ago · 3 skills"
  );
  // A name counts once, however many agents see it: the five its details list.
  await expect(row(page, "workbench")).toHaveAccessibleDescription(
    "Changed Mar 9, 2025 · 5 skills"
  );
  await expect(row(page, "Cloud machine")).toHaveAccessibleDescription(
    "Changed 3 days ago · 3 skills"
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
        - heading "Claude Code 2" [level=2]
        - list:
          - listitem: changelog Library example/skills
          - listitem: commit-message Library Hub
      - region "Codex":
        - heading "Codex 2" [level=2]
        - list:
          - listitem: changelog Library example/skills
          - listitem: commit-message Library Hub
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
        - heading "Claude Code 5" [level=2]
        - list:
          - listitem: changelog Modified Library example/skills
          - listitem: commit-message Library Hub
          - listitem: release-notes Library Hub
          - listitem: lint review plugin
          - listitem: scratch Skills directory
      - region "Codex":
        - heading "Codex 3" [level=2]
        - list:
          - listitem: changelog Modified Library example/skills
          - listitem: commit-message Library Hub
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
        - heading "Codex 3" [level=2]
        - list:
          - listitem: changelog Library example/skills
          - listitem: commit-message Library Hub
          - listitem: release-notes Library Hub
  `);
});

test("By skill lists every skill across the machines with what each machine has of it, and Only differences leaves the skills that differ", async ({
  cli,
  page,
}) => {
  await openMachines(cli, page, fleet);
  await views(page).getByRole("radio", { name: "By skill" }).check();
  // A column per machine, in the list's order. A machine is compared with the first one that
  // holds the skill: changelog is edited on workbench and at an older commit of its source on the
  // cloud machine, whose skills have no content hash, so its hub skills claim no difference.
  await expect(bySkill(page).getByRole("table")).toMatchAriaSnapshot(`
    - table "Skills on your machines":
      - rowgroup:
        - row "Skill studio This machine atelier Changed 2 hours ago workbench Changed Mar 9, 2025 Cloud machine Changed 3 days ago"
      - rowgroup:
        - row "changelog example/skills Has it Has it Another version Another version"
        - row "commit-message Hub Has it Has it Has it Has it"
        - row "lint review plugin Not there Not there Has it Not there"
        - row "release-notes Hub Not there Not there Has it Has it"
        - row "scratch Skills directory Not there Has it Has it Not there"
  `);

  // In a window too narrow for every machine, the columns scroll sideways under the skills.
  await page.setViewportSize({ height: 700, width: 1100 });
  const table = bySkill(page).getByRole("region", {
    name: "Skills on your machines",
  });
  const name = table.getByRole("rowheader", { name: /^changelog/u });
  const before = await name.boundingBox();
  await table.evaluate((el) => {
    el.scrollLeft = 120;
  });
  expect(await table.evaluate((el) => el.scrollLeft)).toBe(120);
  expect(await name.boundingBox()).toEqual(before);

  // commit-message is the same on every machine.
  await bySkill(page)
    .getByRole("checkbox", { name: "Only differences" })
    .click();
  await expect
    .poll(async () => await skillNames(page))
    .toEqual(["changelog", "lint", "release-notes", "scratch"]);

  // The view stays as it was left while another screen shows.
  const nav = page.getByRole("navigation", { name: "Main" });
  await nav.getByRole("link", { name: "Agents" }).click();
  await expect(
    page.getByRole("heading", { level: 1, name: "Agents" })
  ).toBeVisible();
  await nav.getByRole("link", { name: "Machines" }).click();
  await expect(
    bySkill(page).getByRole("checkbox", { name: "Only differences" })
  ).toBeChecked();
  expect(await skillNames(page)).toEqual([
    "changelog",
    "lint",
    "release-notes",
    "scratch",
  ]);

  // By machine is the list and the open machine again.
  await views(page).getByRole("radio", { name: "By machine" }).check();
  await expect(row(page, "studio")).toHaveAttribute("aria-current", "true");

  // With every skill the same on every machine, one line says that nothing differs.
  await views(page).getByRole("radio", { name: "By skill" }).check();
  await cli.serveEmit([fleetOfSameSkills]);
  await expect(bySkill(page)).toContainText(
    "No differences between your machines"
  );

  // One machine has nothing to be compared with: its list and details show, with no switch.
  await cli.serveEmit([fleetOfThisMachine]);
  await expect(row(page, "studio")).toHaveAttribute("aria-current", "true");
  await expect(views(page)).toHaveCount(0);
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

test("a row says how long ago as of now when the screen shows again", async ({
  cli,
  page,
}) => {
  await page.clock.install();
  await openMachines(cli, page, fleet);
  await expect(row(page, "atelier")).toHaveAccessibleDescription(
    "Changed 2 hours ago · 3 skills"
  );
  // An hour passes while another screen shows, where the hidden screen keeps no time.
  const nav = page.getByRole("navigation", { name: "Main" });
  await nav.getByRole("link", { name: "Agents" }).click();
  await expect(
    page.getByRole("heading", { level: 1, name: "Agents" })
  ).toBeVisible();
  await page.clock.fastForward("01:00:00");
  await nav.getByRole("link", { name: "Machines" }).click();
  await expect(row(page, "atelier")).toHaveAccessibleDescription(
    "Changed 3 hours ago · 3 skills"
  );
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
  // The machines of a hub that is no longer set, which serve may still send, do not show.
  await cli.serveEmit([fleet]);
  await expect
    .poll(
      async () =>
        await page.evaluate(() => localStorage.getItem("agentx.launch"))
    )
    .toContain(WORKBENCH_ID);
  expect(await rowNames(page)).toEqual(["studio"]);
  await list(page).getByRole("button", { name: "Open Settings" }).click();
  await expect(page).toHaveURL(/#\/settings$/u);
});

test("with a hub that holds no other machine, one line says how a machine shows", async ({
  cli,
  page,
}) => {
  await openMachines(cli, page, fleetOfThisMachine);
  await expect(list(page)).toContainText(
    "A machine shows here once Report inventory is on there."
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
