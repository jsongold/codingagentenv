# PROGRESS（進行中タスクの引き継ぎ）
<!-- /clear の前に handoff スキルで更新する。30行以内。古い内容は消して上書き。 -->
- 最終更新：2026-09-20 11:22

## 目的（なぜやるか）
全タスクを Claude Code 内蔵の Task list 経由で Subagent に実行させるハーネスを強化し、全プロジェクトに適用する。

## 完了条件（Done when）
- [ ] 新セッションで `/pickup` → `/dispatch` が3タスクを Subagent で完遂し、/clear 後も Task list が残る
- [ ] 別プロジェクト1つで `/pickup` → `/dispatch` が global skill で動く（TASK_LIST_ID が自動追加され、再起動後に Task list が共有される）
- [ ] hook が実機で効く：新セッションで PROGRESS.md が文脈に入り、VERIFIED 行の無い completed が拒否される

## 決定事項（理由つき）
- task queue = 内蔵 Task list。main が dispatcher — background Subagent は Task tools を持たないため（ADR-0002）
- ハーネスは global、project はデータだけ。正本は `skills/` と `hooks/`（ADR-0003, 0004）。dispatch 必須は複数ステップの実装・調査のみ（ユーザー決定）
- hook は SessionStart と TaskCompleted の2つ。検証は証跡行 `VERIFIED:` の有無で判定し、hook はコマンドを実行しない（ADR-0004）

## 却下した案（再提案しないこと）
- キューの自作 / agent teams / Subagent に Task 更新させる（ADR-0002）。テンプレートのコピー / 常に dispatch / 明示時のみ dispatch（ADR-0003）。hook でのコマンド実行 / task JSON 読み / git clean 要求 / PreToolUse での実装阻止（ADR-0004）

## 現在の状態
- 済：ADR-0001〜0004、skill 3本、global インストール（skill・env・CLAUDE.md）、hook 2本と install script、`bash test/hooks.test.sh` 26件 pass
- 途中：hook の global 登録。Claude からの `~/.claude/settings.json` 変更は auto mode に拒否された。ユーザーが `bash hooks/install.sh` を実行する
- 未着手：dispatch と hook の実地検証、CLAUDE.md 業務コンテキスト欄、docs/context/business.md、remote / PR

## 次の一手（最初にやること）
1. ユーザーが `bash hooks/install.sh` を実行 → Claude Code を再起動 → 新セッションで PROGRESS.md が自動で入るか確認し、`/dispatch` で3タスクの実地検証をする

## 注意・未解決の質問
- Task list が消えた：Task #2（pending）を含め `~/.claude/tasks/codingagentenv/` が空（.highwatermark は 9）。原因不明で、永続性は未確認。description 追記と completed を同じ `TaskUpdate` にまとめられるかも未確認（ADR-0004）
