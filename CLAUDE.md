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
- テスト：`bash test/hooks.test.sh` と `bash test/harness.test.sh`。lint / 起動：なし
- global への展開：`bin/harness install`（ユーザーが実行する）。drift の確認：`bin/harness status`（読み取りのみ。Claude が実行してよい）
- `/dispatch <やりたいこと>`：Task list に分解して Subagent に実行させる
- `/handoff`：/clear 前に PROGRESS.md を書き出してコミット
- `/pickup`：/clear 後に文脈を復元して復唱（built-in の `/resume` とは別物）
- タスク一覧：`Ctrl+T`。実体は `~/.claude/tasks/codingagentenv/`

## 規約・注意点（デフォルトと違うものだけ）
- 編集は repo 内だけ。`~/.claude/` 配下は Claude も人も直接編集しない。global に効かせたい変更は repo の正本（`skills/`、`hooks/`、`global/CLAUDE.harness.md`）を直し、ユーザーに `bin/harness install` を依頼する（ADR-0005）
- dispatcher のルールの正本は `global/CLAUDE.harness.md`。`skills/` と `hooks/` は symlink なので、編集すると全 project の挙動が即座に変わる
- 「task queue」= 内蔵 Task list（ADR-0002）。キューを自作しない（ADR-0001 はその誤解で置き換え済み）
- `hooks/` も global の正本（ADR-0004）。編集は全 project に即座に効くので、変更したら必ず `bash test/hooks.test.sh` を通す
- task を completed にする前に、description に `VERIFIED: <コマンド> -> <結果>` を別の `TaskUpdate` で追記する。無いと hook が拒否する
- Claude Code の仕様は docs の原文で確認する。WebFetch の要約は TaskCompleted の exit 2 について誤答した（ADR-0004）
- Task tools が見えないときは `bin/harness status` で展開漏れを確認する。代替手段を作らない

## コンテキスト圧縮時の指示
- 圧縮（compact）時は、変更したファイル一覧、テストコマンド、PROGRESS.md の「決定事項」を必ず残すこと。
