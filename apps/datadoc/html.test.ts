import { test } from "node:test";
import assert from "node:assert/strict";
import { renderHtml } from "./html.ts";

test("renders one table without a heading", () => {
  const html = renderHtml([{ name: "a.tsv", header: ["h1", "h2"], rows: [["a", "b"]] }]);
  assert.equal(
    html,
    "<table>\n<thead><tr><th>h1</th><th>h2</th></tr></thead>\n<tbody>\n<tr><td>a</td><td>b</td></tr>\n</tbody>\n</table>\n",
  );
});

test("puts <h2> before each table when there are several", () => {
  const html = renderHtml([
    { name: "a.tsv", header: ["x"], rows: [["1"]] },
    { name: "b.tsv", header: ["y"], rows: [["2"]] },
  ]);
  assert.equal(
    html,
    "<h2>a.tsv</h2>\n<table>\n<thead><tr><th>x</th></tr></thead>\n<tbody>\n<tr><td>1</td></tr>\n</tbody>\n</table>\n" +
      "<h2>b.tsv</h2>\n<table>\n<thead><tr><th>y</th></tr></thead>\n<tbody>\n<tr><td>2</td></tr>\n</tbody>\n</table>\n",
  );
});

test("escapes & < > in cells, headers and names, leaves quotes and newlines", () => {
  const html = renderHtml([
    { name: "a&<b>.tsv", header: ["<h>"], rows: [['&lt; "q"\nx']] },
    { name: "c.tsv", header: ["k"], rows: [] },
  ]);
  assert.match(html, /<h2>a&amp;&lt;b&gt;\.tsv<\/h2>/);
  assert.match(html, /<th>&lt;h&gt;<\/th>/);
  assert.ok(html.includes('<td>&amp;lt; "q"\nx</td>'));
});

test("renders header only with an empty tbody", () => {
  const html = renderHtml([{ name: "a.tsv", header: ["x", "y"], rows: [] }]);
  assert.equal(html, "<table>\n<thead><tr><th>x</th><th>y</th></tr></thead>\n<tbody>\n</tbody>\n</table>\n");
});

test("returns an empty string for no tables", () => {
  assert.equal(renderHtml([]), "");
});
