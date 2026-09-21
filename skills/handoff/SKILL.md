---
name: handoff
description: /clear やセッション終了の前に、進行中タスクの状態を .claude/handoff/<name>.md（1セッション1ファイル）に書き出してコミットする。引数 <name> は任意。
disable-model-invocation: true
---

# handoff — /clear 前の引き継ぎ

引数 `<name>` は handoff の名前（任意）。kebab-case（`[a-z0-9-]`）以外は拒否して聞き直す。

1. `TaskList` と `git status` / `git log --oneline -10` で現状を確認する。
2. 保存先 `.claude/handoff/<name>.md` を決める。ディレクトリが無ければ作る。
   - 名前あり：そのまま使う。
   - 名前なし：既存の handoff が1件ならそれを更新する。0件なら `default`。複数なら一覧（`ls -t .claude/handoff`）を見せてユーザーに聞く。勝手に選ばない。
   - 旧 `PROGRESS.md` が project ルートにあり `.claude/handoff/` が無い場合：先に `git mv PROGRESS.md .claude/handoff/default.md`（未追跡なら `mv`）して、以降 `default` として扱う。移行したことをユーザーに伝える。
3. 保存先を上書きする。30行以内。古い内容は消す。無ければ次の見出しで新規作成する。冒頭の見出し行は `# handoff: <name>`。
   最終更新 / 目的 / 完了条件 / 決定事項（理由つき） / 却下した案 / 現在の状態 / 次の一手 / 注意・未解決の質問
   - 最終更新は絶対日時で書く。
   - 「決定事項」「却下した案」は理由つき。詳細を ADR に書いて参照するのは、プロジェクトに `docs/decisions/` がある場合だけ。無ければ理由は handoff ファイルに残す。
   - 「現在の状態」に「このセッションが担当する Task ID」を書く。Task list は project の全セッションで共有されるため。
   - 「次の一手」は、文脈ゼロの新セッションがそのまま実行できる具体さで書く。
   - 会話にしか無い情報（ユーザーの訂正、未解決の質問）を必ず拾う。
4. 自分の handoff ファイルだけをコミットする（`git add <path>` と `git commit <path>`。旧 `PROGRESS.md` を移行したときは `PROGRESS.md` の削除も同じコミットに含める）。他セッションの未コミット変更は巻き込まない。それらは commit せず、「注意・未解決の質問」に「未コミットの他変更あり」とだけ書く。
5. in progress のまま残るタスクがあれば、どこまで進んだかをタスクの説明に追記する。
6. 「/clear して `/pickup <name>` で再開できる」と1行で伝える。
