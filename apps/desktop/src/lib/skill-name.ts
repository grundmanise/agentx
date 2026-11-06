// The name a skill of your own can take: the Agent Skills name grammar the CLI checks for
// `skill new` and `skill fork --name` (docs/spec/cli-contract.md, "Creating a skill"). A field
// checks a name here before it runs the command; the CLI still has the last word, since only it
// knows which names are taken.

/** Lowercase letters and digits in runs joined by single hyphens, as the CLI matches it. */
const grammar = /^[a-z0-9]+(?:-[a-z0-9]+)*$/u;

/** The longest name the grammar allows. */
export const SKILL_NAME_LIMIT = 64;

/** The rule as a field states it under itself. */
export const SKILL_NAME_RULE =
  "1 to 64 lowercase letters, digits and single hyphens";

/** Whether the CLI takes `name` as a skill's name, as far as its grammar goes. */
export const isSkillName = (name: string) =>
  name.length <= SKILL_NAME_LIMIT && grammar.test(name);

/**
 * Whether `name` breaks the rule in a way more typing cannot mend: an empty field, or a name
 * that ends in the hyphen of its next part ("my-"), is still being typed.
 */
export const breaksSkillNameRule = (name: string) =>
  name !== "" &&
  !isSkillName(name) &&
  !(name.endsWith("-") && isSkillName(name.slice(0, -1)));
