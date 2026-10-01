// cl.ts <scratchpad> <cmd> [args]   cmd: new | add | spec | impl | link | decide | show | list
// Checklist (CL) storage and rendering. CLs live at <scratchpad>/cl/<name>.json.
// Invalid input -> message on stderr, exit 1. Mutating commands touch only the own scratchpad.
import { existsSync, mkdirSync, readFileSync, readdirSync, statSync, writeFileSync } from "node:fs";
import { basename, dirname, join } from "node:path";

type SpecState = "todo" | "decided";
type ImplState = "todo" | "done" | "n/a"; // n/a: nothing to implement (agreement, permission, etc.)
type Priority = "P0" | "P1" | "P2";
type Link = { label: string; url: string };
type Item = { id: number; priority: Priority; title: string; spec: SpecState; impl: ImplState; links: Link[] };
type Decision = { date: string; text: string }; // YYYY-MM-DD
type Checklist = { name: string; purpose: string; deadline?: string; decisions: Decision[]; items: Item[] };

const PRIORITIES: Priority[] = ["P0", "P1", "P2"];
const SPECS: SpecState[] = ["todo", "decided"];
const IMPLS: ImplState[] = ["todo", "done", "n/a"];

class CliError extends Error {}

function fail(msg: string): never {
  throw new CliError(msg);
}

function oneOf<T extends string>(values: readonly T[], v: string | undefined, what: string): T {
  if (!values.includes(v as T)) fail(`invalid ${what}: ${v ?? "(missing)"} (expected ${values.join("|")})`);
  return v as T;
}

function validate(cl: Checklist, path: string): Checklist {
  if (!cl || !cl.name || !cl.purpose || !Array.isArray(cl.items) || !Array.isArray(cl.decisions)) fail(`invalid checklist: ${path}`);
  for (const it of cl.items) {
    if (!it || !Array.isArray(it.links)) fail(`invalid checklist: ${path}`);
    oneOf(PRIORITIES, it.priority, "priority");
    oneOf(SPECS, it.spec, "spec");
    oneOf(IMPLS, it.impl, "impl");
  }
  return cl;
}

function load(path: string): Checklist {
  if (!existsSync(path)) fail(`not found: ${path}`);
  return validate(JSON.parse(readFileSync(path, "utf8")), path);
}

function save(path: string, cl: Checklist): void {
  validate(cl, path);
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, JSON.stringify(cl, null, 2) + "\n");
}

const isOpen = (it: Item) => !(it.spec === "decided" && it.impl !== "todo");

// All CLs in the repo: <scratchpad>/../../*/scratchpad/cl/*.json, newest first.
function scan(scratchpad: string): { path: string; cl: Checklist; mtime: number; own: boolean }[] {
  const repoDir = dirname(dirname(scratchpad));
  const own = join(scratchpad, "cl");
  const found = [];
  for (const session of existsSync(repoDir) ? readdirSync(repoDir) : []) {
    const dir = join(repoDir, session, "scratchpad", "cl");
    if (!existsSync(dir)) continue;
    for (const f of readdirSync(dir).filter((f) => f.endsWith(".json"))) {
      const path = join(dir, f);
      try {
        found.push({ path, cl: load(path), mtime: statSync(path).mtimeMs, own: dir === own });
      } catch (e) {
        console.error(`warning: skipped ${path}: ${(e as Error).message}`);
      }
    }
  }
  return found.sort((a, b) => b.mtime - a.mtime);
}

// Markdown table cell / link label escaping.
const cell = (s: string) => s.replace(/\|/g, "\\|").replace(/\r?\n/g, " ");
const labelEsc = (s: string) => cell(s).replace(/\]/g, "\\]");
const localDate = (d: Date) => d.toLocaleDateString("sv-SE");
const localDateTime = (d: Date) => `${localDate(d)} ${d.toLocaleTimeString("sv-SE").slice(0, 5)}`;

function list(scratchpad: string): string {
  const rows = scan(scratchpad);
  if (rows.length === 0) return "No checklists.";
  const lines = ["| Name | Purpose | Open | Updated | This session |", "|---|---|---|---|---|"];
  for (const r of rows) {
    const updated = localDateTime(new Date(r.mtime));
    lines.push(`| ${cell(r.cl.name)} | ${cell(r.cl.purpose)} | ${r.cl.items.filter(isOpen).length} | ${updated} | ${r.own ? "yes" : ""} |`);
  }
  return lines.join("\n");
}

function render(cl: Checklist): string {
  const out = [`Purpose: ${cl.purpose}`];
  if (cl.deadline) out.push(`Deadline: ${cl.deadline}`);
  out.push("", "Decisions", ...cl.decisions.map((d) => `- ${d.date}: ${d.text.replace(/\r?\n/g, " ")}`));
  for (const p of PRIORITIES) {
    out.push("", `## ${p}`, "| # | Item | Spec | Impl | Links |", "|---|---|---|---|---|");
    for (const it of cl.items.filter((i) => i.priority === p)) {
      const links = it.links.map((l) => `[${labelEsc(l.label)}](${l.url})`).join(", ");
      out.push(`| ${it.id} | ${cell(it.title)} | ${it.spec} | ${it.impl} | ${links} |`);
    }
  }
  return out.join("\n");
}

// Own scratchpad first, then the whole repo (newest wins; mention the others).
function show(scratchpad: string, name: string): string {
  const own = join(scratchpad, "cl", `${name}.json`);
  const others = (): number => scan(scratchpad).filter((r) => basename(r.path) === `${name}.json` && r.path !== own).length;
  const mk = (n: number) => (n > 0 ? `\n\nNote: ${n} other checklist(s) named ${name} exist in this repo.` : "");
  if (existsSync(own)) return render(load(own)) + mk(others());
  const hit = scan(scratchpad).find((r) => basename(r.path) === `${name}.json`);
  if (!hit) fail(`not found: ${name}`);
  return render(hit.cl) + mk(others() - 1);
}

function item(cl: Checklist, id: string | undefined): Item {
  const it = cl.items.find((i) => i.id === Number(id));
  if (!it) fail(`no such item: ${id ?? "(missing)"}`);
  return it;
}

function flag(args: string[], name: string): string | undefined {
  const i = args.indexOf(name);
  const v = i >= 0 ? args[i + 1] : undefined;
  return v === undefined || v === "" || v.startsWith("--") ? undefined : v;
}

function main(): void {
  const [scratchpad, cmd, name, ...rest] = process.argv.slice(2);
  if (!scratchpad || !cmd) fail("usage: cl.ts <scratchpad> new|add|spec|impl|link|decide|show|list [name] ...");
  if (cmd === "list") {
    console.log(list(scratchpad));
    process.exit(0);
  }
  if (!name || !/^[A-Za-z0-9][A-Za-z0-9._-]*$/.test(name)) fail(`invalid name: ${name ?? "(missing)"}`);
  if (cmd === "show") {
    console.log(show(scratchpad, name));
    process.exit(0);
  }

  const path = join(scratchpad, "cl", `${name}.json`);
  let cl: Checklist;
  if (cmd === "new") {
    if (existsSync(path)) fail(`already exists: ${name}`);
    const purpose = flag(rest, "--purpose") ?? fail("new requires --purpose <s>");
    cl = { name, purpose, decisions: [], items: [] };
    const deadline = flag(rest, "--deadline");
    if (deadline) cl.deadline = deadline;
  } else {
    cl = load(path);
    const [a, b] = rest;
    if (cmd === "add") {
      const priority = oneOf(PRIORITIES, a, "priority");
      const title = rest.slice(1).join(" ");
      if (!title) fail("add requires a title");
      const id = Math.max(0, ...cl.items.map((i) => i.id)) + 1;
      cl.items.push({ id, priority, title, spec: "todo", impl: "todo", links: [] });
    } else if (cmd === "spec") item(cl, a).spec = oneOf(SPECS, b, "spec");
    else if (cmd === "impl") item(cl, a).impl = oneOf(IMPLS, b, "impl");
    else if (cmd === "link") {
      const label = rest.slice(1, -1).join(" ");
      const url = rest.length > 2 ? rest[rest.length - 1] : "";
      if (!label || !url) fail("link requires <label> <url>");
      item(cl, a).links.push({ label, url });
    } else if (cmd === "decide") {
      const text = rest.join(" ");
      if (!text) fail("decide requires <text>");
      cl.decisions.push({ date: localDate(new Date()), text });
    } else fail(`unknown command: ${cmd}`);
  }
  save(path, cl);
  console.log(render(cl));
}

try {
  main();
} catch (e) {
  if (!(e instanceof CliError)) throw e;
  console.error(e.message);
  process.exit(1);
}
