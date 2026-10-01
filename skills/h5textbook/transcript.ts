// transcript.ts <session-id> [after-uuid]
// このセッションの会話のうち after-uuid（checkpoint）より後の user / assistant の本文を Markdown で stdout に出す。
// after-uuid が無ければセッションの最初から。compact で要約された部分も transcript の原文から拾える。
// 最後の行に、次の checkpoint にする uuid を `checkpoint: <uuid>` で出す。
import { readFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";

type Entry = { type?: string; uuid?: string; message?: { content?: unknown } };

function text(content: unknown): string {
  if (typeof content === "string") return content;
  if (!Array.isArray(content)) return "";
  return content
    .filter((c) => c?.type === "text" && typeof c.text === "string")
    .map((c) => c.text)
    .join("\n");
}

const [sessionId, after] = process.argv.slice(2);
if (!sessionId) {
  console.error("usage: transcript.ts <session-id> [after-uuid]");
  process.exit(1);
}
const project = process.cwd().replace(/[^A-Za-z0-9]/g, "-");
const file = join(homedir(), ".claude", "projects", project, `${sessionId}.jsonl`);

const msgs = readFileSync(file, "utf8")
  .split("\n")
  .filter(Boolean)
  .map((l) => JSON.parse(l) as Entry)
  .filter((e) => e.type === "user" || e.type === "assistant");

const i = after ? msgs.findIndex((e) => e.uuid === after) : -1;
if (after && i < 0) {
  console.error(`checkpoint ${after} not found in ${file}`);
  process.exit(1);
}

for (const e of msgs.slice(i + 1)) {
  const t = text(e.message?.content);
  if (t) console.log(`## ${e.type}\n${t}\n`);
}
console.log(`checkpoint: ${msgs.at(-1)?.uuid ?? after ?? ""}`);
