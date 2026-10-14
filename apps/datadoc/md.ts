import type { Table } from "./table.ts";

// renderMd turns tables into GFM tables; with 2+ tables each gets a "## name" heading.
export function renderMd(tables: Table[]): string {
  const blocks = tables.map((t) => {
    const table = renderTable(t);
    return tables.length > 1 ? `## ${t.name}\n\n${table}` : table;
  });
  return blocks.join("\n");
}

function renderTable(t: Table): string {
  const lines = [row(t.header), row(t.header.map(() => "---")), ...t.rows.map(row)];
  return lines.map((l) => l + "\n").join("");
}

function row(cells: string[]): string {
  return `| ${cells.map(cell).join(" | ")} |`;
}

function cell(s: string): string {
  return s.replaceAll("|", "\\|").replaceAll("\n", "<br>");
}
