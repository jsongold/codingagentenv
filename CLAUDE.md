# codingagentenv

<!-- 目安：全体で150行以内。コードから推測できないことだけ書く。 -->

## 業務コンテキスト（毎回必要な要点だけ）
- 目的：<このプロダクトが解決する業務課題を1行で>
- 利用者：<誰が・どんな場面で使うか>
- 成功の定義：<何ができれば価値があるか>
- 絶対に守る業務ルール：
  - <例：金額は税込・円単位で切り捨て>
  - <例：顧客データを外部APIに送らない>
- 今の優先順位：<例：正確性 > 速度 > 見た目>
- 詳細 → docs/context/business.md（業務フロー）、docs/context/glossary.md（用語）

## 作業の始め方（/clear後も必ず）
1. PROGRESS.md を読む（進行中タスクの状態）
2. 関係する docs/decisions/ を確認する（却下済みの案を再提案しない）
3. 着手前に「目的・完了条件・次の一手」を3行で復唱し、ずれがあれば質問する

## コマンド
- テスト：`node --test test/tq.test.js`（`node --test test/` は Node v26 で失敗する。ファイルを指定すること）
- lint / 型チェック：なし（未導入）
- キュー操作：`bin/tq <enqueue|claim|done|fail|requeue|show|list>`
  - `bin/tq enqueue <title> [--agent <type>] [--body <text>]`
  - `bin/tq claim [--id <id>]`（キューが空なら exit 2）
  - `bin/tq done|fail|requeue <id> [--note <text>]`
  - `bin/tq list [--state queued|running|done|failed]`

## 規約・注意点（デフォルトと違うものだけ）
- 全タスクは TaskQueue（`bin/tq`）経由で Subagent に実行させる（ADR-0001）
- `bin/tq` は node 標準ライブラリのみ。依存を追加しない
- `queue/tasks.jsonl` は実行時状態。git 管理しない・手で編集しない
- `lock timeout` が出たら、tq プロセスが動いていないことを確認してから `queue/tasks.jsonl.lock` を削除する

## コンテキスト圧縮時の指示
- 圧縮（compact）時は、変更したファイル一覧、テストコマンド、PROGRESS.md の「決定事項」を必ず残すこと。
