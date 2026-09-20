# PROGRESS（進行中タスクの引き継ぎ）

<!-- /clear の前に handoff スキルで更新する。30行以内。古い内容は消して上書き。 -->

- 最終更新：2026-09-20

## 目的（なぜやるか）
全タスクを TaskQueue 経由で Subagent に実行させる、依存ゼロの最小ハーネスを作る。

## 完了条件（Done when）
- [x] `node --test test/tq.test.js` が通る（9/9）
- [ ] 未定：ハーネス全体の完了条件をユーザーと合意する

## スコープ外（やらないこと）
- 複数マシンでのキュー共有（ADR-0001 の再検討条件）

## 決定事項（理由つき）
- キューは JSONL + `bin/tq`、排他は mkdir、書き込みは tmp+rename — 依存ゼロで並列安全（docs/decisions/0001-task-queue.md）

## 却下した案（再提案しないこと）
- TASKS.md チェックボックス / ディレクトリ状態機械 / SQLite / tasks.jsonl の git 管理 — 理由は ADR-0001

## 現在の状態
- 済：`bin/tq`、テスト、ADR-0001、CLAUDE.md のコマンド・規約欄
- 途中：なし
- 未着手：CLAUDE.md 業務コンテキスト欄、docs/context/*.md、Subagent から tq を呼ぶ運用手順、remote / PR

## 次の一手（最初にやること）
1. 完了条件と業務コンテキストをユーザーに確認して CLAUDE.md を埋める

## 注意・未解決の質問
- README が案内する `.claude/skills/handoff` `.claude/skills/resume` がリポジトリに存在しない
- feat/task-queue は未マージ、remote 未設定
