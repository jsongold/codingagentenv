# Commit を軸にタスクを回す設計（概要）

日付 2026-09-29。未コミットの設計メモ。前提: mission.md、ADR-0002 / 0004 / 0007 / 0009 / 0014 / 0015、orchd/wake.md、`.claude/handoff/dev-env-upgrade.md`。
「未検証」は Claude Code / git の挙動としてこの Mac で確かめていないもの。確認済み（git 2.39.5）: trailer の読み出し（`git log --format=%(trailers:key=…)`、`git interpret-trailers`）、空コミット（`--allow-empty`）、`git notes` の存在。

## 1. 問題（commit を軸にしていないことの不都合）

- **状態の正本が 4 か所に散っている**: Task list（マシンローカル、ADR-0002）、handoff（セッション単位、ADR-0007）、Issue コメント（owner 判断待ち、ADR-0015）、PR（レビュー・CI）。どれも task の「今どこまで・次は何・何で止まっているか」を単独では答えられない。
- **CCO（cloud セッション）からは Task list も handoff の続きも見えない**。Task list は `~/.claude/tasks/` でマシンローカル、handoff はセッションの名前を人が選ぶ前提。CCO が頼れるのは open PR / Issue の本文だけで、「owner 待ち」が PR コメントの文章にしか無いので判別できず、同じ PR を毎回読み直して止まる（観測された停滞）。
- **検証の記録（`VERIFIED:`）が成果物と別の場所にある**。task の description に書くので、PR・commit を見ても「どの head に対して何を通したか」が分からない。レビューは head SHA に対して行う（ADR-0015）のに、検証だけが SHA と結び付いていない。
- **`1 タスク = 1 コミット` の規則があるのに、commit は「結果」でしかなく「進行」を表さない**。途中で止めた task を別セッション・別マシンが拾うとき、push 済み branch はあっても「次の一手」は handoff にしか無い。

## 2. 案

### 案 A「commit 台帳」— task の状態遷移を全部 task branch の commit にする
- 進捗も状態変更も 1 遷移 = 1 commit。変更が無い遷移（検証済み・owner 待ち・レビュー待ち）は空コミット。commit message の trailer に `Task:` `State:` `Verified:` `Next:` `Blocked-On:` を書く。
- commit に載るもの: 進捗（diff）、検証結果（何を通したか）、次の一手、止まっている理由と誰待ちか。
- 既存との関係: Task list は「claim（in_progress）と completed」だけを持つ（ADR-0002 のまま）。`VERIFIED:` 行は commit の trailer を description に写す形で hook（ADR-0004）はそのまま。handoff はセッションの「決定事項・却下・会話にしか無い情報」に痩せ、task ごとの「現在の状態・次の一手」は `git log` から出す。orchd/wake.md に「open PR の branch の最新 commit の trailer を読み、`Blocked-On: owner` は飛ばす」を足す。新 ADR が 1 本要る。
- 主な欠点: 空コミットで履歴が増える（merge commit 方式なので main にも残る）。trailer は自己申告（今の `VERIFIED:` と同じ、ADR-0004 の受け入れ済みトレードオフ）。「1 タスク = 1 コミット」の言い換えが要る（1 task = 1 PR、1 遷移 = 1 commit）。branch が無い Issue（未着手）は対象外で、今までどおり Issue から拾う。

### 案 B「状態ファイルを branch で運ぶ」— task branch に 1 つの状態ファイルを置き、更新のたびに commit
- task ごとの handoff 相当（目的・完了条件・現在・次の一手・待ち）を branch 内のファイルに書き、進むたびに commit。merge 時に消す。
- commit に載るもの: 状態ファイルの差分（= 状態遷移の履歴）と実装の diff。
- 既存との関係: handoff の task 版。ADR-0009 が却下した「spec をコミットする」に近いが、spec（契約）ではなく進行状態、かつ task branch 内なので並列 worktree の衝突理由は当たらない。
- 主な欠点: 読むのに checkout か `git show` が要り、branch を横断して一覧できない。merge 前に消す手順を忘れると main に task ファイルが溜まる。owner が編集して commit するには branch を取り出す手間が要り、Issue コメントより重い。却下案との距離が近く、再提案と見なされる余地がある。

### 案 C「head SHA に外部状態を結び付ける」— commit の中身は変えず、SHA に付く印で次を決める
- 状態 = head SHA に付いた CI・レビュー・「検証済み」印・owner 判断（SHA を引用した PR レビュー / コメント）。CCO は「今の head に何が足りないか」から次を決める。
- commit に載るもの: 何も載せない。載るのは SHA を鍵にした GitHub 側の記録（または `git notes`。notes の push / fetch の既定挙動は未検証）。
- 既存との関係: ADR-0015 の「レビューは現在の PR head に対して」を検証と owner 判断にも広げるだけ。Task list / handoff は今のまま。
- 主な欠点: 状態が git ではなく GitHub に残り、wake ごとに API を叩く（コスト小だが決定的に読める形式を自分で決める必要がある）。branch の無い task には SHA が無い。「owner 待ち」が結局コメントの規約になり、今の停滞の原因（文章でしか分からない）を解消しにくい。

## 3. 推奨: 案 A（commit 台帳）。案 C の「head SHA に対してレビュー」は ADR-0015 のまま併用

- **コスト最小**: 新しいツールも常駐も要らない。`git log --format` は決定的で LLM を通さずに読める（mission「判断は分類に限る」と整合）。CCO が「owner 待ち」を trailer 1 行で判別できるので、同じ PR を毎回読み直すトークンが消える。
- **枠を使い切る**: `Blocked-On: owner` を機械的に飛ばせるので、owner 判断待ちで CCO が止まらず次の task に進む。停滞の原因に直接効く。
- **小さく独立**: 規約（trailer の名前）と `git log` の読み方だけ。skill / wake.md の文章変更で始められ、要らなくなれば trailer を書くのをやめるだけで戻せる。
- **/clear・マシン移動・cloud に強い**: push 済み branch にすべて載るので、Task list（マシンローカル）や spec（未コミット）が消えても、任意のセッションが `git fetch` だけで再開できる。ADR-0002 / 0009 で受け入れた「マシンローカル」のトレードオフを、状態の側面だけ補える。
- 案 B を採らない理由: 却下案に近く、横断一覧ができない。案 C 単独を採らない理由: 「owner 待ち」が文章のままで、停滞の原因が残る。

## 4. 未決事項（owner の判断が要る点）

1. **空コミットを許すか**。許さないなら状態遷移は「変更を伴う commit の trailer」だけになり、owner 待ち・レビュー待ちの印は PR コメント（案 C 寄り）に戻る。
2. **owner の返答はどこに書くか**。(a) owner も branch に空コミットで `Decision:` を積む、(b) PR コメントに書き CCO が次の wake で commit に写す、(c) Issue コメントのまま。(b) が今の運用に近いが 1 wake 遅れる。
3. **`VERIFIED:` の正本を commit に移すか、Task description との二重書きにするか**。hook（ADR-0004）は description を見るので、移すなら hook の判定対象を変える ADR が要る（hook の改変は全 project に効く、ADR-0004）。
4. **「1 タスク = 1 コミット」の規則の書き換え**（global/CLAUDE.harness.md）。1 task = 1 PR、1 遷移 = 1 commit に変えてよいか。
5. **branch 名の規約**。CCO が task branch を機械的に列挙するには `task/<n>` のような固定形が要る。今は `task/<n>`、`feat/…`、`docs/…` が混在。
6. **適用範囲**。まず codingagentenv だけか（自動 merge と同じ範囲、ADR-0015）、全 project のハーネス規約にするか。

## 5. 掘るならどこか

- trailer の語彙: `State:` の値集合（working / verified / review-wait / owner-wait / merge-ready）を最小で決める。
- wake.md の 1 段落: 「open PR の head commit の trailer を読み、owner-wait を飛ばし、最古の working から進める」の文面。
- `/pickup` `/handoff` の役割分担: handoff から「現在の状態・次の一手」を抜いて `git log` に寄せたとき、残る 6 項目で足りるか。
- 停滞の実測: CCO のログ（session_01RjAmXaVcwRf7xmeNoMPqhq）で、owner 待ち PR に何 wake 消費したかを数え、効果の見積りにする。
- 未検証の確認 2 点: cloud セッションが wake ごとに `git fetch` した最新 branch を見られるか（未検証）、`git notes` の push / fetch 既定（案 C を残す場合のみ、未検証）。
