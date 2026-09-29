# 用語集

<!-- 業務用語とコード上の名前の対応を明確にする。曖昧語はここで定義する。 -->

| 業務用語 | 意味 | コード上の名前 | 混同しやすい語 |
|---|---|---|---|
| <例：案件> | <受注前の商談単位> | `Deal` | 「プロジェクト」（受注後） |
| Task | 1 PR 単位の作業。GitHub Issue として登録する (ADR-0016) | GitHub Issue | 自作キュー（作らない） |
| Orchestrator | main セッションの役割。Task を作って Subagent を呼び、完了を判断する。自分では実装しない | `skills/taskman`, `skills/dispatch` | Subagent（実装担当） |
| Subagent | Agent tool で起動する background エージェント。実装・検証・コミットを行う | `Agent` tool | agent teams の teammate (別セッション) |
| handoff | /clear 前に .claude/handoff/<name>.md を書き出してコミットする skill | `/handoff` | pickup（反対の処理） |
| pickup | /clear 後に .claude/handoff/<name>.md・ADR・Issue を読み直して復唱する skill | `/pickup` | built-in `/resume` (セッション履歴の再開) |
