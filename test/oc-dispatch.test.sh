#!/bin/bash
# Tests for skills/dispatch/oc.ts against a real `opencode serve` and a local OpenAI-compatible stub model
# (no API key, no model traffic): oc.ts starts serve when none answers, a simple Task -> done with text,
# a Task that calls the task tool -> done only after the child session finished, --timeout 1 -> timeout
# and the session aborted, a model error -> error, bad input -> exit 2.
# Needs opencode (OPENCODE_BIN or on PATH; skips otherwise) and npm registry access on the first run
# (opencode installs @ai-sdk/openai-compatible into its cache).
# Run: bash test/oc-dispatch.test.sh
set -u

ROOT=$(cd "$(dirname "$0")/.." && pwd)
OC=$ROOT/skills/dispatch/oc.ts
OPENCODE_BIN=${OPENCODE_BIN:-$(command -v opencode || true)}
[ -n "$OPENCODE_BIN" ] || { echo "skip: opencode is not installed (set OPENCODE_BIN)"; exit 0; }
export OPENCODE_BIN

TMP=$(mktemp -d)
FAILED=0
check() { # name, expected, actual
  if [ "$2" = "$3" ]; then echo "ok   - $1"; else echo "FAIL - $1 (expected '$2', got '$3')"; FAILED=1; fi
}
field() { python3 -c "import json,sys; print(json.loads(sys.argv[1]).get('$1',''))" "$2"; }
free_port() { python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])'; }

# Stub model. The Task text picks the behaviour: USE_TASK -> call the task tool (its prompt says SUBTASK),
# SUBTASK -> answer after 2s, SLOW -> answer after 30s, FAIL -> HTTP 400, otherwise PONG.
# After a tool result it answers PARENT-DONE. Requests without tools (titles) get a plain answer.
cat >"$TMP/stub.mjs" <<'JS'
import http from "node:http";
const text = (c) => (typeof c === "string" ? c : (c ?? []).map((p) => p.text ?? "").join(" "));
const srv = http.createServer((req, res) => {
  let body = "";
  req.on("data", (c) => (body += c));
  req.on("end", async () => {
    const j = JSON.parse(body || "{}");
    const msgs = j.messages ?? [];
    const users = msgs.filter((m) => m.role === "user").map((m) => text(m.content)).join(" ");
    const last = msgs[msgs.length - 1] ?? {};
    const base = { id: "c1", object: "chat.completion.chunk", created: 0, model: j.model };
    const send = (o) => res.write(`data: ${JSON.stringify({ ...base, ...o })}\n\n`);
    const answer = (s) => {
      res.writeHead(200, { "content-type": "text/event-stream" });
      send({ choices: [{ index: 0, delta: { role: "assistant", content: s }, finish_reason: null }] });
      send({ choices: [{ index: 0, delta: {}, finish_reason: "stop" }], usage: { prompt_tokens: 1, completion_tokens: 1, total_tokens: 2 } });
      res.end("data: [DONE]\n\n");
    };
    const wait = (ms) => new Promise((r) => setTimeout(r, ms));
    if (!j.tools?.length) return answer("title");
    if (last.role === "tool") return answer("PARENT-DONE");
    if (users.includes("SUBTASK")) { await wait(2000); return answer("CHILD-DONE"); }
    if (users.includes("USE_TASK")) {
      res.writeHead(200, { "content-type": "text/event-stream" });
      const args = JSON.stringify({ description: "sub", prompt: "SUBTASK finish", subagent_type: "general" });
      send({ choices: [{ index: 0, delta: { role: "assistant", tool_calls: [{ index: 0, id: "call_1", type: "function", function: { name: "task", arguments: args } }] }, finish_reason: null }] });
      send({ choices: [{ index: 0, delta: {}, finish_reason: "tool_calls" }], usage: { prompt_tokens: 1, completion_tokens: 1, total_tokens: 2 } });
      return res.end("data: [DONE]\n\n");
    }
    if (users.includes("SLOW")) { await wait(30000); return answer("late"); }
    if (users.includes("FAIL")) { res.writeHead(400, { "content-type": "application/json" }); return res.end(JSON.stringify({ error: { message: "stub refused", type: "invalid_request_error" } })); }
    return answer("PONG");
  });
});
srv.listen(0, "127.0.0.1", () => console.log(srv.address().port));
JS
node "$TMP/stub.mjs" >"$TMP/stub.port" 2>"$TMP/stub.err" &
STUB_PID=$!
for _ in $(seq 50); do [ -s "$TMP/stub.port" ] && break; sleep 0.1; done
STUB_PORT=$(cat "$TMP/stub.port")

WORK=$TMP/work
mkdir -p "$WORK" "$TMP/xdg"
cat >"$WORK/opencode.json" <<JSON
{ "\$schema": "https://opencode.ai/config.json", "model": "stub/m", "small_model": "stub/m", "autoupdate": false, "share": "disabled",
  "provider": { "stub": { "npm": "@ai-sdk/openai-compatible", "name": "stub",
    "options": { "baseURL": "http://127.0.0.1:$STUB_PORT/v1", "apiKey": "x" }, "models": { "m": { "name": "m" } } } } }
JSON
# Keep the user's opencode config and sessions out of the test; the package cache stays shared.
export XDG_CONFIG_HOME=$TMP/xdg/config XDG_DATA_HOME=$TMP/xdg/data XDG_STATE_HOME=$TMP/xdg/state
PORT=$(free_port)
export OC_URL=http://127.0.0.1:$PORT OC_POLL_MS=300
cleanup() {
  kill "$STUB_PID" 2>/dev/null
  # serve outlives oc.ts by design; a busy serve can ignore TERM, so escalate before removing its files.
  local pids; pids=$(pgrep -f "serve --hostname 127.0.0.1 --port $PORT")
  if [ -n "$pids" ]; then
    kill $pids 2>/dev/null
    for _ in $(seq 50); do kill -0 $pids 2>/dev/null || break; sleep 0.1; done
    kill -9 $pids 2>/dev/null
  fi
  rm -rf "$TMP"
}
trap cleanup EXIT
api() { curl -s "$OC_URL$1$( [[ $1 == *\?* ]] && echo '&' || echo '?')directory=$WORK"; }
oc() { echo "$1" | node "$OC" --dir "$WORK" "${@:2}" 2>"$TMP/oc.err"; }

out=$(oc "say PONG" --model stub/m --timeout 120); code=$?
check "starts serve and returns done" "done" "$(field status "$out")"
check "done text is the reply" "PONG" "$(field text "$out")"
check "done exits 0" "0" "$code"
check "serve keeps running for the next Task" "200" "$(curl -s -o /dev/null -w '%{http_code}' "$OC_URL/doc")"

out=$(oc "USE_TASK please" --timeout 120); code=$?
sid=$(field sessionID "$out")
check "subtask: done" "done" "$(field status "$out")"
check "subtask: text is the parent's final reply" "PARENT-DONE" "$(field text "$out")"
children=$(api "/session/$sid/children")
check "subtask: a child session ran" "1" "$(python3 -c 'import json,sys; print(len(json.loads(sys.argv[1])))' "$children")"
child=$(python3 -c 'import json,sys; print(json.loads(sys.argv[1])[0]["id"])' "$children")
check "subtask: the child finished before done" "CHILD-DONE" "$(api "/session/$child/message" | python3 -c '
import json,sys
m=[m for m in json.load(sys.stdin) if m["info"]["role"]=="assistant"][-1]
print(m["parts"] and "".join(p.get("text","") for p in m["parts"] if p["type"]=="text") if m["info"].get("time",{}).get("completed") else "running")')"

out=$(oc "SLOW one" --timeout 1); code=$?
sid=$(field sessionID "$out")
check "timeout: status" "timeout" "$(field status "$out")"
check "timeout: exit 1" "1" "$code"
sleep 1
check "timeout: session aborted (not busy)" "no" "$(api /session/status | python3 -c "import json,sys; print('yes' if '$sid' in json.load(sys.stdin) else 'no')")"

out=$(oc "FAIL now" --timeout 60); code=$?
check "model error: status" "error" "$(field status "$out")"
check "model error: exit 1" "1" "$code"

check "missing --dir: exit 2" "2" "$(echo x | node "$OC" >/dev/null 2>&1; echo $?)"
check "empty Task: exit 2" "2" "$(echo "" | node "$OC" --dir "$WORK" >/dev/null 2>&1; echo $?)"
check "bad --model: exit 2" "2" "$(echo x | node "$OC" --dir "$WORK" --model nomodel >/dev/null 2>&1; echo $?)"

[ "$FAILED" = 0 ] && echo "PASS: oc-dispatch" || { cat "$TMP/oc.err"; exit 1; }
