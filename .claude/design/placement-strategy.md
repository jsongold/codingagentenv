# Placement strategy（provider 選択）設計メモ

日付 2026-09-27。未コミットの scratch。前提: ADR-0008 / 0009、`.agent/policy.json`、`cad/*.go`、handoff dev-env-upgrade。
検証済みの事実は「(verified: URL)」、未確認は「(unverified)」を付ける。

## 1. 何がスループットを縛るか → 何を最適化するか

| 制約 | 実態 | 配置で動かせるか |
|---|---|---|
| サブスク usage（5h / 7d 窓） | Orchestrator・全 Worker・`claude-opus` レビュアーが**1つの窓を共有**。Worker をどこに置いても消費量は同じ | 置き場所では減らない。**同時数（admission）と無駄撃ち（preemption 再走・timeout）** でしか守れない |
| Mac の RAM | 空き ~3GB → `slots = floor((3000-2048)/1500) = 0`。local は実質「小さい task 専用」 | Worker を外に出せば消える。最適化対象ではなく **local を候補から外す条件** |
| cold start | local 数秒 / sandbox 数秒 / Cloud Run 10〜30 秒 / GCE 起動+pull 1〜2 分（いずれも推定、unverified） | task 20〜45 分に対し 2〜7%。**light-edit と urgent だけ**で効く |
| gate のメモリ | Postgres+pytest+mypy で 1.5〜4GB | cloud では contention でなく金額。1 task ≈ $0.03〜0.13（§5）。**縛りではない** |
| レビュー quota | codex-bot 切れ → codex-local → claude-opus（サブスクを食う） | 配置ではなく WIP 上限。Worker 数 > レビュー処理量なら PR が溜まるだけ |

**結論**: 最適化するのは「サブスク窓 1% あたりの merge 済み PR 数」。制約は (a) サブスク窓の残量、(b) WIP ≤ レビュー処理量、(c) `maxCostUSD`。cold start と cloud $ は tie-break。
`local-first` を既定にすると slots=0 の Mac では全部詰まるので既定にしない。`biggest` は resources が spec にあるので不要。

## 2. 二段構成: 分類（Claude）→ 配置（決定的）

Owner 質問「配置アルゴリズムも classifier か？」→ **違う**。分類は Orchestrator（Claude）が spec を書くときに 1 回だけ行い、
配置は `dev-dispatch` が class + cad の値から表引きで決める（判断しない。同じ入力なら同じ出力）。

### 2a. 分類（Orchestrator が spec に書く）

spec に `class` を足す（`placement.class`）。1 task に 1 class。判断材料は ChangeGraph node と `done[]`。

| class | シグナル（Orchestrator が見るもの） | 既定 resources / timeout | 既定 strategy |
|---|---|---|---|
| `light-edit` | files が docs/config/1〜2 ファイル、`done[]` が lint・vet のみで 2 分未満 | 512MB / 1 cpu / 15 min | `fast` |
| `gate-heavy` | `done[]` に pytest / go test / tsc / mypy、DB なし | 2048MB / 2 cpu / 45 min | `cheap` |
| `needs-db` | `done[]` に testdb / psql / docker / DATABASE_URL | 4096MB / 2 cpu / 45 min | `cheap`、`allow` を DB 実行可能 provider に限定（§6 Q2） |
| `long-running` | timeoutMin > 45（大きい migration、E2E） | spec の値 | `safe`（preemptible 除外） |
| `urgent` | owner が結果を待っている（対話中に頼まれた） | class 相当 | `fast` |
| `retry` | 前回 preemption / 消失で pending に戻した task（Task 説明欄に理由あり） | 同じ | `safe` |

Orchestrator は class を決めた後に resources / timeout / strategy を**上書きしてよい**が、書かなければ表の既定を `dev-dispatch` が埋める。

### 2b. 配置（dev-dispatch。決定的）

**入力**
- spec: `class`, `resources{memoryMB,cpus,diskGB,timeoutMin}`, `placement{allow[],maxCostUSD,strategy,order[]?,provider?}`
- cad `GET /v1/capacity`（local slots）、`/v1/workers`（provider ごとの稼働数）、`/v1/policy`
- cad `GET /v1/pricing`（新 topic。`agent/providers/<name>/price` の出力: `{provider, vcpuHour, gibHour, minBillSec, preemptible, coldStartSecP50, caps{docker,db}, maxMemMB, maxCpus, maxMin}`）
- cad `GET /v1/usage`（新 topic。サブスク窓: `{fiveHour{usedPct,resetsAt}, sevenDay{usedPct,resetsAt}, collectedAt}`、出所は §4）
- policy に足す値: `placement.reservePct`（Orchestrator 用の残し、例 15）、`placement.estPct{class}`（1 task が 5h 窓を食う%の見込み。初期値は仮置き、§2d で更新）、`placement.maxWorkers`

**手順（疑似コード）**
```
place(spec, cad):
  # 0. admission（サブスク窓）。落ちたら exit 3 + stdout {"defer_until": resetsAt, "reason": ...}
  running = len(workers where state in (starting, running))
  est     = policy.placement.estPct[spec.class]
  if usage.fiveHour.usedPct + running*est + est > 100 - reservePct: defer(fiveHour.resetsAt)
  weekPace = 100 * elapsedFractionOfWeek(usage.sevenDay.resetsAt)   # 均等消費ペース
  if usage.sevenDay.usedPct + est > weekPace + policy.placement.weekSlackPct: defer(sevenDay.resetsAt)
  if running >= policy.placement.maxWorkers: defer(now + 5min)

  # 1. 明示指定
  if spec.placement.provider: return it if feasible(it) else exit 2

  # 2. 候補 = allow ∩ policy.providers.allowed、feasible で絞る
  feasible(p):
    price.maxMemMB >= memoryMB and maxCpus >= cpus and maxMin >= timeoutMin
    and (class != needs-db or caps.db)
    and cost(p) <= maxCostUSD            # cost = (vcpuHour*cpus + gibHour*memoryMB/1024) * max(timeoutMin, minBillSec/60) / 60
    and (p != local or capacity.slots >= 1)   # 「稼働 0 なら 1 本」は agent-gate の例外。Worker には適用しない
    and (strategy != safe or not price.preemptible)

  # 3. strategy で並べる（全部 total order。同値は tie-break 列で解決）
  cheap : key = (cost, preemptible, coldStartSecP50, name)
  fast  : key = (coldStartSecP50, cost, name)
  safe  : key = (cost, coldStartSecP50, name)             # preemptible は feasible で既に除外
  ranked: key = index in spec.placement.order（order 外は末尾、名前順）
  return min(feasible, key)   # 空なら exit 2（ADR-0009: 起動せず非 0）
```
tie-break の最後は必ず provider 名の辞書順 → 同じ入力で同じ答え。

**admission と Task list**: defer は `dev-dispatch` が非 0 で返すだけ。**待たない・キューを持たない**（ADR-0002）。Orchestrator は Task を pending のままにし、説明欄に `DEFER: until <resetsAt> (<reason>)` を追記して次のターン（または `/loop`）で再 dispatch する。urgent は Orchestrator が先に dispatch するだけで、admission は素通りしない（Orchestrator 自身の窓を守るため）。

**preemption / retry**: 自動リトライなし（ADR-0009）。`result=ok` 無しで `stopped/unknown` → Orchestrator が理由を書いて pending に戻し、class を `retry` に付け替えて再 dispatch（→ `safe`、push 済み branch から再開）。同じ spec id で 2 回 preemption されたら `allow` から preemptible を外して spec を書き直す。

### 2c. 最終 strategy セット（各 1 例）
- `cheap`（既定）: gate-heavy の cad workers collector 実装。候補 gce-spot $0.03 / cloud-run $0.12 / e2b $0.12 / local slots=0 → **gce-spot**。
- `fast`: light-edit「ADR の typo 修正 + `go vet`」。local slots=1 なら **local**（cold start 数秒、$0）。slots=0 なら sandbox 系。
- `safe`: long-running の E2E 60 分。preemptible 除外 → **cloud-run-jobs**（168h 上限、preemption なし）。
- `ranked` + `order:[e2b, cloud-run-jobs]`: owner が sandbox の挙動を試したい task。e2b が feasible ならそれ。
- `provider: local`: `.claude/` 直下や Keychain を触るなど Mac でしか意味がない task。

旧案の `local-first` は `cheap`（local は price 0 なので slots があれば自然に勝つ）、`quickest-start` は `fast`、`biggest` は削除。

### 2d. 後で学習する（今は ML なし、数値の更新だけ）
`dev-dispatch` が start 時、Orchestrator が VERIFIED 時に `.claude/runs/<specId>.jsonl`（未コミット、spec と同じ扱い）へ 1 行ずつ追記:
`{specId, class, provider, strategy, startedAt, coldStartSec, wallMin, costUSD, sub5hBefore, sub5hAfter, sub7dBefore, sub7dAfter, result, preempted, reviewer}`。
`sub*` は cad `/v1/usage` のスナップショット。これで (class, provider) ごとに中央値が取れる: 消費%（→ `estPct[class]`）、coldStart（→ `price` の `coldStartSecP50`）、preemption 率（→ `retry` を `safe` にする閾値）、成功率。
更新先は policy と price スクリプトの定数のみ。dispatch 時の判断ロジックは変えない（決定性を保つ）。

## 4. サブスク usage の出所と cad での収集

**verified**（公式 docs）:
- status line の stdin JSON に `rate_limits.five_hour.used_percentage / resets_at`、`rate_limits.seven_day.*` がある。Pro/Max の claude.ai ログイン時のみ、セッション最初の API 応答後から。`resets_at` 到達で再実行、`refreshInterval` で idle 中も再実行できる。 https://code.claude.com/docs/en/statusline
- `/usage` はプラン使用量バーを表示するが、対話コマンドでプログラムから読めない。裏の usage endpoint は rate limit されうる。 https://code.claude.com/docs/en/costs
- `claude auth status --json` は `subscriptionType: "max"` 等を返すが usage は無い（この Mac で実行、v2.1.283）。`claude --help` に usage/status サブコマンドは無い。
- Worker の認証: `claude setup-token` → `CLAUDE_CODE_OAUTH_TOKEN`（1 年、Pro/Max/Team/Enterprise、CI 向け、model 要求のみ）。`--bare` はこの変数を読まない → Worker で `--bare` は使えない。 https://code.claude.com/docs/en/authentication
- 規約: OAuth 資格情報は Claude Code / Anthropic 純正アプリ専用。自分のサブスクで未改変の Claude Code をホスト先で動かすのは可。**OAuth token で他の API を叩くのは不可**。 https://code.claude.com/docs/en/legal-and-compliance

**採用**: Orchestrator セッションの `statusLine` command を「stdin を `~/.cache/cad/rate_limits.json` へ書いてから表示」にし（`refreshInterval` 60s）、cad の collector が policy と同じ mtime 監視でそれを読み `usage` topic に出す。`~/.claude/` は直接編集しないので、statusLine 設定は repo の正本 → `codingenv install` で展開。
**不採用**: 非公開 `GET api.anthropic.com/api/oauth/usage`（コミュニティ報告のみ。上記規約に反し、非 Claude Code からの要求は拒否されると報じられている）。

**unverified**:
- status line の `rate_limits` が他マシン（Worker）の消費を含むか。窓はアカウント単位なので含むはずだが、最初の e2e で before/after を比べて確認する。
- `-p`（headless）の `result` メッセージに rate limit は載らない（docs に記載なし）→ Worker 側からは取らない。
- `--max-budget-usd` はサブスクでも「見積り $」で止まるか（暴走ガードとして使う前に 1 回試す）。
- spec 例の `secrets: [ANTHROPIC_API_KEY]` はサブスク運用なら `CLAUDE_CODE_OAUTH_TOKEN` に直す（ADR-0009 follow-up）。

## 5. 最初の e2e（Worker 1 本、task 1 件）の provider

前提: 656MB の Linux image、2 vCPU / 2〜4GiB、45 分。この Mac に `gcloud`（認証済、project suggestorder-dev）と `docker` はある。`e2b` / `daytona` CLI は無い。

| | Cloud Run Jobs | GCE Spot (e2-standard-2) | E2B (Pro) |
|---|---|---|---|
| cold start | 「Starts immediately」+ image pull。10〜30 秒（推定） | VM 起動 + pull。1〜2 分（推定） | 数秒（公称は unverified） |
| 単価 | $0.000018/vCPU-s（Tier1、verified snippet）、GiB-s ≈ $0.000002（unverified）、**最低 1 分課金** | $0.0402/h（第三者集計、2026-09-27） | $0.000014/vCPU-s + $0.0000045/GiB-s、Pro $150/月 |
| 45 分 2vCPU/4GiB | ≈ $0.12 | ≈ $0.03 | ≈ $0.12 + 月額 |
| Docker image | Artifact Registry / Docker Hub / GHCR から直接 | COS + `docker run`、任意 | テンプレートは `fromBaseImage()`（Dockerfile 直はunverified） |
| secrets | Secret Manager → env（`--set-secrets`） | Secret Manager + metadata/SA | 未確認 |
| preemption | なし。task timeout 最大 168h、8 vCPU / 32GiB | あり。通知 **既定 0 秒**（120 秒は Preview）、価格は日次で変動 | なし。Pro 24h / Hobby 1h |
| 実行時 override | `--update-env-vars` `--args` `--task-timeout`（REPO/BRANCH/BASE 注入に直結） | metadata に書く | SDK 経由 |
| 落とし穴 | job の `--max-retries` 既定 **3** → `0` にする（ADR-0009 自動リトライなし）。Docker-in-Docker 不可 → needs-db は image に Postgres 同梱が要る | 結果を metadata に書く配管、preemption 対応、`stop` 漏れの VM が課金 | 新規契約・CLI 導入・template ビルドの学習 |
| 所要（1 人） | AR push + Secret 2 個 + `gcloud run jobs create`。半日以内 | 1 日 | 1 日（アカウント含む） |

出典: Cloud Run 料金 https://cloud.google.com/run/pricing / 上限 https://docs.cloud.google.com/run/quotas / retries https://docs.cloud.google.com/run/docs/configuring/max-retries / 実行 override https://docs.cloud.google.com/run/docs/execute/jobs / secrets https://docs.cloud.google.com/run/docs/configuring/jobs/secrets / registries https://docs.cloud.google.com/run/docs/deploying / Spot https://docs.cloud.google.com/compute/docs/instances/spot / Spot 価格（第三者） https://gcloud-compute.com/e2-standard-2.html / E2B https://e2b.dev/pricing / Daytona（参考: $0.0504/vCPU/h + $0.0162/GiB/h、$200 無料枠、Dockerfile から snapshot 可） https://www.daytona.io/pricing

**推奨: Cloud Run Jobs**。理由: (1) 既にある `gcloud` 認証と Docker image をそのまま使え、新しい契約・CLI が要らない。(2) preemption が無いので、最初の e2e から「結果の伝え方」「retry」の配管を切り離せる。(3) `start` = `gcloud run jobs execute --update-env-vars --args`、`list` = `executions list --format json`（実行状態と exit がそのまま `labels.result` の材料）、`stop` = `executions cancel` と 3 本が gcloud だけで書ける。(4) 1 task ≈ $0.12 で `maxCostUSD 1.5` に余裕。
GCE Spot は 2 番目（`cheap` の主力になるが、0 秒通知の preemption を最初に踏むと原因が切り分けられない）。E2B/Daytona は `fast` 用に後から足す。

## 6. Owner への質問（≤5）
1. Worker の認証は Max サブスク（`setup-token`、窓を共有）で固定か。窓が枯れたら API key（別枠・従量）へ溢れさせる hybrid を許すか。
2. `needs-db` を Cloud Run で回すために Worker image に Postgres を同梱するか、それとも `needs-db` は docker が動く provider（local / GCE）限定にするか。
3. admission の初期値: `reservePct`（Orchestrator 用の残し）15、`estPct` を light-edit 1 / gate-heavy 4 / needs-db 6 / long-running 8（すべて仮）で始めてよいか。
4. 最初の e2e の GCP project / region（今の gcloud は suggestorder-dev。専用 project を作るか）。
5. 走行ログ `.claude/runs/*.jsonl` は spec と同じく未コミットでよいか（機械を移ると消える）。
