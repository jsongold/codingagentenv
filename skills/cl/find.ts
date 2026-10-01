// Locate CLs across the repo's sessions: scan, show, where.
import { existsSync, readdirSync, statSync } from "node:fs";
import { basename, dirname, join } from "node:path";
import { fail, load } from "./model.ts";
import type { Checklist } from "./model.ts";
import { render } from "./render.ts";

export type Found = { path: string; cl: Checklist; mtime: number; own: boolean };

// All CLs in the repo: <scratchpad>/../../*/scratchpad/cl/*.json, newest first.
export function scan(scratchpad: string): Found[] {
  const repoDir = dirname(dirname(scratchpad));
  const own = join(scratchpad, "cl");
  const found: Found[] = [];
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

// Own scratchpad first, then the whole repo (newest wins; mention the others).
export function show(scratchpad: string, name: string): string {
  const own = join(scratchpad, "cl", `${name}.json`);
  const others = (): number => scan(scratchpad).filter((r) => basename(r.path) === `${name}.json` && r.path !== own).length;
  const mk = (n: number) => (n > 0 ? `\n\nNote: ${n} other checklist(s) named ${name} exist in this repo.` : "");
  if (existsSync(own)) return render(load(own)) + mk(others());
  const hit = scan(scratchpad).find((r) => basename(r.path) === `${name}.json`);
  if (!hit) fail(`not found: ${name}`);
  return render(hit.cl) + mk(others() - 1);
}

// Same lookup as show: print the local file path, and the store URL when set.
export function where(scratchpad: string, name: string): string {
  const own = join(scratchpad, "cl", `${name}.json`);
  const hit = existsSync(own) ? { path: own, cl: load(own) } : scan(scratchpad).find((r) => basename(r.path) === `${name}.json`);
  if (!hit) fail(`not found: ${name}`);
  return [`local: ${hit.path}`, ...(hit.cl.store ? [`store: ${hit.cl.store.url}`] : [])].join("\n");
}
