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

export const labelEsc = (s: string) => cell(s).replace(/\]/g, "\\]");

export function render(cl: Checklist): string {
  const out = [`Purpose: ${cell(cl.purpose)}`];
  if (cl.deadline) out.push(`Deadline: ${cl.deadline}`);
  out.push("", "Decisions", ...cl.decisions.map((d) => `- ${d.date}: ${d.text.replace(/\r?\n/g, " ")}`));
  for (const p of PRIORITIES) {
    out.push("", `## ${p}`, "| # | Item | Spec | Impl | Links |", "|---|---|---|---|---|");
    for (const it of cl.items.filter((i) => i.priority === p)) {
      const links = it.links.map((l) => `[${labelEsc(l.label)}](${l.url})`).join(", ");
      out.push(`| ${it.id} | ${cell(it.title)} | ${it.spec} | ${it.impl} | ${links} |`);
    }
  }
  return out.join("\n");
}
