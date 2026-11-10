// The Skills header's controls, for specs that start from them: Add is a button group whose
// three-dots trigger, "More add actions", opens a menu with Create skill.
import type { Page } from "@playwright/test";

/** The header of the Skills list column. */
export const skillsHeader = (page: Page) =>
  page.getByRole("region", { name: "Skills list" });

/** The three-dots trigger of Add, which opens its menu. */
export const moreAddActions = (page: Page) =>
  skillsHeader(page).getByRole("button", {
    exact: true,
    name: "More add actions",
  });

/** Opens the Create skill dialog from the Skills header: Add's menu, then Create skill. */
export const openCreateSkillDialog = async (page: Page) => {
  await moreAddActions(page).click();
  await page.getByRole("menuitem", { name: "Create skill" }).click();
};
