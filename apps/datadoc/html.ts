// renderHtml turns Table[] into an HTML fragment (no <html>/<head>, no styles).
import type { Table } from "./table.ts";

function escape(s: string): string {
  return s.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;");
}

function renderTable(t: Table): string {
  const th = t.header.map((h) => `<th>${escape(h)}</th>`).join("");
  const trs = t.rows.map((r) => `<tr>${r.map((c) => `<td>${escape(c)}</td>`).join("")}</tr>`);
  return ["<table>", `<thead><tr>${th}</tr></thead>`, "<tbody>", ...trs, "</tbody>", "</table>"].join("\n");
}

export function renderHtml(tables: Table[]): string {
  if (tables.length === 0) return "";
  const titled = tables.length > 1;
  const blocks = tables.map((t) => (titled ? `<h2>${escape(t.name)}</h2>\n` : "") + renderTable(t));
  return blocks.join("\n") + "\n";
}
