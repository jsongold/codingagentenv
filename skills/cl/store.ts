// GitHub/Jira store: URL parsing, gh calls, sync payloads.
import { execFileSync } from "node:child_process";
import { fail } from "./model.ts";
import type { Checklist, LogEntry, Store } from "./model.ts";
import { render } from "./render.ts";

export function parseStore(url: string | undefined): Store {
  const gh = /^https:\/\/github\.com\/[\w.-]+\/[\w.-]+\/issues\/\d+$/;
  const jira = /^https:\/\/[\w.-]+\/browse\/[A-Z][A-Z0-9_]*-\d+$/;
  if (url && gh.test(url)) return { kind: "github", url };
  if (url && jira.test(url)) return { kind: "jira", url };
  return fail(`invalid store url: ${url ?? "(missing)"} (expected https://github.com/<o>/<r>/issues/<n> or https://<host>/browse/<KEY>-<n>)`);
}

function gh(args: string[]): { id?: number } {
  try {
    return JSON.parse(execFileSync("gh", args, { encoding: "utf8" }) || "{}");
  } catch (e) {
    return fail(`gh failed: ${(e as Error).message}`);
  }
}

export const pending = (cl: Checklist) => cl.log.slice(cl.synced);
export const managedBody = (cl: Checklist) => `<!-- cl:${cl.name} -->\n${render(cl)}`;
const entryBody = (cl: Checklist, e: LogEntry) => `cl ${cl.name} ${e.at} ${e.cmd}: ${e.change}`;

// github: managed comment (create or edit) + one comment per pending entry. Nothing is marked synced on failure.
export function syncGithub(cl: Checklist, store: Store): void {
  const m = /^https:\/\/github\.com\/([^/]+\/[^/]+)\/issues\/(\d+)$/.exec(store.url)!;
  const base = `repos/${m[1]}/issues`;
  if (store.commentId) gh(["api", "-X", "PATCH", `${base}/comments/${store.commentId}`, "-f", `body=${managedBody(cl)}`]);
  else store.commentId = String(gh(["api", "-X", "POST", `${base}/${m[2]}/comments`, "-f", `body=${managedBody(cl)}`]).id);
  for (const e of pending(cl)) gh(["api", "-X", "POST", `${base}/${m[2]}/comments`, "-f", `body=${entryBody(cl, e)}`]);
  cl.synced = cl.log.length;
}
