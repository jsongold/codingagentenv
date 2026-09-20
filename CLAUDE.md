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
`/pickup` が以下を実行する。
1. PROGRESS.md を読む（進行中タスクの状態）。`TaskList` で残タスクを確認する
2. 関係する docs/decisions/ を確認する（却下済みの案を再提案しない）
3. 着手前に「目的・完了条件・次の一手」を3行で復唱し、ずれがあれば質問する

## コマンド
- テスト / lint / 起動：なし（このリポジトリはハーネス。実行コードを持たない）
- `/dispatch <やりたいこと>`：Task list に分解して Subagent に実行させる
- `/handoff`：/clear 前に PROGRESS.md を書き出してコミット
- `/pickup`：/clear 後に文脈を復元して復唱（built-in の `/resume` とは別物）
- タスク一覧：`Ctrl+T`。実体は `~/.claude/tasks/codingagentenv/`

## 規約・注意点（デフォルトと違うものだけ）
- 全タスクは Claude Code 内蔵の Task list（`TaskCreate` 系）経由で Subagent に実行させる（ADR-0002）。main セッションは dispatcher で、自分では実装しない
- 「task queue」= 内蔵 Task list。キューを自作しない（ADR-0001 はその誤解で置き換え済み）
- Task tools が見えないときは `.claude/settings.json` の `env` を確認する。代替手段を作らない
- Subagent の報告は main が完了条件のコマンドで検証してから completed にする。1タスク = 1コミット
- `~/.claude/tasks/` 配下を手で編集しない

## コンテキスト圧縮時の指示
- 圧縮（compact）時は、変更したファイル一覧、テストコマンド、PROGRESS.md の「決定事項」を必ず残すこと。
