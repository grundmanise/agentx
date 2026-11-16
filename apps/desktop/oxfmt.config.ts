import { defineConfig } from "oxfmt";
import ultracite from "ultracite/oxfmt";

// Code copied from another project, in src/vendor, keeps that project's formatting so that it can
// be compared with its source.
export default defineConfig({
  ...ultracite,
  ignorePatterns: [...(ultracite.ignorePatterns ?? []), "src/vendor/**"],
});
