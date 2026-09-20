---
name: handoff
description: /clear やセッション終了の前に、進行中タスクの状態を PROGRESS.md に書き出してコミットする。
disable-model-invocation: true
---

# handoff — /clear 前の引き継ぎ

1. `TaskList` と `git status` / `git log --oneline -10` で現状を確認する。
2. PROGRESS.md を上書きする。30行以内。古い内容は消す。
   - 最終更新は絶対日時で書く。
   - 「決定事項」「却下した案」は理由つき。詳細は docs/decisions/ の ADR に書いて参照する。
   - 「次の一手」は、文脈ゼロの新セッションがそのまま実行できる具体さで書く。
   - 会話にしか無い情報（ユーザーの訂正、未解決の質問）を必ず拾う。
3. 未コミットの変更があればコミットする。PROGRESS.md も含める。
4. in progress のまま残るタスクがあれば、どこまで進んだかをタスクの説明に追記する。
5. 「/clear して `/pickup` で再開できる」と1行で伝える。
