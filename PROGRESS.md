# PROGRESS（進行中タスクの引き継ぎ）
<!-- /clear の前に handoff スキルで更新する。30行以内。古い内容は消して上書き。 -->
- 最終更新：2026-09-20 11:10

## 目的（なぜやるか）
全タスクを Claude Code 内蔵の Task list 経由で Subagent に実行させるハーネスを強化し、全プロジェクトに適用する。

## 完了条件（Done when）
- [x] Task tools が有効で、Task list が `~/.claude/tasks/codingagentenv/` に保存される
- [ ] 新セッションで `/pickup` → `/dispatch` が3タスクを Subagent で完遂し、/clear 後も Task list が残る（Task #2）
- [ ] 別プロジェクト1つで `/pickup` → `/dispatch` が global skill で動く（TASK_LIST_ID が自動追加され、再起動後に Task list が共有される）

## 決定事項（理由つき）
- task queue = 内蔵 Task list。main が dispatcher — background Subagent は Task tools を持たないため（ADR-0002）
- ハーネスは global、project はデータだけ。skill 正本は `skills/` で `~/.claude/skills/` へ symlink（ADR-0003）。dispatch 必須は複数ステップの実装・調査のみ、小修正と質問は main 直接（ユーザー決定）
- 個人の応答スタイル・情報提供ルールは `~/.claude/CLAUDE.md` に置き、この repo には入れない
- 復帰 skill の名前は `/pickup` — built-in `/resume` と衝突するため

## 却下した案（再提案しないこと）
- `bin/tq` などキューの自作 / agent teams / Subagent に Task 更新させる — 理由は ADR-0002。テンプレートを各 project にコピー / 常に dispatch / 明示時のみ dispatch — 理由は ADR-0003

## 現在の状態
- 済：`bin/tq` 削除、ADR-0002、skill 3本、CLAUDE.md・README 更新、glossary、ADR 一覧、ADR-0003、skills/ 移動、global インストール（symlink・env・CLAUDE.md）
- 未着手：Task #2、別プロジェクトでの検証、CLAUDE.md 業務コンテキスト欄、docs/context/business.md、remote / PR

## 次の一手（最初にやること）
1. Claude Code を再起動し、別プロジェクトで `/pickup` → 小さな複数ステップ依頼で `/dispatch` を試す。続けてこの repo で Task #2

## 注意・未解決の質問
- `~/.claude/settings.json` のバックアップは `~/.claude/settings.json.bak-20260920-pre-harness`。`~/.claude/CLAUDE.md` は git 管理外のまま。`/clear` 後に Task list が残るかは未確認（Task #2）
