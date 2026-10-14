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
- 並列開発（worktree 5〜10 本）が前提。コンフリクトを起こさない分割を厳守する
  - 1 PR = 1 つの変更。差分は目安 400 行以内（テスト・lock ファイル除く）。超えそうなら PR を分ける
  - ファイルは 1 ファイル 1 責務で細かく分ける。既存の大きいファイルに書き足すより、新しいファイルを足す
  - 複数 PR が触る共有ファイル（登録一覧・index・ルート一覧・設定）は衝突の元。ディレクトリ走査などの自動登録にするか、その追記だけを先に 1 PR で入れる
  - 複数 PR に分かれる作業は、着手前に ChangeGraph（PR 単位のノード: 触るファイル・依存先）を作る。同じファイルを触るノードは並列にしない
- PR 本文には、開発者がローカルで検証できるコピペ実行可能なコマンドを必ず書く（レビュー用 worktree の作成と移動から検証・片付けまで。例: `git fetch origin pull/<N>/head` → `git worktree add --detach ../<repo>-pr-<N> FETCH_HEAD` → `cd` → 依存インストール → lint/型/テスト → 必要なら手動確認手順 → `git worktree remove`。`gh pr checkout` は今のチェックアウトを切り替えるので使わない）。コードブロックにまとめ、プレースホルダを残さない
- PR 本文には、その PR で「〜ができること」を箇条書きのチェックリスト（`- [ ] 〜ができる`）で書く。レビュアーが動作確認でチェックを付けられる粒度にする
- PR のブランチに main を取り込むときは rebase ではなく merge（`git merge origin/main`）。履歴を書き換えないので force push が不要になる。force push はしない
- Subagent にタスクを渡すときは、使うモデル（例: sonnet / opus / haiku / fable）をユーザーに伝える
- Subagent は 1 回のツール呼び出しを約 4 分以内に収める（600 秒進捗なしで watchdog に殺される）。長い pytest・CI 待ち・レビューは `run_in_background` で走らせてポーリングする
- merge 前のチェック（gate）の中身はアプリ依存で、ハーネスは持たない。正は対象 repo の CI（GitHub Actions）、spec の `done` は Worker / Subagent 上の事前確認。重いコマンドの同時実行数はホストの空き容量で決まり、その関門はメタデータアプリ `cad` が担う（Issue #10）。クラウドでは 1 sandbox = 1 run で、並列数 = worker 数。テスト DB は per-run コンテナを作らず、共有 Postgres（`testdb up` / `testdb url <worktree>` で worktree ごとに 1 DB）を使う
- merge には CI green が必須（CI がある repo のみ）。AI レビューは CI の job として走らせる（例: `claude-review`）。実装者自身のレビューは事前チェック扱いで、merge 条件にはならない

### CCOinOrca（Orca の中の Claude Code Orchestrator）

main（Claude Code のメインセッション）は、決める・渡す・完了を判定するだけ。作業の種類ごとの担当は次のとおり。

| 作業 | 担当 |
|---|---|
| 依頼の分類、設計の提案、Task への分割（`/taskman`） | main |
| Task の実装・テスト・コミット（1 Task = 1 コミット） | Orca の worker |
| worker の起動、待ち受け、質問への返答、片付け | Subagent（配送役） |
| PR 作成、CI 待ち、merge | Subagent（配送役） |
| 調査 | opencode（裏取りは Subagent） |
| チケットの登録と一括の修正 | Subagent |
| 完了の判定（done コマンドの再実行 1 回）と、ユーザーへの報告 | main |
| 1 ファイルの小さな修正、短い質問への回答 | main |

- 流れ：main が Task をチケットに登録する → 配送役が Orca の orchestration（skill `orchestration`。worker の起動は `orca orchestration worker-start`）で worker に振る → 待つ → worker の報告（`worker_done`）を受ける → PR・CI・merge → main が done コマンドを再実行して判定し、チケットを閉じる。手順は `/cco`
- 担当は作業の種類で決める。「数回のツール呼び出しで済む」ことは、main がやる理由にならない
- 複数ステップの依頼では、最初のツール呼び出しの前に、ステップごとの担当を書いた計画を出す
- Task はチケットで管理する。置き場所は harness.json の `tickets` に従う（github なら Issue、jira なら Jira）
- チケットを閉じる前に、main が worker や Subagent の報告を鵜呑みにせず、完了条件（done）のコマンドを自分で再実行して確認する
- Subagent と worker の報告は 10 行以内にさせる

### データの受け渡し

- 10 行以上のデータ（表・一覧）は、会話にも Agent のプロンプトにも書かない。`/tmp` に TSV で保存し、パスで渡す
- Subagent は結果を `/tmp` の TSV に書き、報告はパスと行数だけにする
- TSV の形式：UTF-8 / LF / タブ区切り / 1 行目ヘッダ / 全行同じ列数 / セル内の `\t` `\n` `\\` はエスケープ
- md / HTML の成果物は TSV から `datadoc` で生成する（`datadoc a.tsv b.tsv --md|--html [-o out]`、`-` で stdin）。モデルが中身を書き写さない
- `datadoc` は `codingenv install` で `~/.local/bin` に展開される。repo を持たない環境（クラウドの worker・VM）では使えない
- 対象は今はデータだけ。Hook / skill での強制は未決定

## コード設計（全プロジェクト共通）

- 対象はコード。DB スキーマとインフラは対象外
- コンセプト：できる限りすべてを関数として表す。どの境界も「Input を受け取り Output を返すもの」とみなし、全体はその連なり（`X → a → x → b → y → c → z`）で表す。後から読んだ人が、データの出どころと行き先を追えるようにする
- 境界とは、名前を付けて他から呼ばれる・参照されるもの（関数・モジュール・クラス・CLI など）。ロジックを含むものを作るときは、先に設計を書く。境界ごとに次を書く
  - 責任：概要 / Scope
  - Input / Output（隣り合う境界の Output と Input を一致させる）
- 設計の最後に必ず Node Graph を Mermaid で載せる。境界をノード、データの受け渡しを矢印にする
- 設計は PR か Issue の本文に書く。手順とテンプレートは `/design` にある

## 応答（全プロジェクト共通）

- 短い質問には1文で答える
- 選択肢を出したら、それで応答を終える。選択肢の後に説明・補足・質問を足さない（回答方法がわからなくなる）
