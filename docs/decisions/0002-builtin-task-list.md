# ADR-0002: タスク管理は Claude Code 内蔵の Task list、main セッションが dispatcher

- 日付：2026-09-20
- 状態：採用（ADR-0001 を置き換え）

## 文脈
「全タスクを task queue 経由で Subagent に実行させる」の task queue は、Claude Code 内蔵の Task list（`TaskCreate` / `TaskGet` / `TaskList` / `TaskUpdate`）を指していた。ADR-0001 はこれを自作物と誤解して `bin/tq` を作った。

公式 docs（code.claude.com/docs：tools-reference, interactive-mode, sub-agents, agent-teams）と実機で確認した事実：
- Task tools が既定で有効なのは Claude 3.x、Opus 4〜4.7、Sonnet 4〜4.6、Haiku 4.5 のみ。それ以外は `CLAUDE_CODE_ENABLE_TODO_TOOLS=1` で opt-in。settings.json の `env` に書けば再起動なしで有効になった。
- `CLAUDE_CODE_TASK_LIST_ID=<name>` で `~/.claude/tasks/<name>/` をセッション間で共有できる。1タスク1 JSON、`.lock` あり。
- 状態は pending / in_progress / completed。依存（blockedBy）、owner、metadata を持つ。failed は無い。
- background Subagent（既定）は Task tools を持たない。持つのは agent teams の teammate だけ。

## 決定
タスクは内蔵 Task list で管理する。main セッションが dispatcher になり、`TaskCreate` → Subagent 起動 → main が完了条件を検証 → `TaskUpdate` の順で回す。手順は `skills/dispatch`。設定は `.claude/settings.json` の `env`。`bin/tq`・テスト・`queue/` は削除した。

## 検討して却下した案
| 案 | 却下理由 |
|---|---|
| `bin/tq` を残す（ADR-0001） | 内蔵機能の再発明。UI（Ctrl+T）も依存関係も無く、保守対象が増えるだけ |
| agent teams で teammate に self-claim させる | 実験的機能。teammate は Subagent ではなく別セッションでトークン消費が大きい。`/resume` で復元されない |
| Subagent 自身に Task list を更新させる | background Subagent は Task tools を持たない。自己申告の completed は検証を素通りする |

## 影響
- 良い影響：自作コードゼロ。claim を main だけが行うので二重取りが構造的に起きない。検証が必ず main を通る
- 受け入れたトレードオフ：Task list は `~/.claude/tasks/` にありマシンローカル、git 管理外。failed 状態が無いので失敗理由は説明欄か metadata に書く。main の文脈を dispatch が消費する
- 再検討する条件：並列度を上げたくて main が詰まったら agent teams を再評価する。agent teams が実験的でなくなったとき
