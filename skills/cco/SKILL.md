---
name: cco
description: Orca の中で、Task を worker に振って完了まで進める（振る → 待つ → 検証 → 片付け → PR → merge）。/taskman の Task 一覧を実行に移すとき、チケットの Task を coding agent に任せるときに使う。
argument-hint: "[Task 一覧 | チケットの Key]"
---

# cco — Task を振って完了まで進める（CCOinOrca）

責任は、Task（仕様と done コマンド）を受け取り、merge まで進めて main に結果を返すこと。役割の線引きはハーネスのルール（CCOinOrca の節）にある。

通常の手順に要る Orca のコマンドは、下の「Orca のコマンド」に書いてある。Orca の guide（`orca skills get orchestration`）は、失敗したときだけ読む。

- Input：Task 一覧（`/taskman` の出力。Task ごとに 仕様ファイルのパス・done コマンド・触るファイル・wave）。仕様は本文を貼らず、ファイルのパスで受け取る。仕様の形は `task-spec.md` にある
- Output：Task ごとの結果（PR、merge commit、done コマンドの結果、未解決事項）

## 誰が実行するか
main はこの手順を自分で実行しない。配送役の Subagent を 1 つ呼び、Task 一覧（仕様はパスのまま）とこの skill を渡す。

配送役のモデルは明示する。既定は sonnet。失敗時の判断が多い wave だけ 1 段上にする。使うモデルはユーザーに伝える。

Orca の Run（完了報告の受信箱）は端末ごとに 1 つなので、同じ wave の Task は 1 つの配送役がまとめて受け持つ。配送役を並列に立てない。

## main がやること
1. goal（配送役を呼ぶ前）：Task の完了条件から goal の文を 1 つ作る。形：`<Task の Key> の done コマンドがすべて通り、その出力が会話に出ていて、PR が merge され、チケットが閉じている。or stop after <N> turns`
   - `/goal <文>` を 1 行で示し、ユーザーに打ってもらう（`/goal` はユーザーが打つ組み込みコマンドで、main からは設定できない）。打たれたら続ける。
   - ユーザーが不在のとき（確認なしで進める指示のとき）は、goal の文を計画に書いて進め、goal を設定できなかったことを最後の報告に書く。
   - goal は session に 1 つで、新しい goal は前の goal を置き換える。状態は引数なしの `/goal` で見る。
2. 配送役を呼ぶ。配送役が返した質問に答える（ユーザーの判断が要るものはユーザーに聞く）。
3. 判定：報告を受けたら、done コマンドを自分で 1 回再実行する。goal を判定するのは別のモデルで、会話に出た内容だけを見る（コマンドの実行もファイルの読み取りもしない）ので、再実行の出力と merge・チケットの結果を会話に出す。
4. 通ったら、チケットをまとめて更新して閉じる。
5. goal は条件を満たした時点で外れる。途中でやめるときは `/goal clear`。

## Orca のコマンド
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

## 手順（配送役）
1. assign：Task ごとに agent と worktree を決める。agent は指定が無ければ `claude`。worktree は、wave の Task が 1 つなら今の worktree、複数なら Task ごとに新しい worktree。
2. start：`run-create` と wave の全 Task の `worker-start` を、1 回の Bash にまとめる。仕様は `$(cat <パス>)` で渡し、本文を読まない・指示文に貼らない（仕様の形は `task-spec.md` に従う）。
3. wait：`check --wait` を `--timeout-ms 230000` で 1 回を長くする（Bash の timeout は 240000）。heartbeat は完了ではない。heartbeat だけの Delivery や時間切れなら、何も書かずに次の待ちに入る。
4. worker の質問：Task の仕様から答えられるものだけ `reply` で答える。答えられないものは、推奨を添えて main に返す。
5. 起動に失敗したとき：ここで guide を読む（`orca skills get orchestration --reference recovery-and-cleanup`）。receipt の `recovery` に従って片付ける。確認画面（hooks・更新・trust・login）には答えない。同じ Task の再試行は 1 回まで。
6. verify〜PR：`worker_done` の後、「done コマンド（worker の worktree で）→ `worker-release` → `check --ack` → push → PR 作成」を、`&&` でつないだ 1 回の Bash にまとめる。done コマンドが通らなければそこで止まるので、結果を添えて worker に差し戻す。ack は Delivery の全メッセージを処理してから。
7. CI green を待って merge する。PR 本文の決まりと merge 条件はハーネスのルールに従う。
8. 報告：Task ごとに、PR・merge commit・done コマンドの結果・未解決事項を、10 行以内で main に返す。

## やらないこと
- Task を作る・分ける（`/taskman`、`/changegraph`）、設計（`/design`）。
- 配送役がチケットを更新する。更新は、main が完了を判定するときにまとめて行う。
- Orca の guide を書き写す。ここに書くのは、通常の手順に要るコマンドだけ。
