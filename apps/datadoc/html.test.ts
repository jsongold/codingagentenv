import { test } from "node:test";
import assert from "node:assert/strict";
import { renderHtml, FILTER_FN } from "./html.ts";

const rowVisible = new Function(`${FILTER_FN}; return rowVisible;`)() as (row: string[], selected: string[][]) => boolean;

test("renders one table with a filter header and a row counter", () => {
  const html = renderHtml([{ name: "a.tsv", header: ["h1", "h2"], rows: [["a", "b"]] }]);
  assert.match(html, /<div class="dd">\n<p class="dd-count">表示 1 \/ 全 1 行<\/p>/);
  assert.match(html, /<th><details class="dd-filter"><summary>h1<\/summary>/);
  assert.match(html, /<td>a<\/td><td>b<\/td>/);
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
  assert.match(html, /<h2>b\.tsv<\/h2>\n<div class="dd">\n<p class="dd-count">表示 2 \/ 全 2 行/);
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
  assert.match(html, /<p class="dd-count">表示 0 \/ 全 0 行<\/p>/);
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
