// Lint rules that keep components on the design system (DESIGN.md). Oxlint loads this file as a
// JS plugin; see .oxlintrc.json. Tailwind class rules live in the better-tailwindcss settings there.

// Native controls carry no design; screens use the components that do.
const CONTROLS = new Set(["button", "input", "textarea", "select"]);

const noRawControls = {
  meta: {
    type: "problem",
    docs: { description: "Use a component from src/components instead of a native control" },
    messages: {
      raw: "Use a component from src/components (such as Button) instead of <{{name}}>; build a new control there if none fits.",
    },
  },
  create(context) {
    const file = (context.physicalFilename ?? context.filename).replaceAll("\\", "/");
    if (file.includes("/src/components/")) return {};
    return {
      JSXOpeningElement(node) {
        if (node.name.type === "JSXIdentifier" && CONTROLS.has(node.name.name)) {
          context.report({ node, messageId: "raw", data: { name: node.name.name } });
        }
      },
    };
  },
};

// Inline styles bypass the tokens. A runtime value may still reach CSS as a custom property
// (style={{ "--progress": n }}), which a class then reads (w-(--progress)).
const noInlineStyle = {
  meta: {
    type: "problem",
    docs: { description: "Disallow inline styles except CSS custom properties" },
    messages: {
      style:
        "Style with token classes, not inline styles. Pass a runtime value as a custom property (style={{ \"--x\": value }}) and read it in a class.",
    },
  },
  create(context) {
    const isCustomProperty = (prop) =>
      prop.type === "Property" &&
      !prop.computed &&
      (prop.key.type === "Literal" || prop.key.type === "StringLiteral") &&
      String(prop.key.value).startsWith("--");
    return {
      JSXAttribute(node) {
        if (node.name.type !== "JSXIdentifier" || node.name.name !== "style") return;
        const value = node.value?.type === "JSXExpressionContainer" ? node.value.expression : null;
        if (value?.type === "ObjectExpression" && value.properties.every(isCustomProperty)) return;
        context.report({ node, messageId: "style" });
      },
    };
  },
};

export default {
  meta: { name: "design-system" },
  rules: { "no-raw-controls": noRawControls, "no-inline-style": noInlineStyle },
};
