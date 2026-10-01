// Checklist types, validation, and JSON load/save.
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname } from "node:path";

export type SpecState = "todo" | "decided";
export type ImplState = "todo" | "done" | "n/a"; // n/a: nothing to implement (agreement, permission, etc.)
export type Priority = "P0" | "P1" | "P2";
export type Link = { label: string; url: string };
export type Item = {
  id: number;
  priority: Priority;
  title: string;
  spec: SpecState;
  how?: string; // how it will be done; required when spec becomes decided
  specUrl?: string; // where it was decided
  impl: ImplState;
  implUrl?: string; // where it was implemented (e.g. the PR)
  links: Link[];
};
export type Decision = { date: string; text: string }; // YYYY-MM-DD
export type LogEntry = { at: string; cmd: string; change: string }; // ISO time
export type Store = { kind: "github" | "jira"; url: string; commentId?: string };
export type Checklist = {
  name: string;
  purpose: string;
  deadline?: string;
  decisions: Decision[];
  items: Item[];
  store?: Store;
  log: LogEntry[];
  synced: number; // count of log entries already pushed to the store
};

export const PRIORITIES: Priority[] = ["P0", "P1", "P2"];
export const SPECS: SpecState[] = ["todo", "decided"];
export const IMPLS: ImplState[] = ["todo", "done", "n/a"];

export class CliError extends Error {}

export function fail(msg: string): never {
  throw new CliError(msg);
}

export function oneOf<T extends string>(values: readonly T[], v: string | undefined, what: string): T {
  if (!values.includes(v as T)) fail(`invalid ${what}: ${v ?? "(missing)"} (expected ${values.join("|")})`);
  return v as T;
}

export function validate(cl: Checklist, path: string): Checklist {
  if (!cl || !cl.name || !cl.purpose || !Array.isArray(cl.items) || !Array.isArray(cl.decisions)) fail(`invalid checklist: ${path}`);
  for (const it of cl.items) {
    if (!it || !Array.isArray(it.links)) fail(`invalid checklist: ${path}`);
    oneOf(PRIORITIES, it.priority, "priority");
    oneOf(SPECS, it.spec, "spec");
    oneOf(IMPLS, it.impl, "impl");
  }
  return cl;
}

export function load(path: string): Checklist {
  if (!existsSync(path)) fail(`not found: ${path}`);
  const raw = JSON.parse(readFileSync(path, "utf8"));
  return validate({ ...raw, log: raw?.log ?? [], synced: raw?.synced ?? 0 }, path);
}

export function save(path: string, cl: Checklist): void {
  validate(cl, path);
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, JSON.stringify(cl, null, 2) + "\n");
}

export function record(cl: Checklist, cmd: string, change: string): void {
  cl.log.push({ at: new Date().toISOString(), cmd, change });
}
