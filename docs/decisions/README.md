# ADR 一覧

| 番号 | タイトル | 状態 | 日付 |
|---|---|---|---|
| [0001](./0001-task-queue.md) | TaskQueue は JSONL + node 製 CLI (`bin/tq`) | 置き換え | 2026-09-20 |
| [0002](./0002-builtin-task-list.md) | タスク管理は Claude Code 内蔵の Task list、main セッションが dispatcher | 採用 | 2026-09-20 |
| [0003](./0003-global-harness.md) | ハーネスは global に置き、各 project はデータだけを持つ | 採用 | 2026-09-20 |
| [0004](./0004-enforcement-hooks.md) | 文脈の復元と検証済み完了を hook で強制する | 採用 | 2026-09-20 |
| [0005](./0005-repo-only-deploy-command.md) | 編集は repo 内だけ。global への展開は `bin/codingenv` に集約する | 採用 | 2026-09-20 |
| [0006](./0006-token-usage-log.md) | task 完了時にトークン消費を hook で集計・記録する | 採用 | 2026-09-20 |

新しい ADR を追加するには、0000-template.md をコピーして次の番号のファイルを作り、上の表に1行追加する。

状態が「置き換え」の ADR と「却下した案」は再提案しない。
