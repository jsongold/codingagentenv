# 用語集

<!-- 業務用語とコード上の名前の対応を明確にする。曖昧語はここで定義する。 -->

| 業務用語 | 意味 | コード上の名前 | 混同しやすい語 |
|---|---|---|---|
| <例：案件> | <受注前の商談単位> | `Deal` | 「プロジェクト」（受注後） |
| Task | 1 PR 単位の作業。GitHub Issue として登録する (#103) | GitHub Issue | 自作キュー（作らない） |
| Orchestrator | main セッションの役割。依頼を分類して Task に分け、完了を判定してユーザーに報告する。実装は worker、配送は配送役が受け持つ | `skills/taskman` | worker（実装担当）、配送役（配送担当） |
| worker | Orca が起動する coding agent。Task を実装する | `orca orchestration worker-start` | Subagent（Agent tool で起動する） |
| 配送役 | worker の起動から merge までを受け持つ Subagent | `/cco` | worker（実装担当） |
| Subagent | Agent tool で起動する background エージェント。実装・検証・コミットを行う | `Agent` tool | agent teams の teammate (別セッション) |
| handoff | /clear 前に .claude/handoff/<name>.md を書き出してコミットする skill | `/handoff` | pickup（反対の処理） |
| harness.json | project ごとのルールを書くファイル。今は `tickets`（チケット管理の場所と spec の見分け方）だけ。project の `.claude/harness.json` > `~/.claude/harness.json` | `global/harness.json`, `/setupca` | settings.json（Claude Code の設定） |
| pickup | /clear 後に .claude/handoff/<name>.md・spec・担当 Issue を読み直して復唱する skill | `/pickup` | built-in `/resume` (セッション履歴の再開) |
