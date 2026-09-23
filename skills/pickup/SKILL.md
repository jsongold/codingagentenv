---
name: pickup
description: /clear 後や新セッションの最初に、.claude/handoff/<name>.md・ADR・Task list を読み直して理解を復唱する。引数 <name> は任意。「前回の続き」と言われたら使う。
---

# pickup — /clear 後の再開

built-in の `/resume`（セッション履歴の再開）とは別物。こちらはファイルから文脈を復元する。引数 `<name>` は handoff の名前（任意）。

0. プロジェクトの `.claude/settings.json` に `env.CLAUDE_CODE_TASK_LIST_ID` があるか確認する。
   - 無ければ、プロジェクトのディレクトリ名を値にして追加する。ファイルが無ければ作る。既存の JSON にはマージし、他のキーを落とさない。
   - 追加したら「反映には Claude Code の再起動が必要」とユーザーに伝え、そのまま以降の手順を続ける。
1. 読む handoff を決める。
   - 名前あり：`.claude/handoff/<name>.md` を読む。無ければ一覧（`ls -t .claude/handoff`）を見せる。
   - 名前なし・handoff が1件：それを読む。
   - 名前なし・handoff が複数：SessionStart hook が出した一覧（無ければ `ls -t .claude/handoff`）を見せてユーザーに選ばせる。選ぶまで作業を始めない。
   - 名前なし・handoff が0件：旧 `PROGRESS.md` があればそれを読み、次の `/handoff` で `.claude/handoff/default.md` に移行されると伝える。どちらも無ければ、無いと伝え、`git log` と Task list から目的を復元して復唱し、次の区切りで `/handoff` するよう勧める。
2. `docs/decisions/` があれば ADR を確認する。状態が「置き換え」のものと「却下した案」は再提案しない。無ければ何も言わずに飛ばす。
3. `TaskList` で残タスクを確認する。`git status` と `git log --oneline -10` で handoff と実態のずれを探す。
   - Task list は全セッション共有。handoff の「担当する Task ID」に無い他セッションのタスクを、自分のものとして進めない。
4. 「目的・完了条件・次の一手・この handoff が担当する Task ID」を復唱する。handoff と実態がずれていたら、復唱の後に指摘する。
5. ユーザーの確認を待たずに進めてよいのは、handoff の「次の一手」が具体的で、ずれが無い場合だけ。
