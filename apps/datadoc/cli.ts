// datadoc CLI: reads TSV files (or stdin) and writes them as Markdown or HTML.
import { readFileSync, writeFileSync } from "node:fs";
import { parseTable, readTable } from "./read.ts";
import { renderMd } from "./md.ts";
import { renderHtml } from "./html.ts";
import type { Table } from "./table.ts";

const USAGE = "usage: datadoc <files...> (--md | --html) [-o <out>]  (use - for stdin)";

type Args = { files: string[]; format: "md" | "html"; out?: string };

// parseArgs turns argv into Args, or returns null when the usage is wrong.
function parseArgs(argv: string[]): Args | null {
  const files: string[] = [];
  let format: Args["format"] | undefined;
  let out: string | undefined;
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a === "--md" || a === "--html") {
      if (format) return null;
      format = a === "--md" ? "md" : "html";
    } else if (a === "-o") {
      if (out !== undefined || i + 1 >= argv.length) return null;
      out = argv[++i];
    } else if (a !== "-" && a.startsWith("-")) {
      return null;
    } else {
      files.push(a);
    }
  }
  if (!format || files.length === 0) return null;
  if (files.filter((f) => f === "-").length > 1) return null;
  return { files, format, out };
}

function loadTable(file: string): Table {
  return file === "-" ? parseTable("stdin", readFileSync(0, "utf8")) : readTable(file);
}

function main(argv: string[]): number {
  const args = parseArgs(argv);
  if (!args) {
    process.stderr.write(USAGE + "\n");
    return 1;
  }
  try {
    const tables = args.files.map(loadTable);
    const text = args.format === "md" ? renderMd(tables) : renderHtml(tables);
    if (args.out !== undefined) writeFileSync(args.out, text);
    else process.stdout.write(text);
    return 0;
  } catch (e) {
    process.stderr.write((e instanceof Error ? e.message : String(e)) + "\n");
    return 1;
  }
}

process.exitCode = main(process.argv.slice(2));
