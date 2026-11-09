// The Settings page: This machine (the label, saved with `config set label`, and the machine id),
// Comparing edits (`config set ignore_system_files`), Storage (the paths and the account repo's
// size from `machine`) and Command line (the versions and Run checks). Every row reads the store,
// and every control runs exactly its command.
import type { Page } from "@playwright/test";

import {
  ACCOUNT_REPO,
  HOME,
  MACHINE_ID,
  machine,
  settings,
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

const isLaunchOrLoad = (line: string[]) =>
  [...launchLines, ...loadLines].some(
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
  cli.register(["machine"], { events: [machine()] });
  cli.register(["config", "list"], { events: [settings(ignore)] });
  await page.goto("/#/settings");
  await expect(
    page.getByRole("heading", { level: 1, name: "Settings" })
  ).toBeVisible();
  await expect(labelField(page)).toHaveValue(machineLabel);
  await expect(ignoreSwitch(page)).toBeVisible();
};

test("each section shows what the store holds, and nothing runs on its own", async ({
  cli,
  page,
}) => {
  await openSettings(cli, page);
  const sections = page.getByRole("main").getByRole("region");
  await expect(sections).toHaveText([
    /^This machine/u,
    /^Account remote/u,
    /^Sources/u,
    /^Comparing edits/u,
    /^Storage/u,
    /^Command line/u,
    /^Export settings/u,
    /^Import settings/u,
  ]);

  const thisMachine = section(page, "This machine");
  await expect(thisMachine).toContainText(
    "Shown on skills you publish from here."
  );
  await expect(thisMachine).toContainText(MACHINE_ID);

  const comparing = section(page, "Comparing edits");
  await expect(comparing).toContainText(
    "How agentx decides a skill was edited."
  );
  await expect(comparing).toContainText(
    ".DS_Store, Thumbs.db and editor backups never count as edits. A skill's own .gitignore always applies."
  );
  await expect(ignoreSwitch(page)).toBeChecked();

  await expect(section(page, "Storage")).toMatchAriaSnapshot(`
    - region "Storage":
      - heading "Storage" [level=2]
      - term: Library
      - definition: ${libraryPath}
      - term: Home
      - definition: ${HOME}
      - term: Account repo
      - definition: ${ACCOUNT_REPO} · 1.5 MiB
      - paragraph: There is no cache to clear; what agentx fetched lives in the account repo.
  `);

  await expect(section(page, "Command line")).toContainText(
    `agentx ${cliVersion} · git 2.43.0`
  );
  expect(later(cli)).toEqual([]);
});

test("an account repo not created yet says so", async ({ cli, page }) => {
  const { account_repo: _, account_repo_bytes: __, ...fresh } = machine();
  cli.register(["machine"], { events: [fresh] });
  await page.goto("/#/settings");
  await expect(section(page, "Storage")).toContainText(
    "Account repoNot created yet"
  );
});

test("Enter with a new label runs config set label and toasts; the field shows the label read again", async ({
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
  // The machine reads its label again after the command.
  cli.register(["machine"], { events: [machine("work laptop")] });
  set.release();
  await expect(toasts(page).getByText("Label saved")).toBeVisible();
  await expect.poll(() => runs(cli, ["machine"])).toBe(2);
  await expect(labelField(page)).toHaveValue("work laptop");
  await expect(labelField(page)).not.toHaveAttribute("aria-busy");

  // Losing focus after Enter sends nothing again.
  await labelField(page).blur();
  expect(later(cli)).toEqual([
    ["config", "set", "label", "work laptop", "--json"],
  ]);
});

test("losing focus with a changed label saves it, and with none runs nothing", async ({
  cli,
  page,
}) => {
  await openSettings(cli, page);
  await labelField(page).focus();
  await labelField(page).blur();
  await labelField(page).fill(`${machineLabel}x`);
  await labelField(page).fill(machineLabel);
  await labelField(page).blur();
  await page.waitForTimeout(300);
  expect(later(cli)).toEqual([]);

  cli.register(["config", "set", "label", "studio"], {
    events: [{ type: "settings" }],
  });
  cli.register(["machine"], { events: [machine("studio")] });
  await labelField(page).fill("studio");
  await labelField(page).blur();
  await expect(toasts(page).getByText("Label saved")).toBeVisible();
  await expect(labelField(page)).toHaveValue("studio");
  expect(later(cli)).toEqual([["config", "set", "label", "studio", "--json"]]);
});

test("a label the CLI refuses shows its message under the field", async ({
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
  await expect(thisMachine.getByRole("alert")).toHaveText(message);
  await expect(labelField(page)).toHaveAttribute("aria-invalid", "true");
  // The field keeps what was typed, and no error toast repeats the message.
  await expect(labelField(page)).toHaveValue(" ");
  await expect(toasts(page).getByText(message)).toHaveCount(0);
  // Losing focus does not send the refused label again.
  await labelField(page).blur();
  await page.waitForTimeout(300);
  expect(later(cli)).toEqual([["config", "set", "label", " ", "--json"]]);
  // Typing a new label and saving it takes the message away.
  cli.register(["config", "set", "label", "studio"], {
    events: [{ type: "settings" }],
  });
  cli.register(["machine"], { events: [machine("studio")] });
  await labelField(page).fill("studio");
  await labelField(page).press("Enter");
  await expect(toasts(page).getByText("Label saved")).toBeVisible();
  await expect(thisMachine.getByRole("alert")).toHaveCount(0);
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
  // The switch shows the setting as the CLI reported it until the settings are read again.
  await expect(ignoreSwitch(page)).not.toBeChecked();
  cli.register(["config", "list"], { events: [settings(true)] });
  on.release();
  await expect(
    toasts(page).getByText("System files are ignored")
  ).toBeVisible();
  await expect(ignoreSwitch(page)).toBeChecked();
  await expect(ignoreSwitch(page)).not.toHaveAttribute("aria-busy");

  cli.register(["config", "set", "ignore_system_files", "false"], {
    events: [{ type: "settings" }],
  });
  cli.register(["config", "list"], { events: [settings(false)] });
  // The whole row is the hit target.
  await section(page, "Comparing edits")
    .getByText("Ignore system files", { exact: true })
    .click();
  await expect(
    toasts(page).getByText("System files count as edits now")
  ).toBeVisible();
  await expect(ignoreSwitch(page)).not.toBeChecked();
  expect(later(cli)).toEqual([
    ["config", "set", "ignore_system_files", "true", "--json"],
    ["config", "set", "ignore_system_files", "false", "--json"],
  ]);
});

test("Run checks opens the startup checks in manual mode, and Open agentx comes back", async ({
  cli,
  page,
}) => {
  await openSettings(cli, page);
  await section(page, "Command line")
    .getByRole("button", { name: "Run checks" })
    .click();
  const layer = page.getByRole("region", { name: "Startup checks" });
  await expect(layer.getByRole("heading", { level: 1 })).toHaveText(
    "You're all set"
  );
  await layer.getByRole("button", { name: "Open agentx" }).click();
  await expect(layer).toHaveCount(0);
  await expect(
    page.getByRole("heading", { level: 1, name: "Settings" })
  ).toBeVisible();
  await expect(page).toHaveURL(/#\/settings$/u);
  // The checks ran again: `version` and `doctor`, and no command of the page's own.
  expect(runs(cli, ["version"])).toBe(2);
  expect(runs(cli, ["doctor"])).toBe(2);
  expect(later(cli)).toEqual([]);
});
