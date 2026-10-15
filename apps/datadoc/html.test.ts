import { test } from "node:test";
import assert from "node:assert/strict";
import { renderHtml, FILTER_FN, SELECT_FN, SORT_FN } from "./html.ts";

const rowVisible = new Function(`${FILTER_FN}; return rowVisible;`)() as (row: string[], selected: string[][]) => boolean;

const selectState = new Function(`${SELECT_FN}; return selectState;`)() as (checked: number, visible: number) => string;

test("renders one table with a filter header and a row counter", () => {
  const html = renderHtml([{ name: "a.tsv", header: ["h1", "h2"], rows: [["a", "b"]] }]);
  assert.match(html, /<div class="dd">\n<p class="dd-count">表示 1 \/ 全 1 行（選択 0）<\/p>/);
  assert.match(html, /<th><details class="dd-filter"><summary>h1<\/summary>/);
  assert.match(html, /<td>a<\/td><td>b<\/td>/);
  assert.match(html, /<th class="dd-sel"><input type="checkbox" class="dd-all"/);
  assert.match(html, /<tr><td class="dd-sel"><input type="checkbox" class="dd-row"><\/td><td>a<\/td>/);
  assert.doesNotMatch(html, /<h2>/);
});

test("lists each column's distinct values once, in order, empty as （空）", () => {
  const html = renderHtml([{ name: "a.tsv", header: ["k"], rows: [["x"], ["y"], ["x"], [""]] }]);
  const values = [...html.matchAll(/value="([^"]*)"/g)].map((m) => m[1]);
  assert.deepEqual(values, ["x", "y", ""]);
  assert.match(html, /<label><input type="checkbox" data-col="0" value="">\s*（空）<\/label>/);
});

test("puts <h2> before each table when there are several, each with its own counter", () => {
  const html = renderHtml([
    { name: "a.tsv", header: ["x"], rows: [["1"]] },
    { name: "b.tsv", header: ["y"], rows: [["2"], ["3"]] },
  ]);
  assert.match(html, /<h2>a\.tsv<\/h2>\n<div class="dd">/);
  assert.match(html, /<h2>b\.tsv<\/h2>\n<div class="dd">\n<p class="dd-count">表示 2 \/ 全 2 行（選択 0）/);
});

test("escapes & < > in cells, headers and names, and quotes in filter values", () => {
  const html = renderHtml([
    { name: "a&<b>.tsv", header: ["<h>"], rows: [['&lt; "q"\nx']] },
    { name: "c.tsv", header: ["k"], rows: [] },
  ]);
  assert.match(html, /<h2>a&amp;&lt;b&gt;\.tsv<\/h2>/);
  assert.match(html, /<summary>&lt;h&gt;<\/summary>/);
  assert.ok(html.includes('<td>&amp;lt; "q"\nx</td>'));
  assert.ok(html.includes('value="&amp;lt; &quot;q&quot;\nx"'));
});

test("renders header only with an empty tbody and a zero counter", () => {
  const html = renderHtml([{ name: "a.tsv", header: ["x", "y"], rows: [] }]);
  assert.match(html, /<p class="dd-count">表示 0 \/ 全 0 行（選択 0）<\/p>/);
  assert.match(html, /<tbody>\n<\/tbody>/);
});

test("embeds the styles (light blue header) and the filter script", () => {
  const html = renderHtml([{ name: "a.tsv", header: ["x"], rows: [["1"]] }]);
  assert.match(html, /<style>[\s\S]*background: #dbeafe/);
  assert.match(html, /<script>[\s\S]*rowVisible[\s\S]*<\/script>/);
});

test("returns an empty string for no tables", () => {
  assert.equal(renderHtml([]), "");
});

test("rowVisible: no selection keeps every row", () => {
  assert.equal(rowVisible(["a", "b"], [[], []]), true);
});

test("rowVisible: values inside one column are OR-ed", () => {
  assert.equal(rowVisible(["a"], [["a", "b"]]), true);
  assert.equal(rowVisible(["c"], [["a", "b"]]), false);
});

test("rowVisible: columns are AND-ed", () => {
  assert.equal(rowVisible(["a", "x"], [["a"], ["y"]]), false);
  assert.equal(rowVisible(["a", "y"], [["a"], ["y"]]), true);
});

test("rowVisible: an empty cell matches the empty-string selection", () => {
  assert.equal(rowVisible([""], [[""]]), true);
  assert.equal(rowVisible(["a"], [[""]]), false);
});

test("renders one row checkbox per data row, none for the filter options", () => {
  const html = renderHtml([{ name: "a.tsv", header: ["k"], rows: [["x"], ["y"], ["z"]] }]);
  assert.equal(html.match(/class="dd-row"/g)?.length, 3);
  assert.equal(html.match(/class="dd-all"/g)?.length, 1);
});

test("selectState: none, some (indeterminate) and all among visible rows", () => {
  assert.equal(selectState(0, 3), "none");
  assert.equal(selectState(0, 0), "none");
  assert.equal(selectState(2, 3), "some");
  assert.equal(selectState(3, 3), "all");
});

const sortFns = new Function(`${SORT_FN}; return { nextSort, isNumeric, compareCells };`)() as {
  nextSort: (cur: { col: number; dir: string }, col: number) => { col: number; dir: string };
  isNumeric: (values: string[]) => boolean;
  compareCells: (a: string, b: string, dir: number, numeric: boolean) => number;
};

test("renders one sort button per data column, none for the checkbox column", () => {
  const html = renderHtml([{ name: "a.tsv", header: ["h1", "h2"], rows: [["a", "b"]] }]);
  assert.equal(html.match(/<button type="button" class="dd-sort"/g)?.length, 2);
  assert.match(html, /<button type="button" class="dd-sort" data-col="1"/);
  assert.match(html, /<th class="dd-sel"><input type="checkbox" class="dd-all" aria-label="全選択"><\/th>/);
});

test("nextSort: asc -> desc -> none on one column, another column restarts at asc", () => {
  const none = { col: -1, dir: "none" };
  const a = sortFns.nextSort(none, 1);
  assert.deepEqual(a, { col: 1, dir: "asc" });
  const d = sortFns.nextSort(a, 1);
  assert.deepEqual(d, { col: 1, dir: "desc" });
  assert.deepEqual(sortFns.nextSort(d, 1), none);
  assert.deepEqual(sortFns.nextSort(d, 0), { col: 0, dir: "asc" });
});

test("isNumeric: numbers and blanks only", () => {
  assert.equal(sortFns.isNumeric(["1", "-2.5", "", ".5", "1e3"]), true);
  assert.equal(sortFns.isNumeric(["1", "a"]), false);
  assert.equal(sortFns.isNumeric(["0x10"]), false);
});

test("compareCells: numeric order, not string order", () => {
  assert.ok(sortFns.compareCells("2", "10", 1, true) < 0);
  assert.ok(sortFns.compareCells("2", "10", 1, false) > 0);
  assert.ok(sortFns.compareCells("2", "10", -1, true) > 0);
});

test("compareCells: strings with localeCompare, descending flips", () => {
  assert.ok(sortFns.compareCells("a", "b", 1, false) < 0);
  assert.ok(sortFns.compareCells("a", "b", -1, false) > 0);
  assert.equal(sortFns.compareCells("a", "a", 1, false), 0);
});

test("compareCells: empty cells go last in both directions", () => {
  for (const numeric of [true, false]) {
    for (const dir of [1, -1]) {
      assert.ok(sortFns.compareCells("", "1", dir, numeric) > 0);
      assert.ok(sortFns.compareCells("1", "", dir, numeric) < 0);
      assert.equal(sortFns.compareCells("", "", dir, numeric), 0);
    }
  }
});

test("each filter starts with an all checkbox that is not a value, even when a value is \"all\"", () => {
  const html = renderHtml([{ name: "a.tsv", header: ["k", "m"], rows: [["all", "y"]] }]);
  assert.equal((html.match(/class="dd-opt-all"/g) ?? []).length, 2);
  assert.equal((html.match(/<details class="dd-filter"/g) ?? []).length, 2);
  assert.match(html, /<div class="dd-options"><label><input type="checkbox" class="dd-opt-all"> all<\/label><label><input type="checkbox" data-col="0" value="all">/);
});
