---
name: cco
description: Orca の中で、Task を worker に振って完了まで進める（振る → 待つ → 検証 → 片付け → PR → merge）。/taskman の Task 一覧を実行に移すとき、チケットの Task を coding agent に任せるときに使う。
argument-hint: "[Task 一覧 | チケットの Key]"
---

# cco — Task を振って完了まで進める（CCOinOrca）

責任は、Task（仕様と done コマンド）を受け取り、merge まで進めて main に結果を返すこと。役割の線引きはハーネスのルール（CCOinOrca の節）にある。

Orca の操作手順そのものは Orca の skill `orchestration` にある（`orca skills get orchestration`）。ここには、その手順に足す差分だけを書く。

- Input：Task 一覧（`/taskman` の出力。Task ごとに 仕様・done コマンド・触るファイル・wave）
- Output：Task ごとの結果（PR、merge commit、done コマンドの結果、未解決事項）

## 誰が実行するか
main はこの手順を自分で実行しない。配送役の Subagent を 1 つ呼び、Task 一覧とこの skill を渡す。使うモデルはユーザーに伝える。

Orca の Run（完了報告の受信箱）は端末ごとに 1 つなので、同じ wave の Task は 1 つの配送役がまとめて受け持つ。配送役を並列に立てない。

## 手順（配送役）
1. assign：Task ごとに agent と worktree を決める。agent は指定が無ければ `claude`。worktree は、wave の Task が 1 つなら今の worktree、複数なら Task ごとに新しい worktree。
2. start：`orca orchestration run-create` の後、wave の Task を全部 `worker-start` してから待つ。spec には Task の本文をそのまま渡す（Target / Change / Constraints / Ownership / Observable acceptance を含むこと）。
3. wait：`orca orchestration check --wait` で待つ。出力は本体の JSON の前に `_keepalive` の行が並ぶので除く。heartbeat は完了ではない。1 回のツール呼び出しは 4 分以内に収める。
4. worker の質問：Task の本文から答えられるものだけ `reply` で答える。答えられないものは、推奨を添えて main に返す。
5. 起動に失敗したとき：receipt の `recovery` に従って片付ける。確認画面（hooks・更新・trust・login）には答えない。同じ Task の再試行は 1 回まで。
6. verify（事前確認）：`worker_done` の後、Task の done コマンドを worker の worktree で実行する。通らなければ、結果を添えて worker に差し戻す。
7. settle：`worker-release` で片付けてから `check --ack` で受領する。
8. PR を作り、CI green を待って merge する。PR 本文の決まりと merge 条件はハーネスのルールに従う。
9. 報告：Task ごとに、PR・merge commit・done コマンドの結果・未解決事項を、10 行以内で main に返す。

## main がやること
- 報告を受けたら、done コマンドを自分で 1 回再実行して完了を判定する。通ったらチケットを閉じる。
- 配送役が返した質問に答える（ユーザーの判断が要るものはユーザーに聞く）。

## やらないこと
- Task を作る・分ける（`/taskman`、`/changegraph`）、設計（`/design`）。
- Orca の skill にある手順を書き写す。
