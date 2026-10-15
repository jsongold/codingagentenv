# Orchestrator の規則（CCOinOrca・データの受け渡し）

global の harness 規則から移した、orchestrator と Subagent の運用ルール。`/cco` を回すときに読む。

## CCOinOrca（Orca の中の Claude Code Orchestrator）

main は決める・渡す・完了を判定するだけ。実装・テスト・PR・CI・merge・調査・チケットの登録と一括修正は worker か Subagent に出す。「数回のツール呼び出しで済む」は main がやる理由にならない
- 複数ステップの依頼では、最初のツール呼び出しの前に、ステップごとの担当を書いた計画を出す
- チケットを閉じる前に、main が worker や Subagent の報告を鵜呑みにせず、done のコマンドを自分で再実行する。Subagent と worker の報告は 10 行以内にさせる
- 担当の表と流れは `roles.md`（手順は `/cco`）

## データの受け渡し

- 10 行以上のデータ（表・一覧）は、会話にも Agent のプロンプトにも書かない。`/tmp` に TSV で保存し、パスで渡す
- Subagent は結果を `/tmp` の TSV に書き、報告はパスと行数だけにする
- TSV の形式：UTF-8 / LF / タブ区切り / 1 行目ヘッダ / 全行同じ列数 / セル内の `\t` `\n` `\\` はエスケープ
- md / HTML の成果物は TSV から `datadoc` で生成する（`datadoc a.tsv b.tsv --md|--html [-o out]`、`-` で stdin）。モデルが中身を書き写さない
- `datadoc` は `codingenv install` で `~/.local/bin` に展開される。repo を持たない環境（クラウドの worker・VM）では使えない
- 対象は今はデータだけ。Hook / skill での強制は未決定
