import { defineConfig } from "oxlint";
import core from "ultracite/oxlint/core";
import react from "ultracite/oxlint/react";
import shadcn from "ultracite/oxlint/shadcn";

// Ultracite's presets, plus the design-system lint: @shadcn/lint, one rule of
// eslint-plugin-better-tailwindcss and the React rules below keep components on DESIGN.md and
// src/styles/tokens.css.
export default defineConfig({
  extends: [core, react, shadcn],
  ignorePatterns: core.ignorePatterns,
  jsPlugins: [...shadcn.jsPlugins, "eslint-plugin-better-tailwindcss"],
  options: {
    denyWarnings: true,
    reportUnusedDisableDirectives: "error",
    typeAware: true,
    typeCheck: true,
  },
  overrides: [
    {
      // The shadcn preset turns these off in components/ui; the primitives there follow the same
      // rules as the rest of src.
      files: ["**/components/ui/**"],
      rules: {
        "shadcn/no-arbitrary-values": "error",
        "shadcn/no-restyle": ["error", { allow: [] }],
        "shadcn/require-static-classes": "error",
      },
    },
    {
      // Controls are built here, on the native elements.
      files: ["src/components/**"],
      rules: { "react/forbid-elements": "off" },
    },
  ],
  rules: {
    // @shadcn/lint flags arbitrary values in brackets but not the (--x) shorthand, which reads a
    // CSS variable: px-(--padding-page), w-(length:--x), md:px-(--x).
    "better-tailwindcss/no-restricted-classes": [
      "error",
      {
        restrict: [
          {
            message:
              '"$1" reads a CSS variable. Use a theme class such as p-page, or add a token to src/styles/tokens.css and document it in DESIGN.md.',
            pattern: "^(.*[-/]\\((?:[a-z-]+:)?--[^)]*\\).*)$",
          },
        ],
      },
    ],
    "react/forbid-component-props": [
      "error",
      {
        forbid: [
          {
            message:
              "Components take typed props, not style. Add a prop or a variant to the component instead.",
            propName: "style",
          },
        ],
      },
    ],
    "react/forbid-elements": [
      "error",
      {
        forbid: [
          {
            element: "button",
            message:
              "Use Button from src/components/ui, or build a new control in src/components.",
          },
          {
            element: "input",
            message:
              "Use a component from src/components, or build a new control there.",
          },
          {
            element: "textarea",
            message:
              "Use a component from src/components, or build a new control there.",
          },
          {
            element: "select",
            message:
              "Use a component from src/components, or build a new control there.",
          },
        ],
      },
    ],
    "shadcn/no-arbitrary-values": "error",
    "shadcn/no-inline-styles": "error",
    "shadcn/no-raw-colors": "error",
    "shadcn/no-restyle": [
      "error",
      {
        allow: [],
        message:
          "<{{component}}> takes no className: choose its look with its typed props, or add a prop or variant in {{file}}. Place it with a wrapping element or with gap on the parent.",
      },
    ],
    "shadcn/no-unknown-classes": "error",
    "shadcn/require-static-classes": "error",
  },
  settings: {
    shadcn: {
      note: "See DESIGN.md and src/styles/tokens.css.",
      ui: ["#/components", "#/screens", "#/app"],
    },
  },
});
