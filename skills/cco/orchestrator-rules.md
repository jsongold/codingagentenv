# Orchestrator の規則（main の役割・データの受け渡し）

global の harness 規則から移した、main と Subagent の運用ルール。`/cco` を回すときに読む。

## main の役割

main（Claude Code のメインセッション）が、依頼の分類・設計・実装・テスト・PR・CI 待ち・merge・調査・チケットの登録と修正まで、基本は自分で処理する。
- Subagent や Orca の worker に渡すかどうか、いつ渡すかは、main がその場で決める。渡すかどうかの基準は決めていない
- 渡したときは、報告を鵜呑みにせず、チケットを閉じる前に done のコマンドを自分で再実行する。Subagent と worker の報告は 10 行以内にさせる
- 流れ：main が Task をチケットに登録する → 実装 → PR・CI・merge → done コマンドを実行して判定し、チケットを閉じる。手順は `/cco`
- Task はチケットで管理する。置き場所は harness.json の `tickets` に従う（github なら Issue、jira なら Jira）

## データの受け渡し

- 10 行以上のデータ（表・一覧）は、会話にも Agent のプロンプトにも書かない。`/tmp` に TSV で保存し、パスで渡す
- Subagent は結果を `/tmp` の TSV に書き、報告はパスと行数だけにする
- TSV の形式：UTF-8 / LF / タブ区切り / 1 行目ヘッダ / 全行同じ列数 / セル内の `\t` `\n` `\\` はエスケープ
- md / HTML の成果物は TSV から `datadoc` で生成する（`datadoc a.tsv b.tsv --md|--html [-o out]`、`-` で stdin）。モデルが中身を書き写さない
- `datadoc` は `codingenv install` で `~/.local/bin` に展開される。repo を持たない環境（クラウドの worker・VM）では使えない
- 対象は今はデータだけ。Hook / skill での強制は未決定
