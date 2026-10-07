import { defineConfig } from "oxfmt";
import ultracite from "ultracite/oxfmt";

export default defineConfig({
  ...ultracite,
  // The lint test reads the fixture line by line, so it keeps its layout.
  ignorePatterns: [...(ultracite.ignorePatterns ?? []), "lint/fixtures"],
});
