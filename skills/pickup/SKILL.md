---
name: pickup
description: /clear 後や新セッションの最初に、PROGRESS.md・ADR・Task list を読み直して理解を復唱する。「前回の続き」と言われたら使う。
---

# pickup — /clear 後の再開

built-in の `/resume`（セッション履歴の再開）とは別物。こちらはファイルから文脈を復元する。

0. プロジェクトの `.claude/settings.json` に `env.CLAUDE_CODE_TASK_LIST_ID` があるか確認する。
   - 無ければ、プロジェクトのディレクトリ名を値にして追加する。ファイルが無ければ作る。既存の JSON にはマージし、他のキーを落とさない。
   - 追加したら「反映には Claude Code の再起動が必要」とユーザーに伝え、そのまま以降の手順を続ける。
1. PROGRESS.md を読む。無ければ無いと伝え、`git log` と Task list から目的を復元して復唱し、次の区切りで `/handoff` するよう勧める。
2. `docs/decisions/` があれば ADR を確認する。状態が「置き換え」のものと「却下した案」は再提案しない。無ければ何も言わずに飛ばす。
3. `TaskList` で残タスクを確認する。`git status` と `git log --oneline -10` で PROGRESS.md と実態のずれを探す。
4. 「目的・完了条件・次の一手」を3行で復唱する。PROGRESS.md と実態がずれていたら、復唱の後に指摘する。
5. ユーザーの確認を待たずに進めてよいのは、PROGRESS.md の「次の一手」が具体的で、ずれが無い場合だけ。
