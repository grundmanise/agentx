// Names and text that break layouts: unicode, emoji, right-to-left scripts, very long names and
// descriptions, Markdown and HTML in descriptions, and spaces in paths where agentx takes them.
import {
  addAgents,
  agentx,
  claudePlugins,
  claudeServers,
  cursorServers,
  hub,
  newHubSkill,
  sourceRepo,
  unmanagedSkills,
} from "./kit.ts";
import type { Machine, Skill } from "./kit.ts";

const markdown =
  "**Bold**, _italic_, `code`, [a link](https://example.com), # not a heading, | a | table |";
const html =
  "<script>alert('x')</script> <b>bold</b> &amp; &lt;tag&gt; <img src=x onerror=alert(1)>";
const arabic =
  "تساعدك هذه المهارة على كتابة رسائل واضحة، with English words and 123 numbers";
const hebrew = "כלי לבדיקת קוד לפני מיזוג";
const emoji = "🚀 Ship it 🎉 with 👨‍👩‍👧 and flags 🇯🇵🇩🇪";
const combining = "Z̷a̴l̶g̸o̵ text and a bidi override: ‮gnp.exe‬";
const long = Array.from(
  { length: 24 },
  (_, i) =>
    `Sentence ${i + 1} of a description that goes on far longer than any card was drawn for.`
).join(" ");
const lines = "First line.\nSecond line.\n\nA paragraph after a blank line.";

const longName = `a-really-long-skill-name-${"that-keeps-going-".repeat(8)}end`;

const sourceSkills: Skill[] = [
  { description: emoji, folder: "skills/résumé helper", name: "résumé-helper" },
  { description: markdown, name: "PDF-Tools" },
  { description: html, name: "R&D-notes" },
  { description: arabic, folder: "skills/arabic", name: "مساعد-الكتابة" },
  { description: hebrew, folder: "skills/hebrew", name: "בודק-קוד" },
  {
    description: "日本語の説明文です。",
    folder: "skills/cjk",
    name: "日本語レビュー",
  },
  { description: emoji, folder: "skills/family", name: "family-👨‍👩‍👧" },
  { description: long, folder: "skills/long", name: longName },
  { description: combining, name: "v2.0_snake_case" },
  {
    description: lines,
    folder: "Team Skills/with spaces",
    name: "it's-quoted",
  },
  { description: "", name: "no-description" },
];

const hubSkills: Skill[] = [
  {
    description: long,
    name: "a-hub-skill-name-that-is-exactly-sixty-four-characters-long-xyzw",
  },
  { description: `${markdown} ${html}`, name: "markdown-and-html" },
  { description: `${arabic} ${emoji}`, name: "mixed-direction" },
];

export const hostile = (m: Machine): void => {
  addAgents(m, ["claude-code", "codex", "cursor", "gemini-cli"]);
  const names = sourceRepo(m, "intl-team/skills", sourceSkills);
  const longRepo = sourceRepo(
    m,
    "a-very-long-organisation-name-for-testing/an-even-longer-repository-name-holding-skills",
    [{ description: long, name: "plain-skill" }]
  );
  unmanagedSkills(m, "claude-code", [
    { description: "A folder name with spaces", name: "My Notes (old)" },
    { description: html, name: "<img src=x onerror=alert(1)>" },
    { description: hebrew, name: "ملاحظات" },
    { description: emoji, name: "🎉 party" },
    { description: combining, name: "naïve-notes" },
  ]);
  claudeServers(m, {
    "<img src=x onerror=alert(1)>": { command: "node", type: "stdio" },
    "café ☕": {
      args: ["-y", "@acme-labs/café-mcp"],
      command: "npx",
      type: "stdio",
    },
    [`server-${"with-a-long-name-".repeat(6)}end`]: {
      type: "http",
      url: `https://mcp.example.com/${"very/long/path/".repeat(8)}mcp`,
    },
    "שרת-בדיקה": { type: "http", url: "https://mcp.example.com/בדיקה" },
  });
  cursorServers(m, {
    日本語サーバー: { args: ["--name", "日本語"], command: "uvx" },
  });
  claudePlugins(m, [
    {
      description: `${emoji} ${markdown}`,
      enabled: true,
      marketplace: "ünïcode-market",
      name: "δοκιμή-plugin",
      servers: { "plugin server 🚀": { command: "node" } },
      skills: [
        { description: html, name: "plugin-skill-ü" },
        { description: long, name: longName },
      ],
      version: "0.0.1-beta+ü",
    },
  ]);

  agentx(m, "skill", "add", names.url, "--all");
  agentx(m, "skill", "add", longRepo.url, "--all");
  hub(m, "intl-team/agentx-hub");
  for (const skill of hubSkills) {
    newHubSkill(m, skill);
  }
  agentx(m, "skill", "publish", ...hubSkills.map((skill) => skill.name));
  m.made.push(`${hubSkills.length} hub skills, published`);
};
