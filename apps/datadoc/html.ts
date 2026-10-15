// renderHtml turns Table[] into a self-contained HTML fragment: each table gets a
// multi-select filter in its header, a row counter, and the styles and script that drive them.
import type { Table } from "./table.ts";

function escape(s: string): string {
  return s.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;");
}

// escapeAttr also escapes quotes, for values placed in double-quoted attributes.
function escapeAttr(s: string): string {
  return escape(s).replaceAll('"', "&quot;");
}

// rowVisible decides whether a row stays visible. selected[c] holds the values picked in
// column c; an empty list means column c is not filtered. Columns combine with AND, the
// values inside one column with OR. Kept as plain JS: it is embedded in the page and tested as is.
export const FILTER_FN = `function rowVisible(row, selected) {
  for (var c = 0; c < selected.length; c++) {
    if (selected[c].length > 0 && selected[c].indexOf(row[c]) < 0) return false;
  }
  return true;
}`;

const STYLE = `<style>
.dd { margin: 1em 0; font-family: system-ui, sans-serif; font-size: 14px; }
.dd table { border-collapse: collapse; }
.dd th, .dd td { border: 1px solid #cbd5e1; padding: 4px 8px; text-align: left; vertical-align: top; }
.dd th { background: #dbeafe; font-weight: 600; }
.dd-filter { position: relative; }
.dd-filter summary { cursor: pointer; list-style: none; }
.dd-filter summary::-webkit-details-marker { display: none; }
.dd-filter summary::after { content: " ▾"; font-size: 11px; color: #475569; }
.dd-options { position: absolute; z-index: 1; background: #fff; border: 1px solid #94a3b8; padding: 4px 8px; max-height: 240px; overflow-y: auto; min-width: 160px; font-weight: 400; white-space: nowrap; }
.dd-options label { display: block; }
.dd-count { color: #475569; margin: 0 0 4px; }
[hidden] { display: none !important; }
</style>`;

const SCRIPT = `<script>
(function () {
  ${FILTER_FN}
  document.querySelectorAll(".dd").forEach(function (root) {
    var table = root.querySelector("table");
    var rows = Array.prototype.slice.call(table.tBodies[0].rows);
    var boxes = Array.prototype.slice.call(root.querySelectorAll("input[type=checkbox]"));
    var count = root.querySelector(".dd-count");
    var width = table.tHead.rows[0].cells.length;
    function update() {
      var selected = [];
      for (var c = 0; c < width; c++) selected.push([]);
      boxes.forEach(function (b) {
        if (b.checked) selected[Number(b.dataset.col)].push(b.value);
      });
      var shown = 0;
      rows.forEach(function (tr) {
        var values = Array.prototype.map.call(tr.cells, function (td) { return td.textContent; });
        var ok = rowVisible(values, selected);
        tr.hidden = !ok;
        if (ok) shown++;
      });
      count.textContent = "表示 " + shown + " / 全 " + rows.length + " 行";
    }
    boxes.forEach(function (b) { b.addEventListener("change", update); });
    update();
  });
})();
</script>`;

function renderFilter(header: string, col: number, values: string[]): string {
  const options = values
    .map((v) => `<label><input type="checkbox" data-col="${col}" value="${escapeAttr(v)}"> ${escape(v === "" ? "（空）" : v)}</label>`)
    .join("");
  return `<details class="dd-filter"><summary>${escape(header)}</summary><div class="dd-options">${options}</div></details>`;
}

// distinctValues lists the values of one column in order of first appearance.
function distinctValues(t: Table, col: number): string[] {
  return [...new Set(t.rows.map((r) => r[col]))];
}

function renderTable(t: Table): string {
  const th = t.header.map((h, c) => `<th>${renderFilter(h, c, distinctValues(t, c))}</th>`).join("");
  const trs = t.rows.map((r) => `<tr>${r.map((c) => `<td>${escape(c)}</td>`).join("")}</tr>`);
  const table = ["<table>", `<thead><tr>${th}</tr></thead>`, "<tbody>", ...trs, "</tbody>", "</table>"].join("\n");
  return `<div class="dd">\n<p class="dd-count">表示 ${t.rows.length} / 全 ${t.rows.length} 行</p>\n${table}\n</div>`;
}

export function renderHtml(tables: Table[]): string {
  if (tables.length === 0) return "";
  const titled = tables.length > 1;
  const blocks = tables.map((t) => (titled ? `<h2>${escape(t.name)}</h2>\n` : "") + renderTable(t));
  return [STYLE, ...blocks, SCRIPT].join("\n") + "\n";
}
