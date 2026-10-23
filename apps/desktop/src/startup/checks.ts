// The five startup checks, read from what the launch sequence found: the CLI's resolution, its
// `version` and `doctor` output, and the serve child's first snapshot. Nothing here looks at the
// machine; every detail and hint is the CLI's own text.
import type {
  DoctorEvent,
  DoctorStatus,
  SnapshotEvent,
} from "#/cli/events.gen.ts";

/** An error the CLI reported, or why a command could not run. */
export interface Problem {
  message: string;
  hint?: string;
}

/** What the launch sequence has found so far; a field not there yet is not known yet. */
export interface Findings {
  /** The CLI's path, null when none was found. */
  cli?: { path: string | null };
  version?: { cliVersion: string } | { error: Problem };
  doctor?: { rows: DoctorEvent[]; exitCode: number | null; error?: Problem };
  serve?: { snapshot: SnapshotEvent } | { error: Problem };
}

export type CheckStatus = "waiting" | "running" | "pass" | "warn" | "fail";

/** A line under a check's label, with the CLI's hint for it. */
export interface CheckLine {
  text: string;
  hint?: string;
}

export interface Check {
  label: string;
  status: CheckStatus;
  lines: CheckLine[];
}

export interface Checks {
  checks: Check[];
  /** The title of the first failure; undefined when nothing failed. */
  failure?: string;
  /** All five checks passed. */
  passed: boolean;
}

// The doctor rows each check reads, the row its pass detail comes from first.
const groups: Record<2 | 3 | 4, readonly string[]> = {
  2: ["git", "fork_merges", "commit_identity"],
  3: ["home", "library"],
  4: [
    "account_repo",
    "settings",
    "lock",
    "mutations",
    "source_remotes",
    "staged_imports",
  ],
};

const labels = [
  "agentx command line",
  "git 2.40 or newer",
  "Home folder is writable",
  "Account repo opens",
  "Watching this machine",
] as const;

const titles = [
  "Reinstall agentx",
  "Update git to continue",
  "agentx can't write to your home folder",
  "agentx can't open its account repo",
  "agentx can't watch this machine",
] as const;

/** The title when doctor itself could not run or report. */
const doctorFailed = "agentx can't check this machine";

const plural = (n: number, one: string, many: string) =>
  `${n} ${n === 1 ? one : many}`;

const line = (row: { detail: string; hint?: string }): CheckLine =>
  row.hint === undefined
    ? { text: row.detail }
    : { hint: row.hint, text: row.detail };

const problemLine = (problem: Problem): CheckLine =>
  problem.hint === undefined
    ? { text: problem.message }
    : { hint: problem.hint, text: problem.message };

/** Your own skills and those from sources, as the library of the first snapshot holds them. */
const counts = (snapshot: SnapshotEvent) => {
  const managed = snapshot.library.filter((s) => s.kind === "managed");
  // Your own skills carry a fork id, or have no source while no account remote is set.
  const own = managed.filter(
    (s) => s.fork_id !== undefined || s.source === undefined
  ).length;
  return `${own} of your skills, ${managed.length - own} from sources`;
};

/** A check read from doctor rows. */
const doctorCheck = (
  label: string,
  names: readonly string[],
  rows: DoctorEvent[]
): Check => {
  const mine = rows.filter((r) => names.includes(r.check));
  const failed = mine.find((r) => r.status === "fail");
  if (failed) {
    return { label, lines: [line(failed)], status: "fail" };
  }
  const warnings = mine.filter((r) => r.status === "warn");
  if (warnings.length > 0) {
    return { label, lines: warnings.map(line), status: "warn" };
  }
  const first = mine.find((r) => r.check === names[0]);
  return {
    label,
    lines: first ? [{ text: first.detail }] : [],
    status: "pass",
  };
};

/** A check not settled yet. Check 5 says what it waits for. */
const pending = (index: number): Check => ({
  label: labels[index] ?? "",
  lines: index === 4 ? [{ text: "Waiting for checks" }] : [],
  status: "waiting",
});

/** Check 1: the CLI this app runs, and its version. */
const cliCheck = ({ cli, version }: Findings): Check => {
  const [label] = labels;
  if (cli?.path === null) {
    const text = import.meta.env.DEV
      ? "Put agentx on your PATH"
      : "This app's agentx command line is missing";
    return { label, lines: [{ text }], status: "fail" };
  }
  if (version === undefined || typeof cli?.path !== "string") {
    return pending(0);
  }
  if ("error" in version) {
    return { label, lines: [problemLine(version.error)], status: "fail" };
  }
  return {
    label,
    lines: [{ text: `${version.cliVersion} at ${cli.path}` }],
    status: "pass",
  };
};

/** Doctor failed with no failed row of checks 2 to 4: it could not check at all. */
const couldNotCheck = (doctor: Findings["doctor"]) =>
  doctor?.error !== undefined &&
  !doctor.rows.some(
    (r) =>
      r.status === "fail" &&
      [...groups[2], ...groups[3], ...groups[4]].includes(r.check)
  );

/** Checks 2 to 4, from doctor's rows; check 4 counts skills once the first snapshot is in. */
const doctorChecks = ({ doctor, serve }: Findings): Check[] => {
  if (doctor === undefined) {
    return [pending(1), pending(2), pending(3)];
  }
  const checks = ([2, 3, 4] as const).map((n) =>
    doctorCheck(labels[n - 1], groups[n], doctor.rows)
  );
  if (couldNotCheck(doctor) && doctor.error !== undefined) {
    checks[0] = {
      label: labels[1],
      lines: [problemLine(doctor.error)],
      status: "fail",
    };
  }
  const account = checks.at(2);
  if (
    account?.status === "pass" &&
    serve !== undefined &&
    "snapshot" in serve
  ) {
    checks[2] = { ...account, lines: [{ text: counts(serve.snapshot) }] };
  }
  return checks;
};

/** Check 5: the serve child's first snapshot. */
const watchingCheck = ({ serve }: Findings): Check => {
  if (serve === undefined) {
    return pending(4);
  }
  if ("error" in serve) {
    return {
      label: labels[4],
      lines: [problemLine(serve.error)],
      status: "fail",
    };
  }
  const agents = plural(
    serve.snapshot.configurations.length,
    "agent",
    "agents"
  );
  return {
    label: labels[4],
    lines: [{ text: `Found ${agents}` }],
    status: "pass",
  };
};

/** The five checks, in order. While `running`, the first check not settled yet runs. */
export const readChecks = (found: Findings, running: boolean): Checks => {
  const all = [cliCheck(found), ...doctorChecks(found), watchingCheck(found)];
  // Checks settle in order: after the first failure the rest wait.
  const firstFailed = all.findIndex((c) => c.status === "fail");
  const checks = all.map((c, i) =>
    firstFailed !== -1 && i > firstFailed ? pending(i) : c
  );
  const firstOpen = checks.findIndex((c) => c.status === "waiting");
  if (running && firstFailed === -1 && firstOpen !== -1) {
    checks[firstOpen] = {
      label: labels[firstOpen] ?? "",
      lines: [],
      status: "running",
    };
  }

  let failure: string | undefined;
  if (firstFailed !== -1) {
    failure =
      firstFailed === 1 && couldNotCheck(found.doctor)
        ? doctorFailed
        : titles[firstFailed];
  }
  return {
    checks,
    failure,
    passed: checks.every((c) => c.status === "pass" || c.status === "warn"),
  };
};

const glyphs: Record<DoctorStatus, string> = {
  fail: "✗",
  info: "•",
  ok: "✓",
  warn: "!",
};

const problemRow = (problem: Problem) =>
  problem.hint === undefined
    ? { detail: problem.message }
    : { detail: problem.message, hint: problem.hint };

interface ReportRow {
  check: string;
  status: DoctorStatus;
  detail: string;
  hint?: string;
}

/**
 * The doctor output as plain text in the CLI's own layout: one aligned row per check, then the
 * warnings and failures again with their hints. A CLI that did not run, and a serve child that
 * stopped, add a row of their own.
 */
export const report = (found: Findings): string => {
  const rows: ReportRow[] = [];
  if (found.cli?.path === null) {
    rows.push({
      check: "agentx",
      detail: "This app's agentx command line is missing",
      status: "fail",
    });
  } else if (found.version !== undefined && "error" in found.version) {
    rows.push({
      check: "agentx",
      status: "fail",
      ...problemRow(found.version.error),
    });
  }
  rows.push(...(found.doctor?.rows ?? []));
  if (found.doctor?.error && !rows.some((r) => r.status === "fail")) {
    rows.push({
      check: "doctor",
      status: "fail",
      ...problemRow(found.doctor.error),
    });
  }
  if (found.serve !== undefined && "error" in found.serve) {
    rows.push({
      check: "serve",
      status: "fail",
      ...problemRow(found.serve.error),
    });
  }

  const checkWidth = Math.max(...rows.map((r) => r.check.length));
  const statusWidth = Math.max(...rows.map((r) => r.status.length));
  const out = rows.map((r) =>
    `${glyphs[r.status]} ${r.check.padEnd(checkWidth)}  ${r.status.padEnd(statusWidth)}  ${r.detail}`.trimEnd()
  );
  const issues = rows.filter((r) => r.status === "warn" || r.status === "fail");
  out.push("");
  if (issues.length === 0) {
    out.push("✓ No issues detected");
  } else {
    out.push(plural(issues.length, "issue", "issues"));
    // The hint sits under the detail, after the glyph, the check and two spaces.
    const indent = " ".repeat(2 + checkWidth + 2);
    for (const r of issues) {
      out.push(
        `${glyphs[r.status]} ${r.check.padEnd(checkWidth)}  ${r.detail}`
      );
      if (r.hint !== undefined) {
        out.push(`${indent}hint: ${r.hint}`);
      }
    }
  }
  return `${out.join("\n")}\n`;
};
