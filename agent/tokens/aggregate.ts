// aggregate: transcript files -> token usage per unit. Counts only; no message body leaves here.
// One API call = one (message.id, requestId). A call is written once per content block and
// the early rows carry a placeholder output_tokens, so the row with the largest value wins.
// A call that shows up in several files is counted in the first file read.
import { readFileSync } from "node:fs";
import type { TranscriptFile } from "./locate.ts";

export type UnitUsage = {
  unit: "session" | "subagent";
  session: string;
  agent?: string;
  description?: string;
  models: string[];
  calls: number;
  input: number;
  output: number;
  cacheRead: number;
  cacheCreation: number;
  total: number;
};

type Call = { model: string; usage: Record<string, unknown> };

const num = (v: unknown): number => (typeof v === "number" ? v : 0);

function calls(path: string): Map<string, Call> {
  const found = new Map<string, Call>();
  for (const line of readFileSync(path, "utf8").split("\n")) {
    let row;
    try {
      row = JSON.parse(line);
    } catch {
      continue;
    }
    const usage = row?.message?.usage;
    if (row?.type !== "assistant" || typeof usage !== "object" || usage === null) continue;
    const key = JSON.stringify([row.message.id ?? null, row.requestId ?? null]);
    const kept = found.get(key);
    if (kept && num(kept.usage.output_tokens) >= num(usage.output_tokens)) continue;
    found.set(key, { model: String(row.message.model), usage });
  }
  return found;
}

export function aggregate(files: TranscriptFile[]): UnitUsage[] {
  const claimed = new Set<string>();
  const units: UnitUsage[] = [];
  for (const { path, ...unit } of files) {
    const u: UnitUsage = { ...unit, models: [], calls: 0, input: 0, output: 0, cacheRead: 0, cacheCreation: 0, total: 0 };
    for (const [key, { model, usage }] of calls(path)) {
      if (claimed.has(key)) continue;
      claimed.add(key);
      if (!u.models.includes(model)) u.models.push(model);
      u.calls += 1;
      u.input += num(usage.input_tokens);
      u.output += num(usage.output_tokens);
      u.cacheRead += num(usage.cache_read_input_tokens);
      u.cacheCreation += num(usage.cache_creation_input_tokens);
    }
    u.total = u.input + u.output + u.cacheRead + u.cacheCreation;
    if (u.calls > 0) units.push(u);
  }
  return units;
}
