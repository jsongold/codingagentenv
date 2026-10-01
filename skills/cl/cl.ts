// cl.ts <scratchpad> <cmd> [args]   cmd: new | add | spec | impl | link | decide | store | sync | synced | checkpoint | show | list
// Checklist (CL) storage and rendering. CLs live at <scratchpad>/cl/<name>.json.
// Invalid input -> message on stderr, exit 1. Mutating commands touch only the own scratchpad.
import { existsSync } from "node:fs";
import { join } from "node:path";
import { CliError, PRIORITIES, SPECS, IMPLS, fail, load, oneOf, record, save } from "./model.ts";
import type { Checklist, Item } from "./model.ts";
import { scan, show, where } from "./find.ts";
import { list, localDate, render } from "./render.ts";
import { managedBody, parseStore, pending, syncGithub } from "./store.ts";

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
  if (!scratchpad || !cmd) fail("usage: cl.ts <scratchpad> new|add|spec|impl|link|decide|store|sync|synced|checkpoint|show|where|list [name] ...");
  if (cmd === "list") {
    console.log(list(scan(scratchpad)));
    process.exit(0);
  }
  if (!name || !/^[A-Za-z0-9][A-Za-z0-9._-]*$/.test(name)) fail(`invalid name: ${name ?? "(missing)"}`);
  if (cmd === "show" || cmd === "where") {
    console.log(cmd === "show" ? show(scratchpad, name) : where(scratchpad, name));
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
    const [a, b] = rest;
    if (cmd === "add") {
      const priority = oneOf(PRIORITIES, a, "priority");
      const title = rest.slice(1).join(" ");
      if (!title) fail("add requires a title");
      const id = Math.max(0, ...cl.items.map((i) => i.id)) + 1;
      cl.items.push({ id, priority, title, spec: "todo", impl: "todo", links: [] });
      record(cl, "add", `#${id} ${priority}: ${title}`);
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
      const label = rest.slice(1, -1).join(" ");
      const url = rest.length > 2 ? rest[rest.length - 1] : "";
      if (!label || !url) fail("link requires <label> <url>");
      item(cl, a).links.push({ label, url });
      record(cl, "link", `#${a}: ${label} ${url}`);
    } else if (cmd === "decide") {
      const text = rest.join(" ");
      if (!text) fail("decide requires <text>");
      cl.decisions.push({ date: localDate(new Date()), text });
      record(cl, "decide", text);
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
}

try {
  main();
} catch (e) {
  if (!(e instanceof CliError)) throw e;
  console.error(e.message);
  process.exit(1);
}

