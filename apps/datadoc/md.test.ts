import { test } from "node:test";
import assert from "node:assert/strict";
import { renderMd } from "./md.ts";

test("renders one table without a heading", () => {
  const md = renderMd([{ name: "a.tsv", header: ["h1", "h2"], rows: [["a", "b"], ["c", "d"]] }]);
  assert.equal(md, "| h1 | h2 |\n| --- | --- |\n| a | b |\n| c | d |\n");
});

test("renders multiple tables with ## name and a blank line between", () => {
  const md = renderMd([
    { name: "a.tsv", header: ["x"], rows: [["1"]] },
    { name: "b|c.tsv", header: ["y"], rows: [["2"]] },
  ]);
  assert.equal(md, "## a.tsv\n\n| x |\n| --- |\n| 1 |\n\n## b|c.tsv\n\n| y |\n| --- |\n| 2 |\n");
});

test("escapes | and turns newlines into <br>, leaving backslashes", () => {
  const md = renderMd([{ name: "a.tsv", header: ["k|v"], rows: [["a|b\nc\\d"]] }]);
  assert.equal(md, "| k\\|v |\n| --- |\n| a\\|b<br>c\\d |\n");
});

test("renders header only when there are no rows", () => {
  assert.equal(renderMd([{ name: "a.tsv", header: ["x", "y"], rows: [] }]), "| x | y |\n| --- | --- |\n");
});

test("returns an empty string for no tables", () => {
  assert.equal(renderMd([]), "");
});
