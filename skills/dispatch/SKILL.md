---
name: dispatch
description: Task を Subagent へ配送する。/taskman が作った Task 一覧、またはロジックを含まない依頼を受け取り、TaskList か Issue に登録して Subagent を起動する。分解・設計・実装はしない。
argument-hint: "[Task 一覧 | 依頼]"
---

# dispatch — Task を Subagent へ配送する

責任は登録と起動まで。実装・検証・コミットは Subagent が行う。

- Input：Task 一覧（`/taskman` の出力）、またはロジックを含まない依頼
- Output：Subagent への Task

## 前提チェック
- `TaskCreate` / `TaskList` / `TaskUpdate` / `TaskGet` が使えること。`~/.claude/settings.json` の `env` に `CLAUDE_CODE_ENABLE_TODO_TOOLS=1` がある前提。
  使えなければ止まって、その設定と再起動をユーザーに依頼する。代替のキューを自作しない。
- プロジェクトの `.claude/settings.json` の `env.CLAUDE_CODE_TASK_LIST_ID` が設定されていること。無ければ先に `/pickup` を実行するようユーザーに伝える。
  未設定だと Task list はセッション単位になり、`/clear` をまたいで残らない。

## 手順
1. Input を受け取る。依頼をそのまま受けたときは、分解せず 1 Task として扱う。Task 一覧が無く、依頼がロジックを含むなら `/taskman` に戻す。
2. 登録する。既定は TaskList（`TaskCreate`）。Issue で管理する repo なら Issue を作る。`TaskList` で既存を確認し、重複して作らない。
3. Subagent を起動する。
   - Subagent は文脈ゼロで始まる。プロンプトに Task の本文をそのまま渡し、関係する ADR（あれば）と報告形式を添える。
   - 報告形式は 15 行以内で、変更ファイル / 実行した検証コマンドと結果 / 指示と違えた点 / 未解決事項 だけ。ログ・diff・ファイル本文を貼らせない。
   - wave が同じで、触るファイルが重ならない Task だけ 1 メッセージで同時に起動する。
   - Agent 呼び出しごとに `model` を明示する（迷ったら 1 段上）。
     - `haiku`：転記・整形・機械的な置換。
     - `sonnet`：調査、手順が明確な実装、テスト追加。
     - 指定なし（既定モデル）：設計判断、デバッグ、ADR に触る変更。
4. 未起動の Task が無くなるまで 3 を繰り返す。区切りで `/handoff`。

## やらないこと
- 分解・設計（`/taskman` の仕事）。
- 実装・検証・コミット（Subagent の仕事）。
