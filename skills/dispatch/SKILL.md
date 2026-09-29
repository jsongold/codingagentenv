---
name: dispatch
description: Task を受け取り、次の Agent（Subagent）を呼ぶ。何も記録しない。Issue・worktree・wave の進行・handoff は main loop（Orchestrator）の仕事。分解・設計・実装もしない。
argument-hint: "[Task | 依頼]"
---

# dispatch — 次の Agent を呼ぶ

責任は次の Agent を呼ぶことだけ。何も記録しない。

- Input：Task（`/taskman` の出力）、またはロジックを含まない依頼
- Output：Subagent の呼び出し

## 手順
1. Input を受け取る。依頼をそのまま受けたときは、分解せず 1 Task として扱う。
2. Subagent を呼ぶ。
   - Subagent は文脈ゼロで始まる。プロンプトに Task の本文をそのまま渡し、関係する ADR（あれば）と報告形式を添える。
   - 報告形式は 15 行以内で、変更ファイル / 実行した検証コマンドと結果 / 指示と違えた点 / 未解決事項 だけ。ログ・diff・ファイル本文を貼らせない。調査タスクは結論と根拠のパスを返させ、詳細は成果物ファイルに書かせる。
   - Agent 呼び出しごとに `model` を明示する（呼び出し時の指定が agent 定義や環境変数より優先される）。迷ったら 1 段上。
     - `haiku`：転記・整形・機械的な置換。
     - `sonnet`：調査、手順が明確な実装、テスト追加。
     - 指定なし（既定モデル）：設計判断、デバッグ、ADR に触る変更。
   - `CLAUDE_CODE_SUBAGENT_MODEL` で一律に下げない（判断タスクまで劣化する）。

## やらないこと
- 記録（Issue の作成、handoff など）、worktree の作成、wave の進行、完了の判断（main loop の仕事）。
- 分解・設計（`/taskman` の仕事）。
- 実装・検証・コミット（Subagent の仕事）。
