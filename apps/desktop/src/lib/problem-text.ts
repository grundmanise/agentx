// A CLI refusal as the app says it (DESIGN.md §6 "Refusal under a field"): the CLI writes its
// message and hint in lowercase, without a full stop; the app shows them as one sentence-cased
// paragraph, the message then the hint. Nothing else in the text changes, so a quoted command
// such as 'agentx source add …' stays as the CLI wrote it. A first word that is a name rather
// than a plain word keeps its case too: the product's name "agentx", a URL, host or path
// ("github.com/acme/skills"), a hyphenated name ("release-notes"), or a name the caller knows,
// such as the skill a dialog is for ("scratch").
import type { InventoryEvent } from "#/cli/events.gen.ts";
import type { Problem } from "#/store/store.ts";

export interface SentenceOptions {
  /**
   * Names that keep their case when one starts the message or the hint, such as the skill a
   * dialog is for or the name typed in it. A first word that equals one of them exactly is left
   * as it is.
   */
  keep?: readonly string[];
}

/** Ends a sentence: a full stop, a question mark or an exclamation mark. */
const ended = /[.!?]$/u;

/** A plain word starts the text: letters, maybe with an apostrophe ("couldn't"), then a space or the end. */
const plainWord = /^\p{Ll}[\p{L}']*(?=\s|$)/u;

/** The text with its first letter in upper case when it starts with a plain word that is no name, and a full stop at its end unless it has one. */
const sentence = (text: string, keep: readonly string[]) => {
  const trimmed = text.trim();
  if (trimmed === "") {
    return "";
  }
  const word = plainWord.exec(trimmed)?.[0];
  const cased =
    word !== undefined && word !== "agentx" && !keep.includes(word)
      ? `${trimmed.charAt(0).toLocaleUpperCase()}${trimmed.slice(1)}`
      : trimmed;
  return ended.test(cased) ? cased : `${cased}.`;
};

/** The message and its hint as one paragraph: "Message. Hint." */
export const problemSentence = (
  { message, hint }: Problem,
  { keep = [] }: SentenceOptions = {}
): string =>
  [sentence(message, keep), sentence(hint ?? "", keep)]
    .filter((part) => part !== "")
    .join(" ");

/** The name of every skill the inventory holds, in the library or in a configuration. */
export const skillNamesOf = (inventory: InventoryEvent | undefined) => [
  ...new Set([
    ...(inventory?.library ?? []).map((entry) => entry.name),
    ...(inventory?.skills ?? []).map((skill) => skill.name),
  ]),
];
