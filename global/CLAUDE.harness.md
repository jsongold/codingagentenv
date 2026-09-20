## ハーネス（全プロジェクト共通）

- 複数ステップの実装・調査は `/dispatch` を通す。main は Task list に分解して Subagent に実行させ、自分では実装しない
- 1ファイルの小さな修正と質問への回答は main が直接やる
- Subagent の報告は main が完了条件のコマンドで検証してから completed にする。1タスク = 1コミット
- completed にする前に、task の description に `VERIFIED: <実行したコマンド> -> <結果>` を別の `TaskUpdate` で追記する。無いと TaskCompleted hook が拒否する
- `/clear` 前は `/handoff`、`/clear` 後・新セッションの最初は `/pickup`（built-in の `/resume` とは別物）
- キューや代替のタスク管理を自作しない。`~/.claude/tasks/` 配下を手で編集しない
- この節・skill・hook の正本は codingagentenv リポジトリ。`~/.claude/` 側を直接編集せず、リポジトリを直して `bin/codingenv install` で展開する
