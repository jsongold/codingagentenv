// oc.ts --dir <worktree> [--model provider/model] [--timeout sec] < task
// Sends one Task (stdin) to opencode serve (HTTP API), waits until it and its subtasks finish,
// prints {sessionID, status: "done"|"error"|"timeout", text} on stdout. Exit 0 only for done.
// Starts `opencode serve` on 127.0.0.1 when nothing answers at OC_URL. Invalid input -> stderr, exit 2.
// Env: OC_URL (default http://127.0.0.1:4096), OPENCODE_BIN (default opencode),
//      OC_POLL_MS (default 1000), OPENCODE_SERVER_PASSWORD / OPENCODE_SERVER_USERNAME (basic auth).
import { spawn } from "node:child_process";
import { openSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

type Status = { type: "idle" } | { type: "busy" } | { type: "retry"; attempt: number; message: string };
type Part = { type: string; text?: string };
type Message = { info: { role: string; error?: { name?: string; data?: { message?: string } }; time?: { completed?: number } }; parts: Part[] };
type Result = { sessionID: string; status: "done" | "error" | "timeout"; text: string };

// Pre-allow every tool so the session never stops on a permission prompt. Unverified against
// opencode beyond the session API accepting the ruleset (see the PR for what was checked).
const PERMISSION = [{ permission: "*", pattern: "*", action: "allow" }];
// The question tool waits for a human; nobody answers inside a dispatched Task.
const TOOLS = { question: false };
const RETRY_LIMIT = 3;

const BASE = (process.env.OC_URL ?? "http://127.0.0.1:4096").replace(/\/$/, "");
const POLL_MS = Number(process.env.OC_POLL_MS ?? 1000);

function usage(msg: string): never {
  process.stderr.write(`oc.ts: ${msg}\nusage: oc.ts --dir <worktree> [--model provider/model] [--timeout sec] < task\n`);
  process.exit(2);
}

function flag(args: string[], name: string): string | undefined {
  const i = args.indexOf(name);
  if (i < 0) return undefined;
  const v = args[i + 1];
  if (v === undefined || v.startsWith("--")) usage(`${name} needs a value`);
  return v;
}

function headers(): Record<string, string> {
  const h: Record<string, string> = { "content-type": "application/json" };
  const pass = process.env.OPENCODE_SERVER_PASSWORD;
  if (pass) h.authorization = `Basic ${Buffer.from(`${process.env.OPENCODE_SERVER_USERNAME ?? "opencode"}:${pass}`).toString("base64")}`;
  return h;
}

async function api<T>(method: string, path: string, dir: string, body?: unknown): Promise<T> {
  const sep = path.includes("?") ? "&" : "?";
  const res = await fetch(`${BASE}${path}${sep}directory=${encodeURIComponent(dir)}`, {
    method,
    headers: headers(),
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!res.ok) throw new Error(`${method} ${path}: HTTP ${res.status} ${await res.text()}`);
  return (res.status === 204 ? undefined : await res.json()) as T;
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

async function alive(): Promise<boolean> {
  try {
    return (await fetch(`${BASE}/doc`, { headers: headers(), signal: AbortSignal.timeout(2000) })).ok;
  } catch {
    return false;
  }
}

// Starts serve detached (it outlives this process and serves later Tasks) and waits until it answers.
async function ensureServe(): Promise<void> {
  if (await alive()) return;
  const url = new URL(BASE);
  if (!["127.0.0.1", "localhost"].includes(url.hostname)) throw new Error(`no opencode serve at ${BASE}`);
  const log = openSync(join(tmpdir(), `opencode-serve-${url.port || 80}.log`), "a");
  const child = spawn(process.env.OPENCODE_BIN ?? "opencode", ["serve", "--hostname", "127.0.0.1", "--port", url.port || "80"], {
    detached: true,
    stdio: ["ignore", log, log],
  });
  let failed: Error | undefined;
  child.on("error", (e) => (failed = e));
  child.unref();
  for (let i = 0; i < 60; i++) {
    if (failed) throw new Error(`cannot start opencode serve: ${failed.message}`);
    if (await alive()) return;
    await sleep(500);
  }
  throw new Error(`opencode serve did not answer at ${BASE} within 30s`);
}

// A session is running while the status map lists it as busy or retrying; absent or idle means idle.
async function running(id: string, status: Record<string, Status>, dir: string): Promise<boolean> {
  if (status[id] && status[id].type !== "idle") return true;
  const children = await api<{ id: string }[]>("GET", `/session/${id}/children`, dir);
  for (const c of children) if (await running(c.id, status, dir)) return true;
  return false;
}

function lastAssistant(messages: Message[]): Message | undefined {
  return [...messages].reverse().find((m) => m.info.role === "assistant");
}

const textOf = (m: Message | undefined) =>
  (m?.parts ?? []).filter((p) => p.type === "text" && p.text).map((p) => p.text).join("\n").trim();

async function run(dir: string, task: string, model: string | undefined, timeoutSec: number): Promise<Result> {
  await ensureServe();
  const session = await api<{ id: string }>("POST", "/session", dir, { title: task.split("\n")[0].slice(0, 80), permission: PERMISSION });
  const body: Record<string, unknown> = { parts: [{ type: "text", text: task }], tools: TOOLS };
  if (model) {
    const i = model.indexOf("/");
    body.model = { providerID: model.slice(0, i), modelID: model.slice(i + 1) };
  }
  await api("POST", `/session/${session.id}/prompt_async`, dir, body);

  const deadline = Date.now() + timeoutSec * 1000;
  let seenBusy = false;
  while (Date.now() < deadline) {
    await sleep(POLL_MS);
    const status = await api<Record<string, Status>>("GET", "/session/status", dir);
    const own = status[session.id];
    if (own?.type === "retry" && own.attempt >= RETRY_LIMIT) {
      await api("POST", `/session/${session.id}/abort`, dir);
      return { sessionID: session.id, status: "error", text: `retry ${own.attempt}: ${own.message}` };
    }
    if (own && own.type !== "idle") {
      seenBusy = true;
      continue;
    }
    // Right after prompt_async the status map is still empty: idle only counts once the session has
    // been seen busy, or once a completed assistant reply shows it ran between two polls.
    const messages = await api<Message[]>("GET", `/session/${session.id}/message`, dir);
    const last = lastAssistant(messages);
    if (!seenBusy && !last?.info.time?.completed) continue;
    // A subtask started in the background can still run after its parent went idle.
    if (await running(session.id, status, dir)) continue;
    if (last?.info.error) {
      return { sessionID: session.id, status: "error", text: last.info.error.data?.message ?? last.info.error.name ?? "error" };
    }
    const text = textOf(last);
    if (text) return { sessionID: session.id, status: "done", text };
    if (last?.info.time?.completed) return { sessionID: session.id, status: "error", text: "assistant finished without text" };
  }
  await api("POST", `/session/${session.id}/abort`, dir);
  const messages = await api<Message[]>("GET", `/session/${session.id}/message`, dir);
  return { sessionID: session.id, status: "timeout", text: textOf(lastAssistant(messages)) };
}

async function main(): Promise<void> {
  const args = process.argv.slice(2);
  const dir = flag(args, "--dir");
  if (!dir) usage("--dir is required");
  const model = flag(args, "--model");
  if (model !== undefined && !/^[^/]+\/.+/.test(model)) usage("--model must be provider/model");
  const timeout = Number(flag(args, "--timeout") ?? 1800);
  if (!Number.isFinite(timeout) || timeout <= 0) usage("--timeout must be a positive number of seconds");
  const chunks: Buffer[] = [];
  for await (const c of process.stdin) chunks.push(c as Buffer);
  const task = Buffer.concat(chunks).toString("utf8").trim();
  if (!task) usage("the Task text goes on stdin");

  let result: Result;
  try {
    result = await run(resolve(dir), task, model, timeout);
  } catch (e) {
    result = { sessionID: "", status: "error", text: (e as Error).message };
  }
  process.stdout.write(`${JSON.stringify(result)}\n`);
  process.exit(result.status === "done" ? 0 : 1);
}

main();
