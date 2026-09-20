---
name: pickup
description: /clear 後や新セッションの最初に、PROGRESS.md・ADR・Task list を読み直して理解を復唱する。「前回の続き」と言われたら使う。
---

# pickup — /clear 後の再開

built-in の `/resume`（セッション履歴の再開）とは別物。こちらはファイルから文脈を復元する。

1. PROGRESS.md を読む。
2. docs/decisions/ の ADR を確認する。状態が「置き換え」のものと「却下した案」は再提案しない。
3. `TaskList` で残タスクを確認する。`git status` と `git log --oneline -10` で PROGRESS.md と実態のずれを探す。
4. 「目的・完了条件・次の一手」を3行で復唱する。PROGRESS.md と実態がずれていたら、復唱の後に指摘する。
5. ユーザーの確認を待たずに進めてよいのは、PROGRESS.md の「次の一手」が具体的で、ずれが無い場合だけ。
