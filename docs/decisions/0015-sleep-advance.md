# ADR-0015: sleep 中のタスク前進は cad-2 の timer（orchd）が Issue / PR の状態から次の一手を決定的に選び、判断が要る作業だけ CC cloud worker に 1 通送る。完了 = merge

- 日付：2026-09-28
- 状態：採用

## 文脈
ADR-0014 で sleep 中は cad-2 の timer が `orchd tick` を回し、`ai` Issue を拾って配置するようにした。完了は `Closes #n` の PR。しかし PR ができた後のレビュー依頼・指摘の修正・main との衝突解消・merge は誰も進めず、朝まで PR が止まる。

これを CC cloud セッション内の `/loop` で回す案（CCO 化）を試したが、実測で発火しなかった（下の却下した案）。PR のライフサイクルで要る判断は「指摘を直す」「衝突を解消する」といった作業の中身だけで、次に何をするかは Issue のラベルと PR の状態から決定的に決まる。

## 決定
**sleep 中のタスク前進は cad-2 の timer（orchd）が決定的なチェック役となり、Issue / PR の状態から次の一手を選ぶ。判断が要る作業だけ CC cloud worker セッションへ `claude -p "<次の一手>" --cloud <session>` で 1 通送る。完了 = merge。**
- 状態はラベルと PR から読む

| 状態 | 次の一手 |
|---|---|
| `ai` 付き・未着手・マイルストーン無し | CC へ dispatch（既存の `dispatch --pending`） |
| PR あり・レビュー未依頼 | `@codex review` をコメント（GitHub の Codex bot。実装者と別のレビュアー） |
| レビューに P0/P1 あり | CC へ「PR #n を直して」 |
| 指摘なし・CI green | orchd が merge |
| main と衝突 | CC へ「`git merge origin/main` して」（rebase・force push はしない） |
| 60 分動き無し / N 往復で直らない | `ai-failed` |

- マイルストーン付きの Issue は対象外（owner 2026-09-28）

**置き換えるもの**：ADR-0014 の「完了 = `Closes #n` の PR」を「完了 = merge」に読み替え、tick の範囲を PR のライフサイクル（レビュー依頼・修正・衝突解消・merge）まで広げる（ADR-0014 のファイルは編集しない）。ADR-0014 の他の決定（sleep 中に LLM の Orchestrator を置かない、外部公開なし、REST）は維持する。

## 検討して却下した案
| 案 | 却下理由 |
|---|---|
| CC cloud セッション内の `/loop` で CCO 化する | 実測で発火しない。2026-09-28 12:29Z にセッションが「次回の確認は5分後です」と発言した後、16 分間 GitHub 上の活動がゼロ。その間に付いた Codex の新しい P1 にも反応しなかった（Task #81） |
| orchd がレビュー結果を解釈して自分で直す | 判断は LLM 側に置き、orchd は状態遷移だけを持つ |

## 影響
- 良い影響：sleep 中も PR が merge まで進む。orchd は状態を読んで 1 通送るだけで、判断は CC に閉じる。レビューは実装者と別の Codex bot が行う
- 受け入れたトレードオフ：自動 merge を許すのは当面 codingagentenv のみ（他 repo は owner 判断）。往復上限 N は未定（仮に 3）。直らないものは `ai-failed` にし、起きてから owner が見る
- 未検証の前提：
  - テストを回す CI が repo に無い（`.github/workflows` は `image.yml` のみ）。merge 条件に CI green を使うには追加が要る
  - Codex bot の P0/P1 はレビューコメント本文の badge（`![P1 Badge](...)`）で機械的に読める見込みだが、未実装
- 再検討する条件：cloud セッションで定期実行できる公式手段が確認できたら
