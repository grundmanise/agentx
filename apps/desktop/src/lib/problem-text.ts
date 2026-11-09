// A CLI refusal as the app says it (DESIGN.md §6 "Refusal under a field"): the CLI writes its
// message and hint in lowercase, without a full stop; the app shows them as one sentence-cased
// paragraph, the message then the hint. Nothing else in the text changes, so a quoted command
// such as 'agentx source add …' stays as the CLI wrote it. A first word that is a name rather
// than a plain word keeps its case too: the product's name "agentx", a URL, host or path
// ("github.com/acme/skills"), or a skill's name ("release-notes").
import type { Problem } from "#/store/store.ts";

/** Ends a sentence: a full stop, a question mark or an exclamation mark. */
const ended = /[.!?]$/u;

/** A plain word starts the text: letters, maybe with an apostrophe ("couldn't"), then a space or the end. */
const plainWord = /^\p{Ll}[\p{L}']*(?=\s|$)/u;

/** The product's name, lowercase wherever it stands. */
const productName = /^agentx(?=\s|$)/u;

/** The text with its first letter in upper case when it starts with a plain word, and a full stop at its end unless it has one. */
const sentence = (text: string) => {
  const trimmed = text.trim();
  if (trimmed === "") {
    return "";
  }
  const cased =
    plainWord.test(trimmed) && !productName.test(trimmed)
      ? `${trimmed.charAt(0).toLocaleUpperCase()}${trimmed.slice(1)}`
      : trimmed;
  return ended.test(cased) ? cased : `${cased}.`;
};

/** The message and its hint as one paragraph: "Message. Hint." */
export const problemSentence = ({ message, hint }: Problem): string =>
  [sentence(message), sentence(hint ?? "")]
    .filter((part) => part !== "")
    .join(" ");
