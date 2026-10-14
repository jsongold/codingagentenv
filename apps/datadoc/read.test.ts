import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { parseTable, readTable } from "./read.ts";

test("parses multiple columns and rows", () => {
  const t = parseTable("a.tsv", "id\tname\n1\tfoo\n2\tbar\n");
  assert.deepEqual(t, {
    name: "a.tsv",
    header: ["id", "name"],
    rows: [["1", "foo"], ["2", "bar"]],
  });
});

test("accepts input without a trailing newline", () => {
  assert.deepEqual(parseTable("a.tsv", "x\ty\n1\t2").rows, [["1", "2"]]);
});

test("restores \\t \\n \\\\ escapes", () => {
  const t = parseTable("a.tsv", 'k\tv\na\\tb\tc\\nd\\\\e\n"q"\t\n');
  assert.deepEqual(t.rows, [["a\tb", "c\nd\\e"], ['"q"', ""]]);
});

test("accepts header only", () => {
  assert.deepEqual(parseTable("a.tsv", "x\ty\n"), { name: "a.tsv", header: ["x", "y"], rows: [] });
});

test("rejects a row with a different column count", () => {
  assert.throws(() => parseTable("a.tsv", "x\ty\n1\t2\n3\n"), /^Error: a\.tsv:3: /);
});

test("rejects an extra blank line at the end", () => {
  assert.throws(() => parseTable("a.tsv", "x\ty\n1\t2\n\n"), /^Error: a\.tsv:3: /);
});

test("rejects an unknown escape", () => {
  assert.throws(() => parseTable("a.tsv", "x\n\\x\n"), /^Error: a\.tsv:2: invalid escape/);
});

test("rejects a trailing lone backslash", () => {
  assert.throws(() => parseTable("a.tsv", "x\ty\n1\t2\\\n"), /^Error: a\.tsv:2: trailing backslash/);
});

test("rejects CR with its line number", () => {
  assert.throws(() => parseTable("a.tsv", "x\n1\r\n"), /^Error: a\.tsv:2: CR/);
  assert.throws(() => parseTable("a.tsv", "x\r\n1\n"), /^Error: a\.tsv:1: CR/);
});

test("rejects empty input", () => {
  assert.throws(() => parseTable("a.tsv", ""), /^Error: a\.tsv:1: empty input/);
});

test("rejects an empty header line", () => {
  assert.throws(() => parseTable("a.tsv", "\n"), /^Error: a\.tsv:1: empty header/);
});

test("readTable uses the basename as the name", () => {
  const path = join(mkdtempSync(join(tmpdir(), "datadoc-")), "data.tsv");
  writeFileSync(path, "x\n1\n");
  assert.deepEqual(readTable(path), { name: "data.tsv", header: ["x"], rows: [["1"]] });
});
