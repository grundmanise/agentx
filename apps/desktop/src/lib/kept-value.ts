// A dialog that a value opens closes when the value goes, and plays its close animation after.
// Until that ends, it keeps showing what it showed while open.
import { useState } from "react";

/** `value`, or while it is undefined, the last value it held. */
export const useKept = <T>(value: T | undefined): T | undefined => {
  const [kept, setKept] = useState(value);
  if (value !== undefined && value !== kept) {
    setKept(value);
  }
  return value ?? kept;
};
