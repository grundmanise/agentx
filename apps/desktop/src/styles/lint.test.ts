import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { join } from "node:path";

// The design-system lint (.oxlintrc.json, built on @shadcn/lint) is the guard that keeps
// components on DESIGN.md. This runs it on a fixture of off-system code so a weakened rule fails
// here, not in review. Each `bad(<rule>)` line must be reported by that rule; `ok` lines must pass.
const root = join(__dirname, "../..");
const fixture = "lint/fixtures/off-system.tsx";

type Diagnostic = { code: string; severity: string; labels: { span: { line: number } }[] };

function lint(): Diagnostic[] {
  let output: string;
  try {
    output = execFileSync(join(root, "node_modules/.bin/oxlint"), ["-f", "json", fixture], {
      cwd: root,
      encoding: "utf8",
    });
  } catch (e) {
    output = (e as { stdout: string }).stdout; // oxlint exits 1 when it reports errors
  }
  return (JSON.parse(output) as { diagnostics: Diagnostic[] }).diagnostics;
}

describe("design-system lint", () => {
  const diagnostics = lint();
  const lines = readFileSync(join(root, fixture), "utf8").split("\n");
  const rulesOn = (line: number) =>
    diagnostics.filter((d) => d.labels[0]?.span.line === line).map((d) => d.code);

  it("loads its configuration without warnings", () => {
    expect(diagnostics.filter((d) => d.severity !== "error")).toEqual([]);
  });

  it("reports every `bad` line with the rule it names", () => {
    const bad = lines.flatMap((l, i) => {
      const rule = /\{\/\* bad\(([a-z-]+)\):/.exec(l)?.[1];
      return rule ? [{ line: i + 1, rule, text: l.trim() }] : [];
    });
    expect(bad.length).toBeGreaterThan(0);
    const missed = bad.filter(({ line, rule }) => !rulesOn(line).some((c) => c.endsWith(`(${rule})`)));
    expect(missed).toEqual([]);
  });

  it("accepts every `ok` line", () => {
    const flagged = lines.flatMap((l, i) =>
      l.includes("{/* ok:") && rulesOn(i + 1).length > 0 ? [`${i + 1}: ${l.trim()} -> ${rulesOn(i + 1).join(", ")}`] : [],
    );
    expect(flagged).toEqual([]);
  });
});
