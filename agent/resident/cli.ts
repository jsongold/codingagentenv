// resident-size [--tokens] — size of the text that loads into every session, printed as TSV (CAO-143).
// Measures only; changes no resident text. Reads ~/.claude and never writes there, except that
// `--tokens` starts one `claude -p` session, which Claude Code records in its own transcript.
import { spawnSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { calls } from "../tokens/aggregate.ts";

type Row = { target: string; chars: string; bytes: string; lines: string; note: string };

const HEADER = ["target", "chars", "bytes", "lines", "note"].join("\t");
const NA = "NA";
const USAGE = "usage: resident-size [--tokens]";
const home = homedir();
const cwd = process.cwd();
const repoRoot = join(dirname(fileURLToPath(import.meta.url)), "..", "..");

function fail(message: string): never {
  console.error(`${message}\n${USAGE}`);
  process.exit(1);
}

const argv = process.argv.slice(2);
for (const arg of argv) if (arg !== "--tokens") fail(`unknown argument: ${arg}`);
const withTokens = argv.includes("--tokens");

const read = (path: string): string | undefined => {
  try {
    return readFileSync(path, "utf8");
  } catch {
    return undefined;
  }
};

// Claude Code names a project directory by replacing every non-alphanumeric character with "-".
const encodeProject = (dir: string): string => dir.replace(/[^A-Za-z0-9]/g, "-");

// The note is one TSV field, so line breaks and tabs are flattened.
const oneLine = (text: string): string => text.replace(/\s+/g, " ").trim();

const lineCount = (text: string): number =>
  text === "" ? 0 : text.split("\n").length - (text.endsWith("\n") ? 1 : 0);

const naRow = (target: string, note: string): Row => ({ target, chars: NA, bytes: NA, lines: NA, note: oneLine(note) });

function textRow(target: string, text: string, note = ""): Row {
  return {
    target,
    chars: String([...text].length),
    bytes: String(Buffer.byteLength(text, "utf8")),
    lines: String(lineCount(text)),
    note: oneLine(note),
  };
}

function fileRow(target: string, path: string): Row {
  const text = read(path);
  return text === undefined ? naRow(target, `not found: ${path}`) : textRow(target, text);
}

// The value of `description:` in the YAML frontmatter; handles plain, quoted and block (`>-`, `|`) forms.
function frontmatterDescription(text: string): string | undefined {
  const frontmatter = text.match(/^---\n([\s\S]*?)\n---/)?.[1];
  if (frontmatter === undefined) return undefined;
  const lines = frontmatter.split("\n");
  const i = lines.findIndex((line) => line.startsWith("description:"));
  if (i < 0) return undefined;
  let value = lines[i].slice("description:".length).trim();
  if (/^[>|][-+]?$/.test(value)) {
    const block: string[] = [];
    for (const line of lines.slice(i + 1)) {
      if (!/^\s/.test(line)) break;
      block.push(line.trim());
    }
    value = block.join(value.startsWith(">") ? " " : "\n");
  }
  return /^(["']).*\1$/s.test(value) ? value.slice(1, -1) : value;
}

function skillDescriptionsRow(): Row {
  const target = "skill_descriptions_user";
  const dir = join(home, ".claude", "skills");
  let names: string[];
  try {
    names = readdirSync(dir).sort();
  } catch {
    return naRow(target, `not found: ${dir}`);
  }
  let chars = 0;
  let skills = 0;
  const missing: string[] = [];
  for (const name of names) {
    const text = read(join(dir, name, "SKILL.md"));
    if (text === undefined) continue;
    skills++;
    const description = frontmatterDescription(text);
    if (description === undefined) missing.push(name);
    else chars += [...description].length;
  }
  const note = `${skills} skills${missing.length > 0 ? `; no description: ${missing.join(", ")}` : ""}`;
  return { target, chars: String(chars), bytes: NA, lines: NA, note };
}

// Only the harness SessionStart command runs. The other SessionStart commands (a notification and
// an Orca send) have side effects, so they are counted but not run.
function sessionStartHookRow(): Row {
  const target = "session_start_hook";
  const settings = read(join(home, ".claude", "settings.json"));
  if (settings === undefined) return naRow(target, `not found: ${join(home, ".claude", "settings.json")}`);
  let groups: { hooks?: { type?: string; command?: string; timeout?: number }[] }[];
  try {
    groups = JSON.parse(settings).hooks?.SessionStart ?? [];
  } catch {
    return naRow(target, "settings.json is not valid JSON");
  }
  const hooks = groups.flatMap((group) => group.hooks ?? []).filter((hook) => hook.type === "command");
  const harness = hooks.filter((hook) => hook.command?.includes("harness-session-start"));
  const others = hooks.length - harness.length;
  const note = `harness-session-start only; ${others} other SessionStart command(s) not run (side effects)`;
  if (harness.length !== 1 || harness[0].command === undefined) {
    return naRow(target, `expected 1 harness SessionStart command, found ${harness.length}; ${note}`);
  }
  const hook = harness[0];
  const run = spawnSync("bash", ["-c", hook.command ?? ""], {
    input: "",
    encoding: "utf8",
    timeout: (hook.timeout ?? 10) * 1000,
  });
  if (run.error || run.status !== 0) {
    const reason = run.error?.message ?? (run.stderr.split("\n")[0] || `exit ${run.status}`);
    return naRow(target, `command failed: ${reason}; ${note}`);
  }
  return textRow(target, run.stdout, note);
}

// Starts a fresh session with a minimal prompt and reads the first API call's input side from its transcript.
function sessionFirstCallTokensRow(): Row {
  const target = "session_first_call_tokens";
  const id = randomUUID();
  const claude = spawnSync("claude", ["-p", "ok", "--session-id", id], {
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
    timeout: 300_000,
  });
  if (claude.error || claude.status !== 0) {
    const reason = claude.error?.message ?? (claude.stderr.split("\n")[0] || `exit ${claude.status}`);
    return naRow(target, `claude -p failed: ${reason}`);
  }
  const transcript = join(home, ".claude", "projects", encodeProject(cwd), `${id}.jsonl`);
  let first;
  try {
    first = [...calls(transcript).values()][0];
  } catch {
    return naRow(target, `transcript not found: ${transcript}`);
  }
  if (first === undefined) return naRow(target, `no assistant usage in ${transcript}`);
  const num = (v: unknown): number => (typeof v === "number" ? v : 0);
  const usage = first.usage;
  const total = num(usage.input_tokens) + num(usage.cache_creation_input_tokens) + num(usage.cache_read_input_tokens);
  return { target, chars: String(total), bytes: NA, lines: NA, note: `input side of the first API call, session ${id}` };
}

const rows: Row[] = [
  fileRow("global_claude_md", join(home, ".claude", "CLAUDE.md")),
  fileRow("harness_source", join(repoRoot, "global", "CLAUDE.harness.md")),
  fileRow("project_claude_md", join(cwd, "CLAUDE.md")),
  fileRow("memory_index", join(home, ".claude", "projects", encodeProject(cwd), "memory", "MEMORY.md")),
  skillDescriptionsRow(),
  sessionStartHookRow(),
];
if (withTokens) rows.push(sessionFirstCallTokensRow());

console.log(HEADER);
for (const row of rows) console.log([row.target, row.chars, row.bytes, row.lines, row.note].join("\t"));
