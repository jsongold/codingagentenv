# ADR-0016: 内蔵 Task list をやめ、Task は GitHub Issue で管理する

- 日付：2026-09-29
- 状態：採用（ADR-0002、0004、0006 を置き換え）

## 文脈
- これまでは内蔵 Task list（`TaskCreate` / `TaskList` / `TaskUpdate` / `TaskGet`）をタスク管理に使い、`VERIFIED:` 行の無い completed を TaskCompleted hook で拒否し、その hook にトークン消費の記録を載せていた。
- Task list は `~/.claude/tasks/` にあるマシンローカルの状態で、`CLAUDE_CODE_ENABLE_TODO_TOOLS` と project ごとの `CLAUDE_CODE_TASK_LIST_ID` を要する。マシンをまたげず、cloud worker や sleep 中の CCO（ADR-0014、0015）からは見えない。
- PR #90 で、Task は `/taskman` が作り、Issue への登録・worktree・wave の進行は main loop（Orchestrator）が行い、`/dispatch` は Agent（Subagent）を呼ぶだけになった。Task の入口はすでに Issue（Issue #10）。
- ユーザーはローカル依存を減らすため、進捗を GitHub Issues で追うと決めた。

## 決定
- Task は GitHub Issue で管理する。main loop が `/taskman` の Task を Issue に登録してから `/dispatch` に渡す。Subagent が実装・検証・コミットし、完了の判断は Orchestrator（main）がする。
- 次を廃止する：Task tools の使用、`VERIFIED:` 行の要件、TaskCompleted hook（`hooks/harness-task-completed.sh`）、`CLAUDE_CODE_ENABLE_TODO_TOOLS`、`CLAUDE_CODE_TASK_LIST_ID`。
- トークン消費の記録も TaskCompleted hook と一緒に廃止し、`.claude/token-usage.jsonl` を削除する。
- /clear をまたぐ状態は handoff（`.claude/handoff/<name>.md`、ADR-0007）と Issue に残す。handoff には担当する Issue 番号を書く。
- SessionStart hook（handoff と ADR 一覧を文脈に入れる、ADR-0007）は残す。
- `bin/codingenv install` は、旧版が `~/.claude/settings.json` に入れた TaskCompleted hook と `CLAUDE_CODE_ENABLE_TODO_TOOLS` を取り除く。

## 検討して却下した案
| 案 | 却下理由 |
|---|---|
| Task list と Issue を併用する | 状態が2か所に分かれ二重管理になる。Task list はマシンローカルで cloud から見えない |
| TaskCompleted hook をトークン記録専用に残す | Task list を使わないと発火しない。記録のキーの task_id も無くなる |
| トークン記録を SubagentStop / Stop hook に移す | この PR の範囲を超える。必要になったら別 ADR で決める |
| `VERIFIED:` 行を Issue のコメントで強制する | hook で強制できる場所が無い。完了の判断は Orchestrator がする |

## 影響
- 良い影響：ローカル状態（`~/.claude/tasks/`）と env 設定が不要になる。Task の状態がマシンや cloud worker をまたいで見える
- 受け入れたトレードオフ：未検証の完了を hook で機械的に拒否できなくなる。完了判断は Orchestrator の規律に頼る。task 単位のトークン消費の記録が無くなる
- 再検討する条件：未検証のまま閉じた Issue が実際に問題を起こしたとき。トークン消費を分析する必要が再び出たとき
