/** `n` and `noun`, with an s when `n` is not one: "1 skill", "2 skills". */
export const plural = (n: number, noun: string) =>
  `${n} ${noun}${n === 1 ? "" : "s"}`;

/** `a`, `a and b`, `a, b and c`. */
export const andList = (words: string[]) =>
  words.length < 2
    ? words.join("")
    : `${words.slice(0, -1).join(", ")} and ${words.at(-1) ?? ""}`;
