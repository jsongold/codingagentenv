## ハーネス（全プロジェクト共通）

- 複数ステップの実装・調査は `/taskman` で Task を作り、`/dispatch` で Subagent に配送する。main は自分では実装しない
- 1ファイルの小さな修正と質問への回答は main が直接やる
- Task は GitHub Issue で管理する。main が `/taskman` の Task を Issue に登録してから `/dispatch` に渡す。Subagent が実装・検証・コミットし、完了の判断は Orchestrator（main）がする。1タスク = 1コミット
- commit の message 末尾に trailer を 2 つ書く。`State:` は `working`（実装中）/ `review-wait`（レビュー依頼済み）/ `merge-ready`（レビュー・CI 済み）のどれか。`Next:` は次の一手を 1 行で。空 commit は使わない
- Issue を閉じる前に、main が Subagent の報告を鵜呑みにせず完了条件（done）のコマンドを自分で再実行して確認し、通ったら `gh issue close <番号> --reason completed` で閉じる
- `/clear` 前は `/handoff`、`/clear` 後・新セッションの最初は `/pickup`（built-in の `/resume` とは別物）
- project ごとのルールは harness.json に書く（project の `.claude/harness.json` > `~/.claude/harness.json`。今は tickets だけ：チケット管理の場所（github / jira）と spec の見分け方）。spec は skill が harness.json の tickets を読んで探す（hook は spec を一覧しない）。未設定なら spec を探さない。作るときは `/setupca`
- キューや代替のタスク管理を自作しない。/clear をまたぐ状態は handoff と Issue に残す
- この節・skill・hook の正本は codingagentenv リポジトリ。`~/.claude/` 側を直接編集せず、リポジトリを直して `bin/codingenv install` で展開する
- 出力・インターフェースを推測で断言しない。実行結果 / 型定義 / API仕様など実物で確認する
- テストは小さい単位で都度実行する。最後にまとめてテストしない
- 最初から機能に厚みを持たせない。機能・実装・テストは最低限動くもの（少し足りないくらい）から始め、必要が見えてから足す
- README は最低限だけ書く：これが何か、使い方。背景が特殊な場合のみ 1 行程度で添える
- Issue・PR 本文・ドキュメントは冒頭に `目的：<1〜2行>` の行を置く。短いものでも省かない
- 並列開発（worktree 5〜10 本）が前提。コンフリクトを起こさない分割を厳守する
  - 1 PR = 1 つの変更。差分は目安 400 行以内（テスト・lock ファイル除く）。超えそうなら PR を分ける
  - ファイルは 1 ファイル 1 責務で細かく分ける。既存の大きいファイルに書き足すより、新しいファイルを足す
  - 複数 PR が触る共有ファイル（登録一覧・index・ルート一覧・設定）は衝突の元。ディレクトリ走査などの自動登録にするか、その追記だけを先に 1 PR で入れる
  - 複数 PR に分かれる作業は、着手前に ChangeGraph（PR 単位のノード: 触るファイル・依存先）を作る。同じファイルを触るノードは並列にしない
- PR 本文には、開発者がローカルで検証できるコピペ実行可能なコマンドを必ず書く（レビュー用 worktree の作成と移動から検証・片付けまで。例: `git fetch origin pull/<N>/head` → `git worktree add --detach ../<repo>-pr-<N> FETCH_HEAD` → `cd` → 依存インストール → lint/型/テスト → 必要なら手動確認手順 → `git worktree remove`。`gh pr checkout` は今のチェックアウトを切り替えるので使わない）。コードブロックにまとめ、プレースホルダを残さない
- PR 本文には、その PR で「〜ができること」を箇条書きのチェックリスト（`- [ ] 〜ができる`）で書く。レビュアーが動作確認でチェックを付けられる粒度にする
- PR のブランチに main を取り込むときは rebase ではなく merge（`git merge origin/main`）。履歴を書き換えないので force push が不要になる。force push はしない
- main セッションはオーケストレーター専任。実装・gate・merge・CI 待ち・レビュー・調査はすべて Subagent に出し、報告は 10 行以内にさせる
- Subagent にタスクを渡すときは、使うモデル（例: sonnet / opus / haiku / fable）をユーザーに伝える
- Subagent は 1 回のツール呼び出しを約 4 分以内に収める（600 秒進捗なしで watchdog に殺される）。長い pytest・CI 待ち・レビューは `run_in_background` で走らせてポーリングする
- merge 前のチェック（gate）の中身はアプリ依存で、ハーネスは持たない。正は対象 repo の CI（GitHub Actions）、spec の `done` は Worker / Subagent 上の事前確認。重いコマンドの同時実行数はホストの空き容量で決まり、その関門はメタデータアプリ `cad` が担う（Issue #10）。クラウドでは 1 sandbox = 1 run で、並列数 = worker 数。テスト DB は per-run コンテナを作らず、共有 Postgres（`testdb up` / `testdb url <worktree>` で worktree ごとに 1 DB）を使う
- merge には「実装者とは別のレビュアーによる独立した AI レビュー」と CI green（CI がある repo のみ）が必須。レビュアーは policy の優先順リストから選び、quota 切れなら自動で次にフォールバックする。現行の手段は `ai-review <pr> <worktree> --implementer <name>`（GitHub の Codex bot → `codex-localreview` → Claude の順。復帰待ちは `codex-probe <queue-file>` をバックグラウンドで）。実装者自身のレビューは事前チェック扱いで、merge 条件にはならない

## コード設計（全プロジェクト共通）

- 対象はコード。DB スキーマとインフラは対象外
- コンセプト：できる限りすべてを関数として表す。どの境界も「Input を受け取り Output を返すもの」とみなし、全体はその連なり（`X → a → x → b → y → c → z`）で表す。後から読んだ人が、データの出どころと行き先を追えるようにする
- 境界とは、名前を付けて他から呼ばれる・参照されるもの（関数・モジュール・クラス・CLI など）。ロジックを含むものを作るときは、先に設計を書く。境界ごとに次を書く
  - 責任：概要 / Scope
  - Input / Output（隣り合う境界の Output と Input を一致させる）
- 設計の最後に必ず Node Graph を Mermaid で載せる。境界をノード、データの受け渡しを矢印にする
- 設計は PR か Issue の本文に書く。手順とテンプレートは `/design` にある
