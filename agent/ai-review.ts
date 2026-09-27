// ai-review <pr> <worktree> [--implementer <name>] — get one independent AI review of a PR.
// Tries policy review.reviewers in order, skipping the implementer and reviewers cad reports as
// exhausted. A backend that hits its quota is reported to cad and the next reviewer is tried.
// Prints "<reviewer> reviewed #<pr>: <comment URL or file>"; exits 1 with a one-line reason if none succeeds.
// Env: CAD_URL (http://127.0.0.1:7878), CAD_NS (default), CAD_TOKEN, CAD_POLICY, AI_REVIEW_IMPLEMENTER.
import { spawnSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { delimiter, dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

type Result = { ok: true; where: string } | { ok: false; quota: boolean; reason: string };

const usage = "usage: ai-review <pr> <worktree> [--implementer <name>]";
const fail = (msg: string): never => {
  console.error(`ai-review: ${msg}`);
  process.exit(1);
};

const argv = process.argv.slice(2);
let implementer = process.env.AI_REVIEW_IMPLEMENTER ?? "";
const i = argv.indexOf("--implementer");
if (i >= 0) implementer = argv.splice(i, 2)[1] ?? fail(usage);
const [pr, wtArg] = argv;
if (!pr || !wtArg || argv.length !== 2) fail(usage);
const wt = resolve(wtArg);
const repo = resolve(dirname(fileURLToPath(import.meta.url)), "..");
// Backends come from PATH (installed tools); the repo's tools/ is the fallback when not installed.
process.env.PATH = `${process.env.PATH}${delimiter}${join(repo, "tools")}`;

const cadURL = process.env.CAD_URL ?? "http://127.0.0.1:7878";
const cadNS = `ns=${encodeURIComponent(process.env.CAD_NS ?? "default")}`; // cad requires ?ns= on GETs
const cadHeaders: Record<string, string> = process.env.CAD_TOKEN ? { Authorization: `Bearer ${process.env.CAD_TOKEN}` } : {};

// Returns undefined when cad is unreachable or answers non-2xx.
async function cad(path: string, body?: unknown): Promise<any> {
  try {
    const res = await fetch(cadURL + path, {
      method: body === undefined ? "GET" : "POST",
      headers: { ...cadHeaders, "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: AbortSignal.timeout(3000),
    });
    if (!res.ok) return undefined;
    return res.status === 204 ? {} : await res.json();
  } catch {
    return undefined;
  }
}

function run(cmd: string, args: string[], input?: string) {
  const r = spawnSync(cmd, args, { cwd: wt, input, encoding: "utf8", maxBuffer: 64 << 20 });
  const out = (r.stdout ?? "").trim();
  const err = (r.stderr ?? "").trim() || (r.error ? String(r.error.message) : "");
  return { code: r.status ?? 127, out, err, last: (err || out).split("\n").pop() ?? "" };
}

// CAD_POLICY > ./.agent/policy.json > cad /v1/policy > the repo's .agent/policy.json.
async function policy(): Promise<{ reviewers: string[]; excludeImplementer: boolean }> {
  const read = (p: string) => JSON.parse(readFileSync(p, "utf8"));
  let p: any;
  if (process.env.CAD_POLICY) p = read(process.env.CAD_POLICY);
  else if (existsSync(".agent/policy.json")) p = read(".agent/policy.json");
  else p = (await cad(`/v1/policy?${cadNS}`)) ?? read(join(repo, ".agent/policy.json"));
  const reviewers = p?.review?.reviewers;
  if (!Array.isArray(reviewers) || reviewers.length === 0) fail("policy has no review.reviewers");
  return { reviewers, excludeImplementer: p.review.excludeImplementer !== false };
}

// A long review can outlive the head it reviewed; never accept or post a review of an old head.
function ensureHead(head: string, name: string): void {
  const now = prHead();
  if (now !== head) fail(`stale: #${pr} head moved ${head.slice(0, 7)} -> ${now.slice(0, 7)} during ${name} review; rerun ai-review`);
}

function prHead(): string {
  const r = run("gh", ["pr", "view", pr, "--json", "headRefOid", "-q", ".headRefOid"]);
  return r.code === 0 ? r.out : fail(`gh pr view ${pr}: ${r.last}`);
}

const CODEX_BOT = "chatgpt-codex-connector[bot]";

// The GitHub Codex bot reviews on its own; use its review only if it already covers the PR head.
function codexBot(head: string): Result {
  const jq = `.[] | select(.user.login == "${CODEX_BOT}" and .commit_id == "${head}" and .state != "DISMISSED") | .html_url`;
  const r = run("gh", ["api", "--paginate", `repos/{owner}/{repo}/pulls/${pr}/reviews`, "--jq", jq]);
  if (r.code !== 0) return { ok: false, quota: false, reason: `gh api: ${r.last}` };
  const url = r.out.split("\n").filter(Boolean).pop();
  return url ? { ok: true, where: url } : { ok: false, quota: false, reason: `no bot review on ${head.slice(0, 7)} yet` };
}

// codex-localreview exits 3 when codex reports its usage limit, 4 (nothing posted) when the head moved.
function codexLocal(): Result {
  const r = run("codex-localreview", [pr, wt]);
  if (r.code === 4) fail(`stale: ${r.last}`);
  if (r.code === 0) return { ok: true, where: r.out.match(/https?:\/\/\S+/g)?.pop() ?? r.out };
  return { ok: false, quota: r.code === 3, reason: r.last };
}

const PROMPT = `You are reviewing a pull request as an independent reviewer. Inspect the changes with
\`git diff origin/main...HEAD\` (and read files as needed). Report correctness bugs, security issues and
missing tests, most severe first, each as "[P1|P2|P3] file:line — problem — fix". If there are none, say so.
Do not modify anything.`;

// Headless Claude review on the worktree, read-only tools, posted as a PR comment.
function claudeOpus(head: string): Result {
  const h = run("git", ["rev-parse", "HEAD"]);
  if (h.out !== head) return { ok: false, quota: false, reason: "worktree HEAD != PR head" };
  const f = run("git", ["fetch", "-q", "origin", "main"]); // the prompt diffs against origin/main
  if (f.code !== 0) return { ok: false, quota: false, reason: `git fetch origin main: ${f.last}` };
  const r = run("claude", [
    "-p", "--model", "opus", "--no-session-persistence", "--permission-mode", "dontAsk",
    "--allowedTools", "Read Grep Glob Bash(git diff *) Bash(git log *) Bash(git show *)",
  ], PROMPT); // prompt on stdin: --allowedTools is variadic and would swallow a positional prompt
  // ponytail: Claude's usage-limit output is unverified, so a quota hit here counts as a plain failure.
  if (r.code !== 0 || !r.out) return { ok: false, quota: false, reason: `claude: ${r.last || "empty review"}` };
  ensureHead(head, "claude-opus");
  const dir = join(process.env.XDG_STATE_HOME ?? join(homedir(), ".local/state"), "ai-review");
  mkdirSync(dir, { recursive: true });
  const file = join(dir, `claude-${pr}.md`);
  writeFileSync(file, `**Claude Opus review on ${head.slice(0, 7)}** (headless \`claude -p\`, read-only).\n\n${r.out}\n`);
  const c = run("gh", ["pr", "comment", pr, "--body-file", file]);
  return c.code === 0 ? { ok: true, where: c.out || file } : { ok: false, quota: false, reason: `gh pr comment: ${c.last} (review kept in ${file})` };
}

const { reviewers, excludeImplementer } = await policy();
if (excludeImplementer && !implementer.trim())
  fail("policy review.excludeImplementer is on: pass --implementer <name> or set AI_REVIEW_IMPLEMENTER");
const quotas: any[] = (await cad(`/v1/quota?${cadNS}`)) ?? [];
const exhausted = new Set(quotas.filter((q) => q?.state === "exhausted").map((q) => q.reviewer));
let head = "";
const tried: string[] = [];

for (const name of reviewers) {
  if (excludeImplementer && name === implementer) { console.error(`ai-review: skip ${name}: implementer`); continue; }
  // Looking up an existing bot review costs no quota, so an exhausted codex-bot is still checked.
  if (exhausted.has(name) && name !== "codex-bot") { console.error(`ai-review: skip ${name}: quota exhausted (cad)`); continue; }
  head ||= prHead();
  const r: Result =
    name === "codex-bot" ? codexBot(head)
    : name === "codex-local" ? codexLocal()
    : name === "claude-opus" ? claudeOpus(head)
    : { ok: false, quota: false, reason: "unknown reviewer" };
  if (r.ok) {
    ensureHead(head, name);
    console.log(`${name} reviewed #${pr}: ${r.where}`);
    process.exit(0);
  }
  console.error(`ai-review: ${name} failed: ${r.reason}`);
  tried.push(`${name}${r.quota ? " (quota)" : ""}`);
  if (r.quota) await cad(`/v1/quota/${encodeURIComponent(name)}`, { state: "exhausted" });
}
fail(`no reviewer succeeded for #${pr} (tried: ${tried.join(", ") || "none"})`);
