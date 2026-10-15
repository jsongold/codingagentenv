// renderHtml turns Table[] into a self-contained HTML fragment: each table gets a
// multi-select filter in its header, a row counter, row checkboxes with a select-all in the
// header, click-to-sort buttons (asc / desc / original), and the styles and script that drive them.
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

// selectState maps (checked visible rows, visible rows) to the header checkbox state:
// "none", "some" (indeterminate) or "all". Embedded in the page like FILTER_FN.
export const SELECT_FN = `function selectState(checked, visible) {
  if (checked === 0) return "none";
  return checked === visible ? "all" : "some";
}`;

// Sort helpers, embedded in the page like FILTER_FN. nextSort cycles asc -> desc -> none on one
// column (another column restarts at asc). isNumeric: every non-empty cell is a plain number.
// compareCells orders two cells ascending (dir 1) or descending (dir -1); empty cells always
// go last, numbers compare as numbers, anything else with localeCompare.
export const SORT_FN = `function nextSort(cur, col) {
  if (cur.col !== col || cur.dir === "none") return { col: col, dir: "asc" };
  return cur.dir === "asc" ? { col: col, dir: "desc" } : { col: -1, dir: "none" };
}
function isNumeric(values) {
  return values.every(function (v) {
    return v === "" || /^\\s*[-+]?(\\d+\\.?\\d*|\\.\\d+)([eE][-+]?\\d+)?\\s*$/.test(v);
  });
}
function compareCells(a, b, dir, numeric) {
  if (a === "" || b === "") return a === b ? 0 : a === "" ? 1 : -1;
  var r = numeric ? Number(a) - Number(b) : a.localeCompare(b);
  return dir * r;
}`;

const STYLE = `<style>
.dd { margin: 1em 0; font-family: system-ui, sans-serif; font-size: 14px; }
.dd table { border-collapse: collapse; }
.dd th, .dd td { border: 1px solid #cbd5e1; padding: 4px 8px; text-align: left; vertical-align: top; }
.dd th { background: #dbeafe; font-weight: 600; }
.dd-filter { position: relative; display: inline-block; }
.dd-sort { border: 0; background: none; cursor: pointer; color: #475569; font-size: 11px; padding: 0 2px; }
.dd-filter summary { cursor: pointer; list-style: none; }
.dd-filter summary::-webkit-details-marker { display: none; }
.dd-filter summary::after { content: " ▾"; font-size: 11px; color: #475569; }
.dd-options { position: absolute; z-index: 1; background: #fff; border: 1px solid #94a3b8; padding: 4px 8px; max-height: 240px; overflow-y: auto; min-width: 160px; font-weight: 400; white-space: nowrap; }
.dd-options label { display: block; }
.dd th.dd-sel, .dd td.dd-sel { width: 1%; text-align: center; }
.dd-count { color: #475569; margin: 0 0 4px; }
[hidden] { display: none !important; }
</style>`;

const SCRIPT = `<script>
(function () {
  ${FILTER_FN}
  ${SELECT_FN}
  ${SORT_FN}
  document.querySelectorAll(".dd").forEach(function (root) {
    var table = root.querySelector("table");
    var rows = Array.prototype.slice.call(table.tBodies[0].rows);
    var boxes = Array.prototype.slice.call(root.querySelectorAll(".dd-options input[type=checkbox]"));
    var rowBoxes = Array.prototype.slice.call(table.tBodies[0].querySelectorAll("input.dd-row"));
    var all = table.tHead.querySelector("input.dd-all");
    var count = root.querySelector(".dd-count");
    var width = table.tHead.rows[0].cells.length - 1;
    var sortBtns = Array.prototype.slice.call(table.tHead.querySelectorAll("button.dd-sort"));
    var sortState = { col: -1, dir: "none" };
    var tbody = table.tBodies[0];
    function applySort() {
      var order = rows.map(function (tr, i) { return { tr: tr, i: i }; });
      if (sortState.dir !== "none") {
        var cellText = function (tr) { return tr.cells[sortState.col + 1].textContent; };
        var numeric = isNumeric(rows.map(cellText));
        var dir = sortState.dir === "asc" ? 1 : -1;
        order.sort(function (x, y) {
          return compareCells(cellText(x.tr), cellText(y.tr), dir, numeric) || x.i - y.i;
        });
      }
      order.forEach(function (o) { tbody.appendChild(o.tr); });
      sortBtns.forEach(function (btn) {
        var on = Number(btn.dataset.col) === sortState.col;
        btn.textContent = !on ? "\u2195" : sortState.dir === "asc" ? "\u25B2" : "\u25BC";
      });
    }
    sortBtns.forEach(function (btn) {
      btn.addEventListener("click", function () {
        sortState = nextSort(sortState, Number(btn.dataset.col));
        applySort();
      });
    });
    function syncAll() {
      var visible = 0, checked = 0;
      rowBoxes.forEach(function (b) {
        if (b.closest("tr").hidden) return;
        visible++;
        if (b.checked) checked++;
      });
      var state = selectState(checked, visible);
      all.checked = state === "all";
      all.indeterminate = state === "some";
    }
    function update() {
      var selected = [];
      for (var c = 0; c < width; c++) selected.push([]);
      boxes.forEach(function (b) {
        if (b.checked) selected[Number(b.dataset.col)].push(b.value);
      });
      var shown = 0;
      rows.forEach(function (tr) {
        var values = Array.prototype.slice.call(tr.cells, 1).map(function (td) { return td.textContent; });
        var ok = rowVisible(values, selected);
        tr.hidden = !ok;
        if (ok) shown++;
      });
      count.textContent = "表示 " + shown + " / 全 " + rows.length + " 行";
      syncAll();
    }
    boxes.forEach(function (b) { b.addEventListener("change", update); });
    all.addEventListener("change", function () {
      rowBoxes.forEach(function (b) {
        if (!b.closest("tr").hidden) b.checked = all.checked;
      });
      syncAll();
    });
    rowBoxes.forEach(function (b) { b.addEventListener("change", syncAll); });
    update();
  });
})();
</script>`;

function renderFilter(header: string, col: number, values: string[]): string {
  const options = values
    .map((v) => `<label><input type="checkbox" data-col="${col}" value="${escapeAttr(v)}"> ${escape(v === "" ? "（空）" : v)}</label>`)
    .join("");
  return `<details class="dd-filter"><summary>${escape(header)}</summary><div class="dd-options">${options}</div></details><button type="button" class="dd-sort" data-col="${col}" aria-label="${escapeAttr(header)} を並べ替え">↕</button>`;
}

// distinctValues lists the values of one column in order of first appearance.
function distinctValues(t: Table, col: number): string[] {
  return [...new Set(t.rows.map((r) => r[col]))];
}

function renderTable(t: Table): string {
  const th = `<th class="dd-sel"><input type="checkbox" class="dd-all" aria-label="全選択"></th>` + t.header.map((h, c) => `<th>${renderFilter(h, c, distinctValues(t, c))}</th>`).join("");
  const trs = t.rows.map((r) => `<tr><td class="dd-sel"><input type="checkbox" class="dd-row"></td>${r.map((c) => `<td>${escape(c)}</td>`).join("")}</tr>`);
  const table = ["<table>", `<thead><tr>${th}</tr></thead>`, "<tbody>", ...trs, "</tbody>", "</table>"].join("\n");
  return `<div class="dd">\n<p class="dd-count">表示 ${t.rows.length} / 全 ${t.rows.length} 行</p>\n${table}\n</div>`;
}

export function renderHtml(tables: Table[]): string {
  if (tables.length === 0) return "";
  const titled = tables.length > 1;
  const blocks = tables.map((t) => (titled ? `<h2>${escape(t.name)}</h2>\n` : "") + renderTable(t));
  return [STYLE, ...blocks, SCRIPT].join("\n") + "\n";
}
