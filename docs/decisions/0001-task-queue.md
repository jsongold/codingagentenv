# ADR-0001: TaskQueue は JSONL + node 製 CLI (`bin/tq`)

- 日付：2026-09-20
- 状態：置き換え（→ ADR-0002）。「TaskQueue」は Claude Code 内蔵の Task list のことで、自作は誤解だった。`bin/tq` は削除済み

## 文脈
全タスクを TaskQueue 経由で Subagent に実行させる。Subagent は並列に動くため、claim の二重取りと書き込み競合を防ぐ必要がある。ハーネスは最小限・依存ゼロに保ちたい。macOS には `flock` が無い。

## 決定
`queue/tasks.jsonl`（1行1タスク）を `bin/tq`（node 標準ライブラリのみ）で操作する。排他は `mkdir` の原子性、書き込みは tmp + rename。状態は `queued → running → done|failed`、`requeue` で `queued` に戻す。`queue/tasks.jsonl` は実行時状態なので git 管理しない。

## 検討して却下した案
| 案 | 却下理由 |
|---|---|
| Markdown チェックボックス (TASKS.md) | 並列 claim で競合する。機械可読性が低い |
| ディレクトリ状態機械 (todo/running/done) | mv は原子的だが ID 採番と一覧が煩雑。ファイル数が増える |
| SQLite | 依存が増える。最小ハーネスの方針に反する |
| tasks.jsonl を git 管理 | ブランチ間で常に衝突し、小さな PR の妨げになる |

## 影響
- 良い影響：依存ゼロ、並列安全（テストで検証）、`jq` で加工可能
- 受け入れたトレードオフ：タスク履歴はマシンローカル。クラッシュ時は `.lock` の手動削除が必要
- 再検討する条件：複数マシンでキューを共有したくなったら
