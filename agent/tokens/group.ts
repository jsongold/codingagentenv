// group: transcript files -> token usage per operation group. Counts only, like aggregate.
// A call belongs to the group of its first tool call (text / thinking only when it has none).
// Read / Write / Edit are split by the file kind of their target; Bash by its first word.
import { extname } from "node:path";
import { calls, type ToolUse } from "./aggregate.ts";
import type { TranscriptFile } from "./locate.ts";

export type GroupUsage = {
  group: string;
  calls: number;
  input: number;
  output: number;
  cacheRead: number;
  cacheCreation: number;
  total: number;
};

const FILE_KIND: Record<string, string> = {
  ".md": "md",
  ".html": "html", ".htm": "html",
  ".py": "code", ".go": "code", ".ts": "code", ".tsx": "code", ".js": "code", ".jsx": "code",
  ".mjs": "code", ".mts": "code", ".cjs": "code", ".sh": "code", ".zsh": "code", ".bash": "code",
  ".sql": "code", ".rb": "code", ".rs": "code", ".java": "code", ".css": "code",
  ".json": "config", ".jsonl": "config", ".yaml": "config", ".yml": "config", ".toml": "config",
  ".ini": "config", ".cfg": "config", ".env": "config", ".plist": "config",
  ".txt": "text", ".log": "text", ".out": "text", ".output": "text", ".diff": "text",
  ".png": "binary", ".jpg": "binary", ".jpeg": "binary", ".gif": "binary", ".pdf": "binary",
};

const BASH_HEADS = new Set(["git", "gh", "node", "npm", "npx", "go", "python3", "grep", "rg", "cat", "ls", "sed", "find", "tail"]);
const SUBAGENT = new Set(["Agent", "SendMessage", "SubagentHandback", "ListAgents"]);
const WEB = new Set(["WebFetch", "WebSearch"]);
const SKILL = new Set(["Skill", "ToolSearch", "Workflow"]);
const WAIT = new Set(["Monitor", "ScheduleWakeup", "RemoteTrigger"]);
const ASK = new Set(["AskUserQuestion", "ExitPlanMode"]);
const FILE_TOOLS = new Set(["Read", "Write", "Edit", "MultiEdit"]);

export function fileKind(path: string): string {
  const ext = extname(path).toLowerCase();
  if (ext === "") return "noext";
  return FILE_KIND[ext] ?? "other";
}

// `cd <dir> && <cmd>` and `export X=1; <cmd>` are setup, not the work, so the first
// command after them decides the group.
const SETUP_HEADS = new Set(["cd", "export", "unset", "set", "true", "echo"]);

function bashHead(command: string): string {
  for (const segment of command.split(/&&|\|\||;/)) {
    const head = segment.trim().split(/\s+/)[0] ?? "";
    if (head === "" || SETUP_HEADS.has(head)) continue;
    return BASH_HEADS.has(head) ? head : "other";
  }
  return "other";
}

// One call's group. Pure: the tool list in, the group label out.
export function groupOf(tools: ToolUse[]): string {
  const first = tools[0];
  if (!first) return "text";
  const { name, input } = first;
  if (name === "Bash") return `Bash:${bashHead(String(input.command ?? ""))}`;
  if (FILE_TOOLS.has(name)) return `${name}:${fileKind(String(input.file_path ?? ""))}`;
  if (name === "Grep" || name === "Glob") return "search";
  if (WEB.has(name)) return "web";
  if (SUBAGENT.has(name)) return "subagent";
  if (/^Task(Create|Update|List|Get|Stop)$/.test(name)) return "task";
  if (name.startsWith("mcp__atlassian__")) return "jira";
  if (name.startsWith("mcp__")) return "mcp";
  if (SKILL.has(name)) return "skill";
  if (WAIT.has(name)) return "wait";
  if (ASK.has(name)) return "ask";
  return `other:${name}`;
}

// Same counting rule as aggregate: a call that shows up in several files is counted once.
export function aggregateGroups(files: TranscriptFile[]): GroupUsage[] {
  const claimed = new Set<string>();
  const byGroup = new Map<string, GroupUsage>();
  for (const { path } of files) {
    for (const [key, { usage, tools }] of calls(path)) {
      if (claimed.has(key)) continue;
      claimed.add(key);
      const group = groupOf(tools);
      const g = byGroup.get(group) ?? { group, calls: 0, input: 0, output: 0, cacheRead: 0, cacheCreation: 0, total: 0 };
      g.calls += 1;
      g.input += Number(usage.input_tokens) || 0;
      g.output += Number(usage.output_tokens) || 0;
      g.cacheRead += Number(usage.cache_read_input_tokens) || 0;
      g.cacheCreation += Number(usage.cache_creation_input_tokens) || 0;
      g.total = g.input + g.output + g.cacheRead + g.cacheCreation;
      byGroup.set(group, g);
    }
  }
  return [...byGroup.values()].sort((a, b) => b.total - a.total || (a.group < b.group ? -1 : 1));
}
