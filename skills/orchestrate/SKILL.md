---
name: orchestrate
description: NS の Orchestrator として、GitHub の `ai` ラベル付き Issue を 1 件取り、class を分類し、orchd が決めた資源（subagent / process / cloud）に実行させる。「Issue を処理して」「ai の Issue を回して」と言われたら使う。
argument-hint: "[ns]"
---

# orchestrate — Issue を 1 件、orchd 経由で実行させる

流れ（NS ごと）：`claude code (orchestrator) → orchd pick → orchd place → orchd dispatch → claude code (orchestrator)`。
orchd は毎回 Orchestrator（このセッション）が呼び、結果の JSON を stdout で Orchestrator に返す。次の一手は Orchestrator が決める。判断は class の分類だけ（ADR-0010）。詳細は `orchd/README.md`。

1. `orchd pick --ns <ns>` → `{issue, classes}`（`{none}` なら終わり）。Issue には `wip` が付く。
2. `classes[].criteria` から class を 1 つ選び、理由を 1 行で Issue にコメントする。
3. `orchd place --ns <ns> --class <c>` → exit 0 なら placement JSON。3 は `defer_until` まで待つ（`wip` を外して `DEFER until <t>` とコメント）、1/2/4 は `wip` を外し、理由をコメントして止める。
4. dispatch は必ず background の Subagent（Agent tool、`run_in_background`）の中で実行する。完了は Subagent の通知で知る。
   - runner `subagent`：Subagent（既定モデル）が `orchd dispatch --ns <ns> --issue <n> --placement '<JSON>'` を実行し、返った `worktree` で `prompt` のとおり作業して PR（`Closes #n`）を出す。
   - runner `process` / `cloud`：監視役の Subagent（`model: haiku`）が dispatch を実行し、`orchd status --issue <n> [--pid <pid>]` を 1 回 4 分以内の呼び出しで繰り返して（60 秒おき・既定 60 分まで）PR か失敗を待つ。
   - 報告は 5 行以内：issue、runner、PR URL か失敗理由。

MODE：owner が「MODE=URGENT」と言ったら `orchd mode set urgent --ns <ns>`、「解除」で `orchd mode clear --ns <ns>`。
