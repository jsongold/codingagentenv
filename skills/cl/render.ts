// Rendering: CL view, list table, cell escaping, local date helpers.
import { PRIORITIES } from "./model.ts";
import type { Checklist, Item } from "./model.ts";
import type { Found } from "./find.ts";

export const isOpen = (it: Item) => !(it.spec === "decided" && it.impl !== "todo");

// Markdown table cell / link label escaping.
export const cell = (s: string) => s.replace(/\|/g, "\\|").replace(/\r?\n/g, " ");
export const localDate = (d: Date) => d.toLocaleDateString("sv-SE");
export const localDateTime = (d: Date) => `${localDate(d)} ${d.toLocaleTimeString("sv-SE").slice(0, 5)}`;

export function list(rows: Found[]): string {
  if (rows.length === 0) return "No checklists.";
  const withStore = rows.some((r) => r.cl.store);
  const lines = [`| Name | Purpose | Open | Updated | This session |${withStore ? " Pending |" : ""}`, `|---|---|---|---|---|${withStore ? "---|" : ""}`];
  for (const r of rows) {
    const updated = localDateTime(new Date(r.mtime));
    lines.push(`| ${cell(r.cl.name)} | ${cell(r.cl.purpose)} | ${r.cl.items.filter(isOpen).length} | ${updated} | ${r.own ? "yes" : ""} |${withStore ? ` ${r.cl.store ? r.cl.log.length - r.cl.synced : ""} |` : ""}`);
  }
  return lines.join("\n");
}

const SPEC_MARK = { todo: "·", decided: "✓" };
const IMPL_MARK = { todo: "·", done: "✓", "n/a": "–" };

// Display width: East Asian wide characters count 2.
const WIDE = [[0x1100, 0x115f], [0x2e80, 0xa4cf], [0xac00, 0xd7a3], [0xf900, 0xfaff], [0xfe30, 0xfe4f], [0xff00, 0xff60], [0xffe0, 0xffe6]];
const width = (s: string) => [...s].reduce((w, ch) => w + (WIDE.some(([a, b]) => ch.codePointAt(0)! >= a && ch.codePointAt(0)! <= b) ? 2 : 1), 0);
const left = (s: string, w: number) => s + " ".repeat(w - width(s));
const right = (s: string, w: number) => " ".repeat(w - width(s)) + s;
const center = (s: string, w: number) => " ".repeat(Math.floor((w - width(s)) / 2)) + s + " ".repeat(Math.ceil((w - width(s)) / 2));

export function render(cl: Checklist): string {
  const out = [`${cl.name} — ${cell(cl.purpose)}${cl.deadline ? ` · due ${cl.deadline}` : ""}`];
  if (cl.decisions.length) out.push(`Decisions: ${cl.decisions.map((d) => `${d.date.slice(5)} ${d.text.replace(/\r?\n/g, " ")}`).join(" · ")}`);
  const foot: string[] = [];
  const items = [...cl.items].sort((a, b) => PRIORITIES.indexOf(a.priority) - PRIORITIES.indexOf(b.priority) || a.id - b.id);
  const rows = items.map((it) => {
    const nums = it.links.map((l) => foot.push(`[${foot.length + 1}] ${l.label.replace(/\r?\n/g, " ")} ${l.url}`));
    return [String(it.id), it.priority, cell(it.title), SPEC_MARK[it.spec], IMPL_MARK[it.impl], nums.length ? `[${nums.join(",")}]` : ""];
  });
  const head = ["#", "P", "Item", "Spec", "Impl", "Links"];
  const w = head.map((h, i) => Math.max(width(h), ...rows.map((r) => width(r[i]!))));
  w[0] = Math.max(w[0]!, 2);
  const align = [right, left, left, center, center, left];
  const line = (cells: string[], fmt: typeof align) => `| ${cells.map((c, i) => fmt[i]!(c, w[i]!)).join(" | ")} |`;
  out.push("", line(head, [left, left, left, left, left, left]), `|${w.map((n) => "-".repeat(n + 2)).join("|")}|`, ...rows.map((r) => line(r, align)));
  out.push("", "✓ decided/done · todo – n/a", ...foot);
  return out.join("\n");
}
