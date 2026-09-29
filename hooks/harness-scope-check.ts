// PreToolUse (Bash) hook: `gh issue create` / `gh pr create` must carry a
// 「追加・変更するもの」 list in the body, i.e. at least one line like
//   - 追加: skill `/foo`
// Body source: the --body-file/-F file when given, else the whole command
// string (covers --body/-b and heredoc bodies; a list line may also start
// right after the opening quote of --body). Exit 2 blocks the tool call
// with the stderr line; anything else (other commands, unparseable stdin) exits 0.
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const TARGET = /\bgh\s+(issue|pr)\s+create\b/;
const BODY_FILE = /(?:^|\s)(?:--body-file|-F)(?:=|\s+)(?:"([^"]*)"|'([^']*)'|(\S+))/;
const SCOPE_LINE = /(?:^|["'])\s*-\s*(追加|変更|削除)[:：]\s*\S+\s+`[^`]+`/m;
const MESSAGE = "本文に「追加・変更するもの」（例: `- 追加: skill /foo`）を書いてから作る";

function body(command: string, cwd: string): string {
  const m = command.match(BODY_FILE);
  if (!m) return command;
  const file = m[1] ?? m[2] ?? m[3];
  if (file === "-") return ""; // stdin is not available to the hook
  try {
    return readFileSync(resolve(cwd, file), "utf8");
  } catch {
    return "";
  }
}

let input: { tool_input?: { command?: unknown }; cwd?: unknown };
try {
  input = JSON.parse(readFileSync(0, "utf8"));
} catch {
  process.exit(0);
}
const command = input?.tool_input?.command;
if (typeof command !== "string" || !TARGET.test(command)) process.exit(0);
const cwd = typeof input.cwd === "string" ? input.cwd : process.cwd();
if (SCOPE_LINE.test(body(command, cwd))) process.exit(0);
process.stderr.write(MESSAGE + "\n");
process.exit(2);
