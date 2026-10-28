// Opening a skill from another screen: the palette, a plugin card, or the command runner on exit
// code 4 all come here. The Skills screen selects the skill its route names and scrolls to it.
import { navigateTo } from "#/lib/navigate.ts";

/** The route of a skill: a library skill by its name, any other by its row's key. */
export const skillPath = (key: string) => `/skills/${encodeURIComponent(key)}`;

/** Shows the Skills screen with `key` selected. */
export const openSkill = (key: string) => {
  navigateTo(skillPath(key));
};
