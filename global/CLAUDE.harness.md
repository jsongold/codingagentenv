## ハーネス（全プロジェクト共通）

- `/clear` 前は `/handoff`、`/clear` 後・新セッションの最初は `/pickup`（built-in の `/resume` とは別物）
- project ごとのルールは harness.json に書く（project の `.claude/harness.json` > `~/.claude/harness.json`。今は tickets だけ：チケット管理の場所（github / jira）と spec の見分け方）。spec は skill が harness.json の tickets を読んで探す（hook は spec を一覧しない）。未設定なら spec を探さない。作るときは `/setupca`
- チケット管理が Jira の project（harness.json の `tickets.system` が `jira`）では、チケットを積む・分ける・完了にするときに必ず `/jira-task` を使う。Epic / Story / Task の表し方、起票の形、完了にするときのコメントの形はそこに従う
- キューや代替のタスク管理を自作しない。/clear をまたぐ状態は handoff とチケットに残す
- この節・skill・hook の正本は codingagentenv リポジトリ。`~/.claude/` 側を直接編集せず、リポジトリを直して `bin/codingenv install` で展開する
- 出力・インターフェースを推測で断言しない。実行結果 / 型定義 / API仕様など実物で確認する
- テストは小さい単位で都度実行する。最後にまとめてテストしない
- 最初から機能に厚みを持たせない。機能・実装・テストは最低限動くもの（少し足りないくらい）から始め、必要が見えてから足す
- README は最低限だけ書く：これが何か、使い方。背景が特殊な場合のみ 1 行程度で添える
- チケット・PR 本文・ドキュメントは冒頭に `目的：<1〜2行>` の行を置く。短いものでも省かない
- PR を作る前、並列開発の分割（1 PR = 1 変更・差分 400 行以内・ChangeGraph）を決める前に `~/.claude/skills/cco/pr-rules.md` を読む。PR 本文の決まり・main の取り込み方（merge。force push はしない）・gate の考え方がある
- merge には CI green が必須（CI がある repo のみ）。実装者自身のレビューは merge 条件にならない
- 作業は基本 main が自分で処理する。Subagent や Orca の worker に渡すかどうか、いつ渡すかは main がその場で決める
- Subagent にタスクを渡すときは、使うモデル（例: sonnet / opus / haiku / fable）をユーザーに伝える
- Subagent は 1 回のツール呼び出しを約 4 分以内に収める（600 秒進捗なしで watchdog に殺される）。長い pytest・CI 待ち・レビューは `run_in_background` で走らせてポーリングする

- main の規則（委譲の扱い・done の再実行）と、10 行以上のデータの受け渡しは `~/.claude/skills/cco/orchestrator-rules.md` にある（`/cco` で読む）

## コード設計（全プロジェクト共通）

- 設計の原則・手順・テンプレートは `/design` にある（ロジックを含む変更の前に使う）

## 応答（全プロジェクト共通）

- 短い質問には1文で答える
- 選択肢を出したら、それで応答を終える。選択肢の後に説明・補足・質問を足さない（回答方法がわからなくなる）
