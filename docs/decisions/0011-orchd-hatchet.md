# ADR-0011: `cad` は事実（usage）だけにし、判断は新設の `orchd` に分ける。task の状態・キュー・実行管理は Hatchet に任せる

- 日付：2026-09-28
- 状態：提案（決定 1 の place 分割は実装済み。Hatchet は未着手）

## 文脈
この repo の目的はコーディングをクラウドでスケールさせること。`cad` は ADR-0008 でメタデータの提供に範囲を絞ったが、ADR-0010 で place・policy（classes / rules / runners）・capacity・workers を抱え、事実（収集した値）と判断（どこで何を動かすか）が1つのバイナリに混ざった。

task を複数の Computer に配り、窓が尽きたら後回しにし、失敗したら再実行するには、永続する task の状態とキューが要る。自作は再発明になる（キューを自作しない）。

Hatchet（github.com/hatchet-dev/hatchet、MIT、v0.107.0 2026-09-15、約 8k stars。engine は Go + Postgres、SDK は Go / TS / Python）は次を持つ：永続する task 状態とキュー、key ごとの同時数、worker slots、worker label による affinity（beta）、retry / timeout、cancel、cron / schedule、durable sleep、GitHub webhook の cookbook、dashboard。

## 決定
1. **分割**：`cad` = 事実。usage の収集と配信だけを持つ（claude は `claude -p /usage`、codex は `codex app-server`、将来 opencode）。`orchd` = 判断。`.agent/policy.json`（2026-09-28 以降は `orchd/policy.json`。ADR-0010 追記）の classes / rules / runners、place、Hatchet への run の投入を持つ。同じ Go module でバイナリを 2 つにする。`orchd` は最初 CLI（`orchd place`、`orchd dispatch`）で、常駐が必要になったら daemon にする。
2. **Hatchet を task の状態・キュー・実行管理に採用する**。self-host の hatchet-lite（engine 1 コンテナ + Postgres。PoC で RAM 約 170MB + 約 300MB）。
   - worker は agent × computer ごとに label を持つ（例 `host=mac`, `agent=opencode`）。
   - `orchd` は place の結果を desired worker labels（required）に変換して run を投入する。
   - worker slots が local の同時数（今の `CAD_SLOTS=5`）を置き換える。
   - 使用枠の窓が尽きたときの defer（ADR-0010 の 409）は Hatchet の durable sleep / schedule で表す。
3. **Claude は引き続き Orchestrator の subagent（self）で動く**。Hatchet の claude worker は PoC では skip を返すだけ。Claude の実行経路は未決（下記）。
4. **`cad` から外すもの**：
   - capacity → Hatchet の worker slots
   - workers → Hatchet の worker registry
   - reviewer quota → 不要（書く agent の usage で判断する。owner 決定）
   - place / policy → `orchd`
5. **Computer の料金**は skypilot-catalog（`catalogs/v8/<cloud>/vms.csv`、7 時間ごとに自動更新、ライセンス要確認）と sandbox 比較サイトの snapshot を使う。place が料金を使うようになるまで取り込まない。

## 実装済み（2026-09-28）
- place は `orchd/`（独立した Go module `github.com/jsongold/codingagentenv/orchd`、`tools/orchd`）に移した。決定 1 の「同じ Go module」ではなく別 module にした（owner 決定：後で置き換え・削除するので `rm -rf orchd tools/orchd` で cad が壊れないこと）
- `orchd` は cad を import せず、`GET /v1/usage`・`GET /v1/capacity`（`CAD_ADDR` / `CAD_TOKEN`）だけで cad と話す
- `orchd` が読む policy は `rules`・`classes`・`runners`・`placement.reservePct`・`agents`（と rule の computer 名の存在確認に `computers` の名前）。`classes` 必須・runners の検証も `orchd` に移した
- `cad` から `POST /v1/place` と `cad show rules|classes|runners` を外した。`cad` は rules / classes / runners を検証せず、`cad add/rm` で書き戻すときはそのまま残す
- 追記（2026-09-28, owner 決定）：`orchd` の範囲は「判断だけ」から **Orchestrator を支える小さな CLI：pick・place・dispatch** に広げた。流れは NS ごとに `claude code (orchestrator) → orchd pick → orchd place → orchd dispatch → claude code (orchestrator)`。`pick` は `ai` Issue を 1 件取って `wip` を付け、`dispatch` は place の runner（subagent / process / cloud）に渡す。どれも結果の JSON を Orchestrator に返すだけで、常駐も状態も持たない（[orchd/README.md](../../orchd/README.md#orchestrator-との関係)）。上の「`orchd` = 判断」はこの追記で読み替える

## PoC 結果（2026-09-28、`~/projects/hatchet-poc`）
hatchet-lite v0.107.0 + postgres 15.6、TS SDK 1.33.2、Node v26。2 つの worker が同じ task `code-task` を登録し、振り分けは run の desired worker labels だけで決まる。
- place `light-edit` → opencode-worker が一時 worktree で `opencode run` を実行し COMPLETED
- `self` 付き → claude-worker（skip を返す）で COMPLETED
- 問題：
  - REST の task events が空で、どの worker が実行したかは DB（`v1_task_events_olap` の ASSIGNED）で確認した
  - worker を同時に登録すると duplicate-key（PutWorkflow）で片方が失敗する。1 秒ずらせば起きない
  - TS SDK は終了時に `process.exit` が要る
  - token の作成手順は docker-compose のページにしか書かれていない

## 検討して却下した案
| 案 | 却下理由 |
|---|---|
| `cad` に place を残す | 事実と判断が混ざる。ADR-0008 の範囲（メタデータ）を外れる |
| task キューを自作する | 再発明で保守対象が増える |
| Temporal | 重い（運用・学習コスト） |
| GitHub Actions を常用のキューにする | 振り分けも usage の判断もできない。Mac 睡眠時の経路としては ADR-0010 のまま残す |
| Hatchet の rate limit で 5h 窓を表す | rate limit の窓は固定区切りで rolling ではなく、5h の長さを指定できない |

## 影響
- 良い影響：
  - `cad` は収集と配信だけになり、判断の変更で `cad` を触らない
  - task の状態・再実行・defer・観測（dashboard）を自作せずに持てる
  - Computer が増えても worker を足して label を付けるだけで振り分けられる
- 受け入れたトレードオフ：Postgres の運用。worker affinity が beta。Hatchet への依存
- 更新が要る ADR：
  - ADR-0008：`cad` の責務を usage の収集・配信に縮小する。「既存 OSS の採用」の却下は再検討条件（満たす OSS が出てきた）に該当した
  - ADR-0010：place の置き場所を `cad` → `orchd` に移す。Mac 睡眠時の GitHub Actions 経路との関係を整理する

## 追記（2026-09-28）：版の引き継ぎと設定の互換
- **設定の互換ルール**：新しい版の cad / orchd は古い設定ファイル（`cad/config.json`、`orchd/policy.json`、state 配下）をそのまま読めること。キーの追加は既定値で吸収する（例 `placement.staleUsage` が無ければ `pass`）。キーの意味や形を壊す変更は、起動時に古い形を新しい形へ書き換える migration を同じ版に入れる。旧名は別名として残す（例 `CAD_POLICY`）
- **cad の入れ替え**：usage を集めるたびに `<state>/usage-snapshot.json` を atomic に書く（state = `CAD_STATE_DIR` > `<app>/state`）。起動時に 24 時間以内の snapshot を全 agent `stale: true`（`fetchedAt` は元のまま）で即公開し、最初の収集で上書きする
- **readiness**：`GET /healthz` は liveness のまま。`GET /healthz?ready` は snapshot を読んだか最初の収集が終わったら 200、それまで 503（JSON で reason）
- **orchd**：place の前に `/healthz?ready` を見る。503 なら exit 3（reason `cad not ready`、`defer_until` = 今 + 2 分）。接続できなければ 2 秒おきに 3 回再試行してから exit 1。stale な agent は `placement.staleUsage`（`pass` 既定 = 従来どおり配置し reason に `<agent>: usage stale`、`block` = 飛ばす）

## 未決
- self-host か Hatchet Cloud か（Cloud の無料枠は 1M runs / 月）
- Claude を Hatchet 経由で動かすか。subagent のままだと Claude の task は Hatchet の状態に乗らない
- GitHub Issue → Hatchet の起動方法（Hatchet の webhook か `orchd dispatch` か）
- Mac 睡眠時の GitHub Actions 経路を Hatchet の cloud worker に置き換えるか
- opencode の usage 収集（`/zen/go/v1/usage`、未確認）

## 再検討する条件
- Hatchet の開発が止まる、またはライセンスが変わったとき
- worker affinity が不安定で振り分けを誤るとき
- Postgres の運用が負担になったとき
