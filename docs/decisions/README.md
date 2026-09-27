# ADR 一覧

| 番号 | タイトル | 状態 | 日付 |
|---|---|---|---|
| [0001](./0001-task-queue.md) | TaskQueue は JSONL + node 製 CLI (`bin/tq`) | 置き換え | 2026-09-20 |
| [0002](./0002-builtin-task-list.md) | タスク管理は Claude Code 内蔵の Task list、main セッションが dispatcher | 採用 | 2026-09-20 |
| [0003](./0003-global-harness.md) | ハーネスは global に置き、各 project はデータだけを持つ | 採用 | 2026-09-20 |
| [0004](./0004-enforcement-hooks.md) | 文脈の復元と検証済み完了を hook で強制する | 採用 | 2026-09-20 |
| [0005](./0005-repo-only-deploy-command.md) | 編集は repo 内だけ。global への展開は `bin/codingenv` に集約する | 採用 | 2026-09-20 |
| [0006](./0006-token-usage-log.md) | task 完了時にトークン消費を hook で集計・記録する | 採用 | 2026-09-20 |
| [0007](./0007-multi-session-handoff.md) | /handoff の保存先を `.claude/handoff/<name>.md`（1セッション1ファイル）にする | 採用 | 2026-09-21 |
| [0008](./0008-orchestrator-worker-cad.md) | Orchestrator（ローカル）と Worker（任意のクラウド）を分け、メタデータは Go 製の `cad` が決定的に返す | 採用 | 2026-09-27 |
| [0009](./0009-task-spec-orchestration.md) | Orchestrator と Worker の契約は task 1件ごとの YAML の task spec。provider は Orchestrator が strategy 付きで決め、`dev-dispatch` が決定的に適用する | 採用 | 2026-09-27 |
| [0010](./0010-placement.md) | 配置は (Agent, Computer) の組を選ぶ。判断は Orchestrator の class 分類だけ、選択は `cad` の `POST /v1/place` が決定的に返す | 提案 | 2026-09-27 |

新しい ADR を追加するには、0000-template.md をコピーして次の番号のファイルを作り、上の表に1行追加する。

状態が「置き換え」の ADR と「却下した案」は再提案しない。
