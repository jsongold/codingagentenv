# 用語集

<!-- 業務用語とコード上の名前の対応を明確にする。曖昧語はここで定義する。 -->

| 業務用語 | 意味 | コード上の名前 | 混同しやすい語 |
|---|---|---|---|
| <例：案件> | <受注前の商談単位> | `Deal` | 「プロジェクト」（受注後） |
| Task list | Claude Code 内蔵のタスク管理 (TaskCreate / TaskGet / TaskList / TaskUpdate)。保存先 ~/.claude/tasks/codingagentenv/ | `TaskCreate`, `TaskGet`, `TaskList`, `TaskUpdate` | 自作キュー bin/tq (ADR-0001, 削除済み), "task queue" (= Task list のこと) |
| dispatcher | main セッションの役割。タスク分解・Subagent 起動・検証・コミット・TaskUpdate を行い、自分では実装しない | `.claude/skills/dispatch` | Subagent（実装担当） |
| Subagent | Agent tool で起動する background エージェント。Task tools を持たない | `Agent` tool | agent teams の teammate (別セッション) |
| handoff | /clear 前に PROGRESS.md を書き出してコミットする skill | `/handoff` | pickup（反対の処理） |
| pickup | /clear 後に PROGRESS.md・ADR・Task list を読み直して復唱する skill | `/pickup` | built-in `/resume` (セッション履歴の再開) |
