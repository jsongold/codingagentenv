// cl.ts <scratchpad> <cmd> [args]   cmd: new | add | spec | impl | link | decide | store | sync | synced | checkpoint | show | list
// Checklist (CL) storage and rendering. CLs live at <scratchpad>/cl/<name>.json.
// Invalid input -> message on stderr, exit 1. Mutating commands touch only the own scratchpad.
import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, readdirSync, statSync, writeFileSync } from "node:fs";
import { basename, dirname, join } from "node:path";

type SpecState = "todo" | "decided";
type ImplState = "todo" | "done" | "n/a"; // n/a: nothing to implement (agreement, permission, etc.)
type Priority = "P0" | "P1" | "P2";
type Link = { label: string; url: string };
type Item = { id: number; priority: Priority; title: string; spec: SpecState; impl: ImplState; links: Link[] };
type Decision = { date: string; text: string }; // YYYY-MM-DD
type LogEntry = { at: string; cmd: string; change: string }; // ISO time
type Store = { kind: "github" | "jira"; url: string; commentId?: string };
type Checklist = {
  name: string;
  purpose: string;
  deadline?: string;
  decisions: Decision[];
  items: Item[];
  store?: Store;
  log: LogEntry[];
  synced: number; // count of log entries already pushed to the store
};

const PRIORITIES: Priority[] = ["P0", "P1", "P2"];
const SPECS: SpecState[] = ["todo", "decided"];
const IMPLS: ImplState[] = ["todo", "done", "n/a"];

function fail(msg: string): never {
  console.error(msg);
  process.exit(1);
}

function oneOf<T extends string>(values: readonly T[], v: string | undefined, what: string): T {
  if (!values.includes(v as T)) fail(`invalid ${what}: ${v ?? "(missing)"} (expected ${values.join("|")})`);
  return v as T;
}

function validate(cl: Checklist, path: string): Checklist {
  if (!cl.name || !cl.purpose) fail(`invalid checklist: ${path}`);
  for (const it of cl.items) {
    oneOf(PRIORITIES, it.priority, "priority");
    oneOf(SPECS, it.spec, "spec");
    oneOf(IMPLS, it.impl, "impl");
  }
  return cl;
}

function load(path: string): Checklist {
  if (!existsSync(path)) fail(`not found: ${path}`);
  const raw = JSON.parse(readFileSync(path, "utf8"));
  return validate({ ...raw, log: raw.log ?? [], synced: raw.synced ?? 0 }, path);
}

function save(path: string, cl: Checklist): void {
  validate(cl, path);
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, JSON.stringify(cl, null, 2) + "\n");
}

function parseStore(url: string | undefined): Store {
  const gh = /^https:\/\/github\.com\/[\w.-]+\/[\w.-]+\/issues\/\d+$/;
  const jira = /^https:\/\/[\w.-]+\/browse\/[A-Z][A-Z0-9_]*-\d+$/;
  if (url && gh.test(url)) return { kind: "github", url };
  if (url && jira.test(url)) return { kind: "jira", url };
  return fail(`invalid store url: ${url ?? "(missing)"} (expected https://github.com/<o>/<r>/issues/<n> or https://<host>/browse/<KEY>-<n>)`);
}

function record(cl: Checklist, cmd: string, change: string): void {
  cl.log.push({ at: new Date().toISOString(), cmd, change });
}

function gh(args: string[]): { id?: number } {
  try {
    return JSON.parse(execFileSync("gh", args, { encoding: "utf8" }) || "{}");
  } catch (e) {
    return fail(`gh failed: ${(e as Error).message}`);
  }
}

const pending = (cl: Checklist) => cl.log.slice(cl.synced);
const managedBody = (cl: Checklist) => `<!-- cl:${cl.name} -->\n${render(cl)}`;
const entryBody = (cl: Checklist, e: LogEntry) => `cl ${cl.name} ${e.at} ${e.cmd}: ${e.change}`;

// github: managed comment (create or edit) + one comment per pending entry. Nothing is marked synced on failure.
function syncGithub(cl: Checklist, store: Store): void {
  const m = /^https:\/\/github\.com\/([^/]+\/[^/]+)\/issues\/(\d+)$/.exec(store.url)!;
  const base = `repos/${m[1]}/issues`;
  if (store.commentId) gh(["api", "-X", "PATCH", `${base}/comments/${store.commentId}`, "-f", `body=${managedBody(cl)}`]);
  else store.commentId = String(gh(["api", "-X", "POST", `${base}/${m[2]}/comments`, "-f", `body=${managedBody(cl)}`]).id);
  for (const e of pending(cl)) gh(["api", "-X", "POST", `${base}/${m[2]}/comments`, "-f", `body=${entryBody(cl, e)}`]);
  cl.synced = cl.log.length;
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
      found.push({ path, cl: load(path), mtime: statSync(path).mtimeMs, own: dir === own });
    }
  }
  return found.sort((a, b) => b.mtime - a.mtime);
}

function list(scratchpad: string): string {
  const rows = scan(scratchpad);
  if (rows.length === 0) return "No checklists.";
  const withStore = rows.some((r) => r.cl.store);
  const lines = [`| Name | Purpose | Open | Updated | This session |${withStore ? " Pending |" : ""}`, `|---|---|---|---|---|${withStore ? "---|" : ""}`];
  for (const r of rows) {
    const updated = new Date(r.mtime).toISOString().slice(0, 16).replace("T", " ");
    lines.push(`| ${r.cl.name} | ${r.cl.purpose} | ${r.cl.items.filter(isOpen).length} | ${updated} | ${r.own ? "yes" : ""} |${withStore ? ` ${r.cl.store ? r.cl.log.length - r.cl.synced : ""} |` : ""}`);
  }
  return lines.join("\n");
}

function render(cl: Checklist): string {
  const out = [`Purpose: ${cl.purpose}`];
  if (cl.deadline) out.push(`Deadline: ${cl.deadline}`);
  out.push("", "Decisions", ...cl.decisions.map((d) => `- ${d.date}: ${d.text}`));
  for (const p of PRIORITIES) {
    out.push("", `## ${p}`, "| # | Item | Spec | Impl | Links |", "|---|---|---|---|---|");
    for (const it of cl.items.filter((i) => i.priority === p)) {
      const links = it.links.map((l) => `[${l.label}](${l.url})`).join(", ");
      out.push(`| ${it.id} | ${it.title} | ${it.spec} | ${it.impl} | ${links} |`);
    }
  }
  return out.join("\n");
}

// Own scratchpad first, then the whole repo (newest wins; mention the others).
function show(scratchpad: string, name: string): string {
  const own = join(scratchpad, "cl", `${name}.json`);
  const matches = scan(scratchpad).filter((r) => basename(r.path) === `${name}.json`);
  const hit = matches.find((r) => r.path === own) ?? matches[0];
  if (!hit) fail(`not found: ${name}`);
  const note = matches.length > 1 ? `\n\nNote: ${matches.length - 1} other checklist(s) named ${name} exist in this repo.` : "";
  return render(hit.cl) + note;
}

function item(cl: Checklist, id: string | undefined): Item {
  const it = cl.items.find((i) => i.id === Number(id));
  if (!it) fail(`no such item: ${id ?? "(missing)"}`);
  return it;
}

function flag(args: string[], name: string): string | undefined {
  const i = args.indexOf(name);
  return i >= 0 ? args[i + 1] : undefined;
}

const [scratchpad, cmd, name, ...rest] = process.argv.slice(2);
if (!scratchpad || !cmd) fail("usage: cl.ts <scratchpad> new|add|spec|impl|link|decide|store|sync|synced|checkpoint|show|list [name] ...");
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
  const store = flag(rest, "--store");
  cl = { name, purpose, decisions: [], items: [], log: [], synced: 0 };
  if (store) cl.store = parseStore(store);
  record(cl, "new", purpose);
  const deadline = flag(rest, "--deadline");
  if (deadline) cl.deadline = deadline;
} else {
  cl = load(path);
  const [a, b, c] = rest;
  if (cmd === "add") {
    const priority = oneOf(PRIORITIES, a, "priority");
    if (!b) fail("add requires a title");
    const id = Math.max(0, ...cl.items.map((i) => i.id)) + 1;
    cl.items.push({ id, priority, title: b, spec: "todo", impl: "todo", links: [] });
    record(cl, "add", `#${id} ${priority}: ${b}`);
  } else if (cmd === "spec") {
    const it = item(cl, a);
    const before = it.spec;
    it.spec = oneOf(SPECS, b, "spec");
    record(cl, "spec", `#${it.id}: ${before} -> ${it.spec}`);
  } else if (cmd === "impl") {
    const it = item(cl, a);
    const before = it.impl;
    it.impl = oneOf(IMPLS, b, "impl");
    record(cl, "impl", `#${it.id}: ${before} -> ${it.impl}`);
  } else if (cmd === "link") {
    if (!b || !c) fail("link requires <label> <url>");
    item(cl, a).links.push({ label: b, url: c });
    record(cl, "link", `#${a}: ${b} ${c}`);
  } else if (cmd === "decide") {
    if (!a) fail("decide requires <text>");
    cl.decisions.push({ date: new Date().toISOString().slice(0, 10), text: a });
    record(cl, "decide", a);
  } else if (cmd === "store") {
    cl.store = parseStore(a);
    record(cl, "store", a!);
  } else if (cmd === "checkpoint") record(cl, "checkpoint", "not synced");
  else if (cmd === "synced") {
    const n = Number(a);
    if (!Number.isInteger(n) || n < 0 || n > cl.log.length) fail(`invalid count: ${a ?? "(missing)"} (0..${cl.log.length})`);
    if (!cl.store) fail("no store set");
    cl.synced = n;
    if (b) cl.store.commentId = b;
  } else if (cmd === "sync") {
    if (!cl.store) fail("no store set (use: store <name> <url>)");
    if (cl.store.kind === "jira") {
      const { kind, url, commentId } = cl.store;
      console.log(JSON.stringify({ kind, url, commentId, body: managedBody(cl), pending: pending(cl), next: cl.log.length }, null, 2));
      process.exit(0);
    }
    const n = pending(cl).length;
    try {
      syncGithub(cl, cl.store);
    } finally {
      save(path, cl); // keeps commentId even if a later post failed; synced only moves on success
    }
    console.log(`synced ${n} log entries to ${cl.store.url}`);
    process.exit(0);
  } else fail(`unknown command: ${cmd}`);
}
save(path, cl);
console.log(render(cl));
