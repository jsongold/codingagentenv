---
name: cco
description: Task を完了まで進める（実装 → done → PR → CI → merge → 判定 → チケットを閉じる）。基本は main が自分で進め、Orca の worker や Subagent に渡すかどうかは main がその場で決める。/taskman の Task 一覧を実行に移すとき、チケットの Task に着手するときに使う。
argument-hint: "[Task 一覧 | チケットの Key]"
---

# cco — Task を完了まで進める

責任は、Task（仕様と done コマンド）を受け取り、merge してチケットを閉じるところまで進めること。main の規則（委譲の扱い・done の再実行・データの受け渡し）は同じディレクトリの `orchestrator-rules.md` にある。

- Input：Task 一覧（`/taskman` の出力。Task ごとに チケットの Key・仕様ファイルのパス・done コマンド・触るファイル・wave）。仕様の形は `task-spec.md` にある
- Output：Task ごとの結果（PR、merge commit、done コマンドの結果、未解決事項）

## 誰が実行するか
main が自分で実行する（下の「手順」）。Orca の worker に振るか、Subagent に任せるかは、main がその場で決める。振るときは「worker に振るとき」に従う。

## チケットの Status
Status は、段階が変わったその場で動かす。後からまとめて直さない。harness.json の `tickets.system` が `jira` のときに行う（github のときは飛ばす）。

| 時点 | Status |
|---|---|
| Task 一覧を受け取った（着手する前） | 着手待ち（例：To Do） |
| 着手した（実装を始めた、または `worker-start` が通った） | 作業中 |
| PR を作った | レビュー中（その Status がある project だけ） |
| done コマンドや判定が通らず、やり直す | 作業中 |
| 着手をやめた | 着手待ち |
| 判定が通った | 完了（main だけが動かす） |

- Status の名前と遷移の id は project ごとに違う。最初に `getTransitionsForJiraIssue` で 1 回確かめ、記憶で決めない。動かすのは `transitionJiraIssue`。
- 親（Story・Epic）も同じ時点で動かす。子が 1 つでも作業中になったら、親も作業中にする。
- 遷移に失敗したら、Task を止めずに進め、報告の未解決事項に書く。

## 手順
1. goal：Task の完了条件から goal の文を 1 つ作る。形：`<Task の Key> の done コマンドがすべて通り、その出力が会話に出ていて、PR が merge され、チケットが閉じている。or stop after <N> turns`
   - `/goal <文>` を 1 行で示し、ユーザーに打ってもらう（`/goal` はユーザーが打つ組み込みコマンドで、main からは設定できない）。打たれたら続ける。
   - ユーザーが不在のとき（確認なしで進める指示のとき）は、goal の文を計画に書いて進め、goal を設定できなかったことを最後の報告に書く。
   - goal は session に 1 つで、新しい goal は前の goal を置き換える。状態は引数なしの `/goal` で見る。
2. 実装：Task ごとに、仕様どおりに変更する。worktree は、Task が 1 つなら今の worktree、並行して進めるなら Task ごとに新しい worktree。テストは小さい単位で都度実行する。1 Task = 1 コミット。
3. done〜merge：done コマンドを実行し、通ったら push して PR を作る。CI green を待って merge する。PR 本文の決まりと merge 条件は同じディレクトリの `pr-rules.md` に従う。
4. 判定：goal を判定するのは別のモデルで、会話に出た内容だけを見る（コマンドの実行もファイルの読み取りもしない）ので、done コマンドの出力と merge・チケットの結果を会話に出す。worker や Subagent に任せた Task は、報告を鵜呑みにせず done コマンドを自分で 1 回再実行する。
5. 通った Task から、その場でチケットを完了にする（ほかの Task の判定を待たない）。Jira のときは、完了にする前に `/jira-task` の「完了にするとき」の形でコメントを残す（「確認」には手順 4 の出力を使う）。子がすべて完了になった親（Story・Epic）も、同じ形のコメントを残して完了にする。
6. goal は条件を満たした時点で外れる。途中でやめるときは `/goal clear`。

## worker に振るとき
main が worker に振ると決めた Task だけ、この節に従う。待ち受けを main が自分でやるか、配送役の Subagent に任せるかも main が決める。配送役に任せるときは、Task 一覧（仕様はパスのまま）とこの skill を渡す。

Orca の Run（完了報告の受信箱）は端末ごとに 1 つなので、同じ wave の Task は 1 つの担当（main か配送役 1 つ）がまとめて受け持つ。配送役を並列に立てない。

Orca の guide（`orca skills get orchestration`）は、失敗したときだけ読む。

```sh
orca orchestration run-create --objective "<wave の目的>" --json
orca orchestration worker-start --spec "$(cat <仕様ファイルのパス>)" --worktree <current|new-child> --agent claude --json
orca orchestration check --wait --types "worker_done,escalation,question" --timeout-ms 230000 --json 2>/dev/null
orca orchestration reply --id <message_id> --body "<答え>" --json
orca orchestration worker-release --dispatch <dispatch_id> --json
orca orchestration check --ack <delivery_id> --json
```
- 新しい worktree（`new-child`）には `--name <名前> --base-branch <ref>` を足す。
- `2>/dev/null` は、待ちの間に stderr に出る `_keepalive` の行を捨てる。
- 続けて待つときは、3 行目に `--ack <delivery_id>` を足す。

1. assign：Task ごとに agent と worktree を決める。agent は指定が無ければ `claude`。worktree は、wave の Task が 1 つなら今の worktree、複数なら Task ごとに新しい worktree。
2. start：`run-create` と wave の全 Task の `worker-start` を、1 回の Bash にまとめる。仕様は `$(cat <パス>)` で渡し、本文を読まない・指示文に貼らない（仕様の形は `task-spec.md` に従う）。起動が通った Task のチケットは、待ちに入る前に作業中にする。
3. wait：`check --wait` を `--timeout-ms 230000` で 1 回を長くする（Bash の timeout は 240000）。heartbeat は完了ではない。heartbeat だけの Delivery や時間切れなら、何も書かずに次の待ちに入る。
4. worker の質問：Task の仕様から答えられるものだけ `reply` で答える。配送役が答えられないものは、推奨を添えて main に返す。
5. 起動に失敗したとき：ここで guide を読む（`orca skills get orchestration --reference recovery-and-cleanup`）。receipt の `recovery` に従って片付ける。確認画面（hooks・更新・trust・login）には答えない。同じ Task の再試行は 1 回まで。再試行もやめた Task のチケットは、着手待ちに戻す。
6. verify〜PR：`worker_done` の後、「done コマンド（worker の worktree で）→ `worker-release` → `check --ack` → push → PR 作成」を、`&&` でつないだ 1 回の Bash にまとめる。done コマンドが通らなければそこで止まるので、結果を添えて worker に差し戻す。ack は Delivery の全メッセージを処理してから。
7. CI green を待って merge する（`pr-rules.md`）。その後は「手順」の 4 から続ける。配送役は、Task ごとに PR・merge commit・done コマンドの結果・チケットの今の Status・未解決事項を、10 行以内で main に返す。

## やらないこと
- Task を作る・分ける（`/taskman`、`/changegraph`）、設計（`/design`）。
- 配送役がチケットを完了にする・完了のコメントを残す。完了にするのは、判定した main。
- Orca の guide を書き写す。ここに書くのは、通常の手順に要るコマンドだけ。
