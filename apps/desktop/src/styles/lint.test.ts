import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import path from "node:path";

// The design-system lint (oxlint.config.ts, built on @shadcn/lint) is the guard that keeps
// components on DESIGN.md. This runs it on a fixture of off-system code so a weakened rule fails
// here, not in review. Each `bad(<rule>)` line must be reported by that rule; `ok` lines must pass.
const root = path.join(import.meta.dirname, "../..");
const fixture = "lint/fixtures/off-system.tsx";

interface Diagnostic {
  code: string;
  severity: string;
  labels: { span: { line: number } }[];
}

const isReport = (value: unknown): value is { diagnostics: Diagnostic[] } =>
  typeof value === "object" &&
  value !== null &&
  "diagnostics" in value &&
  Array.isArray(value.diagnostics);

const lint = (): Diagnostic[] => {
  let output: string;
  try {
    output = execFileSync(
      path.join(root, "node_modules/.bin/oxlint"),
      ["-f", "json", fixture],
      {
        cwd: root,
        encoding: "utf-8",
      }
    );
  } catch (error) {
    // oxlint exits 1 when it reports errors.
    output =
      error instanceof Error && "stdout" in error ? String(error.stdout) : "";
  }
  const report: unknown = JSON.parse(output);
  return isReport(report) ? report.diagnostics : [];
};

describe("design-system lint", () => {
  const diagnostics = lint();
  const lines = readFileSync(path.join(root, fixture), "utf-8").split("\n");
  // Only the design-system rules count on a line; the fixture breaks Ultracite's other rules too.
  const designRules = diagnostics.filter((d) =>
    /^(?:shadcn\(|react\(forbid-)/u.test(d.code)
  );
  const rulesOn = (line: number) =>
    designRules
      .filter((d) => d.labels[0]?.span.line === line)
      .map((d) => d.code);

  it("loads its configuration without warnings", () => {
    expect(diagnostics.filter((d) => d.severity !== "error")).toEqual([]);
  });

  it("reports every `bad` line with the rule it names", () => {
    const bad = lines.flatMap((l, i) => {
      const rule = /\{\/\* bad\((?<rule>[a-z-]+)\):/u.exec(l)?.groups?.rule;
      return rule === undefined ? [] : [{ line: i + 1, rule, text: l.trim() }];
    });
    expect(bad.length).toBeGreaterThan(0);
    const missed = bad.filter(
      ({ line, rule }) => !rulesOn(line).some((c) => c.endsWith(`(${rule})`))
    );
    expect(missed).toEqual([]);
  });

  it("accepts every `ok` line", () => {
    const flagged = lines.flatMap((l, i) =>
      l.includes("{/* ok:") && rulesOn(i + 1).length > 0
        ? [`${i + 1}: ${l.trim()} -> ${rulesOn(i + 1).join(", ")}`]
        : []
    );
    expect(flagged).toEqual([]);
  });
});
