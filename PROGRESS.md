# PROGRESS（進行中タスクの引き継ぎ）

<!-- /clear の前に handoff スキルで更新する。30行以内。古い内容は消して上書き。 -->

- 最終更新：2026-09-20 11:00

## 目的（なぜやるか）
全タスクを Claude Code 内蔵の Task list 経由で Subagent に実行させるハーネスを強化する。

## 完了条件（Done when）
- [x] Task tools が有効で、Task list が `~/.claude/tasks/codingagentenv/` に保存される
- [ ] 新セッションで `/pickup` → `/dispatch` が3タスクを Subagent で完遂し、/clear 後も Task list が残る（Task #2）。以降の条件は未定

## 決定事項（理由つき）
- task queue = 内蔵 Task list。main が dispatcher — background Subagent は Task tools を持たないため（ADR-0002）
- skill は project 側 `.claude/skills/` に置く — Context Kit のファイル構成が前提のため（ユーザー決定）
- 復帰 skill の名前は `/pickup` — built-in `/resume` と衝突するため

## 却下した案（再提案しないこと）
- `bin/tq` などキューの自作 / agent teams / Subagent に Task 更新させる — 理由は ADR-0002

## 現在の状態
- 済：`bin/tq` 削除、ADR-0002、settings.json、skill 3本（dispatch / handoff / pickup）、CLAUDE.md・README 更新
- 未着手：Task #2（dispatch の実地検証）、CLAUDE.md 業務コンテキスト欄、docs/context/*.md、remote / PR

## 次の一手（最初にやること）
1. 新セッションで `/pickup`、続けて `/dispatch` で Task #2 を実行する

## 注意・未解決の質問
- `/clear` 後に Task list が残るかは docs に記載なし。Task #2 で確認する
