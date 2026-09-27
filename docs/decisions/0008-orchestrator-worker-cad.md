# ADR-0008: Orchestrator（ローカル）と Worker（ローカル／任意のクラウド）を分け、メタデータは Go 製の `cad` が決定的に返す

- 日付：2026-09-27
- 状態：採用

## 文脈
並列開発（worktree 5〜10 本）で gate・レビュー・実装を回すと、Mac 1台のメモリと CPU が詰まる。Worker を外に出したいが、クラウドは1つに決めない（GCE Spot、Cloud Run Jobs、E2B / Daytona 系の sandbox など、task ごとに向き不向きがある）。長期構想ではなく、今の作業として必要。

同時実行数（slots）や使うレビュアーのような動的な値を、そのたびにプロンプトで調べさせると、結果が揺れて検証できない。値はツール／アプリが決定的に返すべき。

レビュアー（Codex bot など）は quota が切れる。切れたときに手で別レビュアーへ切り替えるのは漏れる。

タスクの状態は内蔵 Task list で持つ（ADR-0002）。新しい仕組みがタスク状態を持つと、キューの自作になり ADR-0002 と衝突する。

## 決定
- **分割**：Orchestrator はローカル Mac（IDE + Claude Code の main セッション）。Worker はローカルまたは任意のクラウド。provider は固定せず、エージェントが `policy.providers.allowed` の中から task ごとに選ぶ。クラウドでの task の起動・追跡は `dev-dispatch` ツールが行い、provider ごとのスクリプトを `agent/providers/<name>/` に置く（start / list / stop）。`cad` はその状態を読むだけ。詳細は後続の ADR / PR で決める。
- **動的な値はプロンプトで取らない**。slots・レビュアー選択などはツール／アプリが決定的に返す。
- **メタデータアプリ `cad` をこの repo に新設する**。Go（標準ライブラリのみ、単一バイナリ、linux + darwin）。repo の TS 優先の例外とする。理由は Worker への配布が単一バイナリで済むこと、常駐メモリが小さいこと。
- **`cad` の責務はこれだけ**：メタデータ（policy・capacity・workers・reviewer quota）の読み出しを提供し、そのデータを収集する。インメモリ。タスク状態は持たない（タスクは内蔵 Task list のまま、ADR-0002）。ローカルでもクラウドでも動く。
- **スキーマ**
  - `.agent/policy.json`：`{version, gate:{memoryMB, cpus, reserveMB, maxSlots}, review:{reviewers[], excludeImplementer, requireCI}, providers:{allowed[]}}`
  - Capacity：`{host, collectedAt, memTotalMB, memFreeMB, cpus, load1, slots}`
  - Worker：`{id, provider, state(starting|running|stopped|unknown), startedAt, lastSeenAt, labels}`
  - Quota：`{reviewer, state(ok|exhausted), lastHitAt?, resetAt?, source(reported|probed)}`
  - `slots = max(0, min(floor((memFree - reserveMB) / gate.memoryMB), floor(cpus / gate.cpus), maxSlots))`。上書きの優先順位は `CAD_SLOTS` 環境変数 > policy の `maxSlots`。
  - `slots = 0` でも、何も走っていなければ agent-gate は1本だけ走らせる（持ち主の決定。詰まって永久に進まない状態を避ける）。
- **API**
  - 読み出し：`GET /v1/meta`、`/v1/policy`、`/v1/capacity`、`/v1/workers`、`/v1/quota`、`/healthz`
  - 書き込みは `POST /v1/quota/:reviewer` の1つだけ
  - Push：SSE `GET /v1/events?topics=...`。topic 単位。`Event{topic, rev, at, data}`。変化があったときだけ publish する。
  - `rev` は topic をまたいだ単一のグローバルな単調増加カウンタ。接続時は購読 topic の snapshot を `rev` 順に送る。`Last-Event-ID` で再開すると、購読 topic のうち `rev > Last-Event-ID` のイベントをリングバッファから再送する。id がバッファより古ければ snapshot を送り直す。
- **環境変数**：`CAD_ADDR`（既定 `127.0.0.1:7878`）、`CAD_TOKEN`（`/healthz` 以外の全エンドポイントで `Authorization: Bearer <token>` を要求する。loopback 以外に bind するのに未設定なら起動を拒否する）、`CAD_POLICY`、`CAD_SLOTS`。
- **Worker の状態は `cad` が provider をポーリングして集める（pull）**。Mac はクラウドの Worker からの inbound 接続を受けない。
- **レビュー方針**：merge の条件は「実装者以外の独立した AI レビュアーの承認 + CI green（CI が設定されている場合）」。レビュアーは優先順位リストで持ち、quota が切れたら自動で次にフォールバックする。従来の「merge には Codex レビューが必須」というルールはこの ADR で置き換える（ハーネスのルールは PR #2 で更新）。

## 検討して却下した案
| 案 | 却下理由 |
|---|---|
| 既存 OSS を採用する | policy・capacity・クラウド横断の workers・quota の4つを全部満たすものが無い。node_exporter / Glances は capacity だけ。Claudexor は quota だけで、しかもハーネスを置き換える。mission-control はタスクボードを持ち ADR-0002 と衝突。Consul / etcd / Nomad / SkyPilot / dstack は重く部分的で、Consul / Nomad は BUSL |
| 値を毎回プロンプトで調べさせる | 結果が揺れて決定的でない。検証できない |
| TypeScript で書く | 単一バイナリで Worker に配れない。常駐メモリが大きい |
| Push に WebSocket を使う | 一方向で足りる。SSE なら標準ライブラリだけで書ける |
| Push に NATS / Redis を使う | 依存が増える |
| Worker から Mac へ状態を push させる | Mac が inbound 接続を受けることになる |
| `cad` にタスク状態も持たせる | キューの自作になり ADR-0002 と衝突する |

## 影響
- 良い影響：slots やレビュアーが決定的に決まり、検証できる。Worker を任意のクラウドへ出せて Mac の資源が空く。Mac は inbound を受けない。quota 切れで手動切り替えが要らない。タスク管理は内蔵 Task list のままで二重管理が起きない
- 受け入れたトレードオフ：repo に Go が入り、TS 優先の例外になる。インメモリなので `cad` を再起動すると収集済みの値（quota の履歴など）が消え、再収集まで古い／空の値になる。provider ごとのポーリング実装が増える
- 再検討する条件：Worker の構成を長期運用として固めるとき。4つを満たす OSS が出てきたとき。再起動で quota 状態が消えることが実害になったら永続化を検討する。Mac が inbound を受けてよい構成になったとき
