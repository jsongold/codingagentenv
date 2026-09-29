# codingagentenv

<!-- 目安：全体で150行以内。コードから推測できないことだけ書く。 -->

## 業務コンテキスト（毎回必要な要点だけ）
- 目的：/clear 後も文脈を失わず、複数ステップの作業を GitHub Issue 経由で Subagent に実行させるハーネスの正本を管理し、全 project に効かせる（#92, #103）
- 利用者：repo の持ち主本人。自分の全 project で Claude Code を使う場面（#92。他人との共有は再検討条件）
- 成功の定義：どの project でも `/pickup` → `/dispatch` が global skill で完遂し、/clear 後も handoff と Issue で文脈が残る（handoff の完了条件）
- 絶対に守る業務ルール：
  - `~/.claude/` 配下を直接編集しない。repo の正本を直し、展開はユーザーが `bin/codingenv install` で行う（#93）
  - キューや代替のタスク管理を自作しない。Task は GitHub Issue、完了の判断は Orchestrator（main）がする（#103）
- 今の優先順位：未定（ユーザー確認待ち）
- 詳細 → docs/context/business.md（業務フロー）、docs/context/glossary.md（用語）

## 作業の始め方（/clear後も必ず）
`/pickup` が以下を実行する。
1. .claude/handoff/<name>.md を読む（進行中タスクの状態。handoff が複数あれば選ぶ）。担当 Issue を `gh issue view` で確認する
2. 関係する `doc:spec` Issue（`gh issue list --label doc:spec`）を確認する（置き換え済みの決定と却下済みの案を再提案しない）
3. 着手前に「目的・完了条件・次の一手」を3行で復唱し、ずれがあれば質問する

## コマンド
- テスト：`bash test/hooks.test.sh`、`bash test/harness.test.sh`。lint / 起動：なし
- global への展開：`bin/codingenv install`（ユーザーが実行する）。drift の確認：`bin/codingenv status`（読み取りのみ。Claude が実行してよい）
- `/taskman <やりたいこと>`：依頼を分類し、必要なら `/design` → `/changegraph` で Task を作る
- `/dispatch <Task | 依頼>`：次の Agent（Subagent）を呼ぶだけ。Issue・worktree・wave の進行は main loop が行う
- `/handoff [name]`：/clear 前に .claude/handoff/<name>.md を書き出して（自分のファイルだけ）コミット
- `/pickup [name]`：/clear 後に文脈を復元して復唱、handoff が複数あれば選ぶ（built-in の `/resume` とは別物）
- タスク一覧：`gh issue list --search "-label:doc:spec"`（`doc:spec` は設計判断で、タスクではない）

## 規約・注意点（デフォルトと違うものだけ）
- 編集は repo 内だけ。`~/.claude/` 配下は Claude も人も直接編集しない。global に効かせたい変更は repo の正本（`skills/`、`hooks/`、`global/CLAUDE.harness.md`）を直し、ユーザーに `bin/codingenv install` を依頼する（#93）
- Orchestrator のルールの正本は `global/CLAUDE.harness.md`。`skills/` と `hooks/` は symlink なので、編集すると全 project の挙動が即座に変わる
- Task = GitHub Issue（#103）。キューを自作しない
- `hooks/` も global の正本（#93）。編集は全 project に即座に効くので、変更したら必ず `bash test/hooks.test.sh` を通す
- Claude Code の仕様は docs の原文で確認する。WebFetch の要約は誤答したことがある

## コンテキスト圧縮時の指示
- 圧縮（compact）時は、変更したファイル一覧、テストコマンド、.claude/handoff/<name>.md の「決定事項」を必ず残すこと。
