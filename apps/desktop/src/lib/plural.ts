/** `n` and `noun`, with an s when `n` is not one: "1 skill", "2 skills". */
export const plural = (n: number, noun: string) =>
  `${n} ${noun}${n === 1 ? "" : "s"}`;
