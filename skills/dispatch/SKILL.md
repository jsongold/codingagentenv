---
name: dispatch
description: 作業を Task list に分解し、1タスクずつ Subagent に実行させて結果で状態を更新する。複数ステップの実装・調査の依頼を受けたら、自分で手を動かす前に使う。1ファイルの小さな修正や質問への回答には使わない（main が直接やる）。
argument-hint: "[やりたいこと]"
---

# dispatch — Task list 経由で Subagent に実行させる

main セッションは dispatcher。自分では実装しない。理由：background Subagent は Task tools を持たず、自己申告の completed は検証を素通りする。
対象は複数ステップの実装・調査だけ。1ファイルの小さな修正と質問への回答は、dispatch せず main が直接やる。

## 前提チェック
- `TaskCreate` / `TaskList` / `TaskUpdate` / `TaskGet` が使えること。`~/.claude/settings.json` の `env` に `CLAUDE_CODE_ENABLE_TODO_TOOLS=1` がある前提。
  使えなければ止まって、その設定と再起動をユーザーに依頼する。代替のキューを自作しない。
- プロジェクトの `.claude/settings.json` の `env.CLAUDE_CODE_TASK_LIST_ID` が設定されていること。無ければ先に `/pickup` を実行するようユーザーに伝える。
  未設定だと Task list はセッション単位になり、`/clear` をまたいで残らない。

## 手順
1. `TaskList` で既存タスクを確認する。前セッションの in progress が残っていれば、成果物（git log / diff）を見て completed か pending に戻す。
2. 依頼（$ARGUMENTS）をタスクに分解して `TaskCreate` する。1タスク = 1コミットで完結する大きさ。順序があるものは依存を設定する。
3. 未ブロックの pending を選び、`TaskUpdate` で in progress にしてから Subagent を起動する。
   - Subagent は文脈ゼロで始まる。プロンプトに必ず入れる：目的、完了条件（実行できるコマンド）、触ってよいファイル、触ってはいけないもの、関係する ADR（あれば）、報告形式。
   - 同じファイルを触るタスクは並列にしない。独立したタスクだけ1メッセージで同時起動する。
   - 機械的な転記・整形は haiku、判断が要るものは既定モデル。
4. Subagent の報告を鵜呑みにしない。完了条件のコマンドを main で実行して確認する。
   - 通った：コミット → `TaskUpdate` で description の末尾に `VERIFIED: <実行したコマンド> -> <結果>` を1行追記 → 別の `TaskUpdate` で completed。
     コマンドで確認できないタスクは `VERIFIED: manual -> <何をどう確認したか>`。この行が無いと TaskCompleted hook が completed を拒否する（ADR-0004）。
   - 通らない：同じ Subagent に差し戻すか、原因をタスクの説明に追記して pending に戻す。Task list に failed 状態は無いので、失敗理由は説明欄に残す。
5. pending が無くなるまで 3〜4 を繰り返す。区切りで `/handoff`。

## やらないこと
- Subagent に Task list の更新を任せない（background Subagent は Task tools を持たない）。
- 未検証のまま completed にしない。
