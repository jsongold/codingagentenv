# PROGRESS（進行中タスクの引き継ぎ）
<!-- /clear の前に handoff スキルで更新する。30行以内。古い内容は消して上書き。 -->
- 最終更新：2026-09-20 11:38

## 目的（なぜやるか）
全タスクを Claude Code 内蔵の Task list 経由で Subagent に実行させるハーネスを強化し、全プロジェクトに適用する。

## 完了条件（Done when）
- [ ] 新セッションで `/pickup` → `/dispatch` が3タスクを Subagent で完遂し、/clear 後も Task list が残る
- [ ] 別プロジェクト1つで `/pickup` → `/dispatch` が global skill で動く（TASK_LIST_ID が自動追加され、再起動後に Task list が共有される）
- [ ] hook が実機で効く：新セッションで PROGRESS.md が文脈に入り、VERIFIED 行の無い completed が拒否される

## 決定事項（理由つき）
- task queue = 内蔵 Task list。main が dispatcher — background Subagent は Task tools を持たないため（ADR-0002）
- ハーネスは global、project はデータだけ（ADR-0003）。編集は repo 内だけ、global への展開はユーザーが `bin/harness install` で行う。正本は `skills/`・`hooks/`・`global/CLAUDE.harness.md`（ADR-0005、ユーザー決定）。dispatch 必須は複数ステップの実装・調査のみ
- hook は SessionStart と TaskCompleted の2つ。検証は証跡行 `VERIFIED:` の有無で判定し、hook はコマンドを実行しない（ADR-0004）

## 却下した案（再提案しないこと）
- キューの自作 / agent teams / Subagent に Task 更新させる（ADR-0002）。テンプレートのコピー / 常に dispatch / 明示時のみ dispatch（ADR-0003）。hook でのコマンド実行 / task JSON 読み / git clean 要求 / PreToolUse での実装阻止（ADR-0004）。Claude による `~/.claude/` の直接編集 / 手作業 install（ADR-0005）

## 現在の状態
- 済：ADR-0001〜0005、skill 3本、hook 2本、`bin/harness`（install / uninstall / status）、テスト 20件 + 28件 pass
- 途中：global への展開。`bin/harness status` は hook 2本と CLAUDE.md ハーネス節が未展開と報告する（skill と env は展開済み）
- 未着手：dispatch と hook の実地検証、CLAUDE.md 業務コンテキスト欄、docs/context/business.md、remote / PR

## 次の一手（最初にやること）
1. ユーザーが `bin/harness install` を実行 → `bin/harness status` が全て ok → Claude Code を再起動 → 新セッションで PROGRESS.md が自動で入るか確認し、`/dispatch` で3タスクの実地検証をする

## 注意・未解決の質問
- Task list が消えた：Task #2（pending）を含め `~/.claude/tasks/codingagentenv/` が空（.highwatermark は 9）。原因不明で、永続性は未確認。description 追記と completed を同じ `TaskUpdate` にまとめられるかも未確認（ADR-0004）
