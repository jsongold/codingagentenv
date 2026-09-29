---
name: pickup
description: /clear 後や新セッションの最初に、.claude/handoff/<name>.md・spec・担当 Issue を読み直して理解を復唱する。引数 <name> は任意。「前回の続き」と言われたら使う。
---

# pickup — /clear 後の再開

built-in の `/resume`（セッション履歴の再開）とは別物。こちらはファイルから文脈を復元する。引数 `<name>` は handoff の名前（任意）。

1. 読む handoff を決める。
   - 名前あり：`.claude/handoff/<name>.md` を読む。無ければ一覧（`ls -t .claude/handoff`）を見せる。
   - 名前なし・handoff が1件：それを読む。
   - 名前なし・handoff が複数：SessionStart hook が出した一覧（無ければ `ls -t .claude/handoff`）を見せてユーザーに選ばせる。選ぶまで作業を始めない。
   - 名前なし・handoff が0件：旧 `PROGRESS.md` があればそれを読み、次の `/handoff` で `.claude/handoff/default.md` に移行されると伝える。どちらも無ければ、無いと伝え、`git log` と open な Issue（spec を除く）から目的を復元して復唱し、次の区切りで `/handoff` するよう勧める。
2. 作業に関係する spec（設計判断）を読む。harness.json の tickets に従う（project の `.claude/harness.json` > `~/.claude/harness.json`）。一覧は SessionStart hook が出したもの。無ければ（gh の timeout・失敗など）取り直す：github のときは `gh issue list --label <tickets.spec.label> --state open`。関係するものだけ読む（`gh issue view <番号>`）。状態が「置き換え」のものと「却下した案」は再提案しない（置き換えられた spec は close せず open のまま、状態行で示す）。tickets が未設定・jira（一覧取得は未対応）・spec の label が無い・gh が使えないときは何も言わずに飛ばす。
   - project の `.claude/settings.json` に旧版が入れた `CLAUDE_CODE_TASK_LIST_ID` が残っていたら、削除するようユーザーに伝える（#103）。自分では編集しない。
3. handoff の「現在の状態」にある `Issue: #<番号>` の行だけを担当 Issue とみなし、`gh issue view <番号> --comments` でコメントまで読む。複数あれば1件ずつ読む。
   - `Issue: #<番号>` の形でない ID（旧 Task ID など）は無視し、`gh` に渡さない。`Issue:` 行が無ければ担当 Issue なしとして扱う。
   - `git status` と `git log --oneline -10` で handoff と実態のずれを探す。
   - Issue は全セッション共有。handoff の「担当する Issue」に無い他セッションの Issue を、自分のものとして進めない。
4. 「目的・完了条件・次の一手・この handoff が担当する Issue」を復唱する。handoff と実態がずれていたら、復唱の後に指摘する。
5. ユーザーの確認を待たずに進めてよいのは、handoff の「次の一手」が具体的で、ずれが無い場合だけ。
