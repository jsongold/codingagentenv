# handoff: default
<!-- /clear の前に /handoff <name> で更新する。30行以内。古い内容は消して上書き。 -->
- 最終更新：2026-09-20 15:30

## 目的（なぜやるか）
全タスクを Claude Code 内蔵の Task list 経由で Subagent に実行させるハーネスを強化し、全プロジェクトに適用する。

## 完了条件（Done when）
- [ ] 新セッションで `/pickup` → `/dispatch` が3タスクを Subagent で完遂し、/clear 後も Task list が残る
- [ ] 別プロジェクト1つで `/pickup` → `/dispatch` が global skill で動く（TASK_LIST_ID が自動追加され、再起動後に Task list が共有される）
- [x] hook が実機で効く：TaskCompleted の拒否は確認済み（ADR-0004）。2026-09-20 の新セッションで SessionStart hook が PROGRESS.md と ADR 一覧を文脈に注入することを確認済み

## 決定事項（理由つき）
- task queue = 内蔵 Task list。main が dispatcher — background Subagent は Task tools を持たないため（ADR-0002）
- ハーネスは global、project はデータだけ（ADR-0003）。編集は repo 内だけ、global への展開はユーザーが `bin/codingenv install` で行う。正本は `skills/`・`hooks/`・`global/CLAUDE.harness.md`（ADR-0005、ユーザー決定）。dispatch 必須は複数ステップの実装・調査のみ
- hook は SessionStart と TaskCompleted の2つ。検証は証跡行 `VERIFIED:` の有無で判定し、hook はコマンドを実行しない（ADR-0004）

## 却下した案（再提案しないこと）
- キューの自作 / agent teams / Subagent に Task 更新させる（ADR-0002）。テンプレートのコピー / 常に dispatch / 明示時のみ dispatch（ADR-0003）。hook でのコマンド実行 / task JSON 読み / git clean 要求 / PreToolUse での実装阻止（ADR-0004）。Claude による `~/.claude/` の直接編集 / 手作業 install（ADR-0005）

## 現在の状態
- 済：ADR-0001〜0005、skill 3本、hook 2本、`bin/codingenv`（install / uninstall / status）、テスト 20件 + 28件 pass
- 済（token 削減）：ADR-0006（TaskCompleted hook が `.claude/token-usage.jsonl` に usage を追記、opt-in）、project 設定で plugin 2 / skill 6 を非表示、dispatch に model 3段基準と報告15行制限、テスト 38件 pass。ベースラインは docs/context/token-baseline.md（初回コンテキスト 約 47,000、消費の大半は cache_read、Subagent 固定費 3〜6万）
- 済：global 展開（`bin/codingenv status` 全9項目 ok、2026-09-20）
- 済：CLAUDE.md 業務コンテキスト欄（5d72bf3。優先順位だけ「未定（ユーザー確認待ち）」）
- 未着手：別プロジェクトでの実地検証、docs/context/business.md（空テンプレートのまま）、remote / PR

## 次の一手（最初にやること）
1. `/dispatch` で3タスクの実地検証（Task #18〜#20 で実施中）。完了後 /clear して Task list が残るか確認
2. token 再計測：Task #20 で実施中。新セッションの初回コンテキストを token-baseline.md の方法で測り 約 47,000 と比較

## 注意・未解決の質問
- Task list が消えた：Task #2（pending）を含め `~/.claude/tasks/codingagentenv/` が空。原因不明で、永続性は未確認。補足：.highwatermark は 9 だったが、新規 TaskCreate は #18 から採番された（2026-09-20）
