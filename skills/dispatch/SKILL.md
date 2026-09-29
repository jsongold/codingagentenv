---
name: dispatch
description: Task を Subagent へ配送する。/taskman が作った Task 一覧、またはロジックを含まない依頼を受け取り、Issue に登録して Subagent を起動する。分解・設計・実装はしない。
argument-hint: "[Task 一覧 | 依頼]"
---

# dispatch — Task を Subagent へ配送する

責任は登録と起動まで。実装・検証・コミットは Subagent が行い、完了の判断は Orchestrator が行う。進捗は Issue で管理する。

- Input：Task 一覧（`/taskman` の出力）、またはロジックを含まない依頼
- Output：Subagent への Task

## 手順
1. Input を受け取る。依頼をそのまま受けたときは、分解せず 1 Task として扱う。Task 一覧が無く、依頼がロジックを含むなら `/taskman` に戻す。
2. Task ごとに Issue を作る。`gh issue list` で既存を確認し、重複して作らない。
3. wave の順に Subagent を起動する。
   - 次の wave は、前の wave の Issue がすべて閉じてから起動する。
   - 同じ wave の Task を並列に起動するときは、先にノードごとに worktree を作る（1 ノード = 1 worktree = 1 PR）。Subagent はその worktree の中だけで作業する。
   - Subagent は文脈ゼロで始まる。プロンプトに Task の本文と Issue 番号をそのまま渡し、関係する ADR（あれば）と報告形式を添える。
   - 報告形式は 15 行以内で、変更ファイル / 実行した検証コマンドと結果 / 指示と違えた点 / 未解決事項 だけ。ログ・diff・ファイル本文を貼らせない。調査タスクは結論と根拠のパスを返させ、詳細は成果物ファイルに書かせる。
   - Agent 呼び出しごとに `model` を明示する（呼び出し時の指定が agent 定義や環境変数より優先される）。迷ったら 1 段上。
     - `haiku`：転記・整形・機械的な置換。
     - `sonnet`：調査、手順が明確な実装、テスト追加。
     - 指定なし（既定モデル）：設計判断、デバッグ、ADR に触る変更。
   - `CLAUDE_CODE_SUBAGENT_MODEL` で一律に下げない（判断タスクまで劣化する）。
4. 全 wave を起動し終えるまで 3 を繰り返す。区切りで `/handoff`。

## やらないこと
- 分解・設計（`/taskman` の仕事）。
- 実装・検証・コミット（Subagent の仕事）。
- 完了の判断（Orchestrator の仕事）。
