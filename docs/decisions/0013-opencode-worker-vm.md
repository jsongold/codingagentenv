# ADR-0013: opencode の cloud worker は停止した GCE VM 2 台（Spot / standard）を on demand で起動する

- 日付：2026-09-28
- 状態：採用

## 文脈
opencode の task は local でしか動かせず、local の slot が埋まると待つしかなかった（ADR-0010 の rule で local は最後の手段）。常時動く VM は idle の費用がかかり、Spot は安いが preempt と容量不足で起動できないことがある。

## 決定
**e2-medium の COS VM を 2 台（`worker-spot`：Spot・termination action STOP、`worker-std`：standard）作っておき、普段は停止（`TERMINATED`）。`orchd dispatch`（runner mode `vm`）が start → IAP ssh で ready を待つ → worker image を `docker run`。VM は最後の worker container が終わって grace（既定 60 秒）後に自分で止まる（保険に idle 30 分）。**
- どちらを使うかは rule の順（auto：`opencode/*`×`gce-spot` → `gce-std` → local）。start できない・ready にならないと dispatch が exit 5、Orchestrator が `orchd place --exclude <computer>` で次へ
- preempt（PR なしで `TERMINATED`）は Issue を queue に戻す（`preempted` コメント、`ai-failed` は付けない）
- 実装：`Dockerfile.worker`、`deploy/worker-run.sh`、`deploy/gcp/{create-worker,worker-startup}.sh`（PR #53）、`orchd/vm.go`

## 検討して却下した案
| 案 | 却下理由 |
|---|---|
| 常時起動の worker VM | idle の費用（std で $24.46 / 月）。task は不定期 |
| task ごとに VM を create / delete | image の pull と boot disk の作成が毎回かかる。停止した VM の start は disk と cache 済み image が残る |
| suspend / resume | メモリの復元が再起動より遅いことがある（MIG standby pool の doc） |
| idle N 分で停止だけ | task の後も N 分課金される。event（container の die）で止め、idle は保険にする |
| Cloud Run Jobs / e2b | この PR の範囲外（rule と runner を足せば並べられる） |

## 影響
- 良い影響：local の slot に関係なく opencode の task を並べられる。停止中は disk だけの費用
- 受け入れたトレードオフ：dispatch に start + boot の数十秒〜（`startSec` で実測）。push は opencode の終了後 1 回なので、その前の preempt は作業を失う。grace 中の VM に来た dispatch と自己停止が競合すると exit 5 → 置き直し
- 再検討する条件：並列度が 2 台で足りない（MIG の standby pool）、`startSec` が遅すぎる、preempt が頻発する（周期的な WIP push か std を先に）

## Update 2026-09-28: gcloud の代わりに REST
- `orchd/vm.go` は gcloud・IAP ssh をやめ、Compute Engine REST API v1（`instances.get` / `setMetadata` / `start`、zone operation の poll）を標準ライブラリだけで呼ぶ。token は GCE の metadata server、無ければ `gcloud auth print-access-token`（Mac）。cad-2（cad image に gcloud が無い）からも dispatch できる
- task は metadata `worker-task` で渡す。VM の `worker-startup.sh` が boot 時に読んで `docker run` し、id を disk に記録して再 boot で再実行しない。ready marker と ssh は廃止
- `TERMINATED` 以外の VM は使用中として exit 5（VM 1 台に task 1 つ）。grace 中の VM の再利用は無くなったので grace の既定を 10 秒に下げた。start の失敗時は task を消す。setMetadata の fingerprint で同時 dispatch の上書きを防ぐ
- トレードオフ：container が起動できたかは orchd から見えない（PR の有無で判断）。cad-2 の SA に worker VM への compute 権限が要る
