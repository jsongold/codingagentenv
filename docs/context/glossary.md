# 用語集

<!-- 業務用語とコード上の名前の対応を明確にする。曖昧語はここで定義する。 -->

| 業務用語 | 意味 | コード上の名前 | 混同しやすい語 |
|---|---|---|---|
| <例：案件> | <受注前の商談単位> | `Deal` | 「プロジェクト」（受注後） |
| Task | 1 PR 単位の作業。チケットとして登録する（置き場所は harness.json の `tickets`。この repo では Jira の project CAO）(#103) | チケット（harness.json の `tickets`） | 自作キュー（作らない） |
| Orchestrator | main セッションの役割。依頼の分類から実装・merge・完了の判定まで基本は自分で処理し、worker や Subagent に渡すかどうかもその場で決める | `skills/taskman`, `skills/cco` | worker、配送役 |
| worker | Orca が起動する coding agent。main が振ると決めた Task を実装する | `orca orchestration worker-start` | Subagent（Agent tool で起動する） |
| 配送役 | main が任せると決めたときに、worker の起動から merge までを受け持つ Subagent | `/cco` | worker（実装担当） |
| Subagent | Agent tool で起動する background エージェント。main が渡すと決めた作業を受け持つ | `Agent` tool | worker（Orca が起動する）、agent teams の teammate (別セッション) |
| handoff | /clear 前に .claude/handoff/<name>.md を書き出してコミットする skill | `/handoff` | pickup（反対の処理） |
| harness.json | project ごとのルールを書くファイル。今は `tickets`（チケット管理の場所と spec の見分け方）だけ。project の `.claude/harness.json` > `~/.claude/harness.json` | `global/harness.json`, `/setupca` | settings.json（Claude Code の設定） |
| pickup | /clear 後に .claude/handoff/<name>.md・spec・担当 Issue を読み直して復唱する skill | `/pickup` | built-in `/resume` (セッション履歴の再開) |
