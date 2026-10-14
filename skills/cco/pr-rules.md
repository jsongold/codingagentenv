# PR・並列開発の決まり
目的：PR を作る・分割を決めるときに読む決まりを 1 か所にまとめる（常駐の harness から移した）。

- 並列開発（worktree 5〜10 本）が前提。コンフリクトを起こさない分割を厳守する
  - 1 PR = 1 つの変更。差分は目安 400 行以内（テスト・lock ファイル除く）。超えそうなら PR を分ける
  - ファイルは 1 ファイル 1 責務で細かく分ける。既存の大きいファイルに書き足すより、新しいファイルを足す
  - 複数 PR が触る共有ファイル（登録一覧・index・ルート一覧・設定）は衝突の元。ディレクトリ走査などの自動登録にするか、その追記だけを先に 1 PR で入れる
  - 複数 PR に分かれる作業は、着手前に ChangeGraph（PR 単位のノード: 触るファイル・依存先）を作る。同じファイルを触るノードは並列にしない
- PR 本文には、開発者がローカルで検証できるコピペ実行可能なコマンドを必ず書く（レビュー用 worktree の作成と移動から検証・片付けまで。例: `git fetch origin pull/<N>/head` → `git worktree add --detach ../<repo>-pr-<N> FETCH_HEAD` → `cd` → 依存インストール → lint/型/テスト → 必要なら手動確認手順 → `git worktree remove`。`gh pr checkout` は今のチェックアウトを切り替えるので使わない）。コードブロックにまとめ、プレースホルダを残さない
- PR 本文には、その PR で「〜ができること」を箇条書きのチェックリスト（`- [ ] 〜ができる`）で書く。レビュアーが動作確認でチェックを付けられる粒度にする
- PR のブランチに main を取り込むときは rebase ではなく merge（`git merge origin/main`）。履歴を書き換えないので force push が不要になる。force push はしない
- merge 前のチェック（gate）の中身はアプリ依存で、ハーネスは持たない。正は対象 repo の CI（GitHub Actions）、spec の `done` は Worker / Subagent 上の事前確認。重いコマンドの同時実行数はホストの空き容量で決まり、その関門はメタデータアプリ `cad` が担う（Issue #10）。クラウドでは 1 sandbox = 1 run で、並列数 = worker 数。テスト DB は per-run コンテナを作らず、共有 Postgres（`testdb up` / `testdb url <worktree>` で worktree ごとに 1 DB）を使う
- merge には CI green が必須（CI がある repo のみ）。AI レビューは CI の job として走らせる（例: `claude-review`）。実装者自身のレビューは事前チェック扱いで、merge 条件にはならない
