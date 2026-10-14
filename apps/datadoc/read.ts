import { readFileSync } from "node:fs";
import { basename } from "node:path";
import type { Table } from "./table.ts";

const ESCAPES: Record<string, string> = { t: "\t", n: "\n", "\\": "\\" };

function fail(name: string, line: number, reason: string): never {
  throw new Error(`${name}:${line}: ${reason}`);
}

// unescapeCell restores \t \n \\ in one cell; any other escape is an error.
function unescapeCell(name: string, line: number, cell: string): string {
  let out = "";
  for (let i = 0; i < cell.length; i++) {
    const c = cell[i];
    if (c !== "\\") {
      out += c;
      continue;
    }
    if (i + 1 >= cell.length) fail(name, line, "trailing backslash");
    const next = cell[++i];
    const value = ESCAPES[next];
    if (value === undefined) fail(name, line, `invalid escape \\${next}`);
    out += value;
  }
  return out;
}

// parseTable validates TSV text and turns it into a Table.
export function parseTable(name: string, text: string): Table {
  if (text === "") fail(name, 1, "empty input");
  const cr = text.indexOf("\r");
  if (cr !== -1) {
    fail(name, text.slice(0, cr).split("\n").length, "CR is not allowed (use LF)");
  }
  const body = text.endsWith("\n") ? text.slice(0, -1) : text;
  const lines = body.split("\n");
  if (lines[0] === "") fail(name, 1, "empty header");
  const columns = lines[0].split("\t").length;
  const cells = lines.map((line, i) => {
    const raw = line.split("\t");
    if (raw.length !== columns) {
      fail(name, i + 1, `expected ${columns} columns, got ${raw.length}`);
    }
    return raw.map((cell) => unescapeCell(name, i + 1, cell));
  });
  return { name, header: cells[0], rows: cells.slice(1) };
}

// readTable reads a UTF-8 TSV file and parses it with its basename as the name.
export function readTable(path: string): Table {
  return parseTable(basename(path), readFileSync(path, "utf8"));
}
