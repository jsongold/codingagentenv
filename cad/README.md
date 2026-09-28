# cad

マシンに 1 つ常駐する小さなメタデータ daemon（Go、標準ライブラリのみ）。
agent（Claude / Codex / opencode のアカウント）の使用量などを定期収集して配信し、
task を「誰に・どこで」やらせるかを決定木で返す（`POST /v1/place`）。設計は [ADR-0008](../docs/decisions/0008-orchestrator-worker-cad.md) / [ADR-0010](../docs/decisions/0010-placement.md)。

## 起動

```bash
cd ~/projects/codingagentenv          # .agent/policy.json を読む
CAD_SLOTS=5 cad &                     # tools/cad が必要なら go build してから起動
cad get meta -ns default              # 動作確認
```

`cad` は `bin/codingenv install` で `~/.local/bin/cad`（→ `tools/cad`）に入る。

## コマンド

| コマンド | 内容 |
|---|---|
| `cad` | サーバーを起動 |
| `cad get meta\|<topic> -ns <ns>` | 起動中の cad からメタデータを JSON で取得（`-ns` 必須、無ければ exit 2） |
| `cad show [classes\|rules\|runners\|collect\|agents\|computers\|policy]` | policy のレコードを表示（引数なし = policy 全体） |
| `cad show cost --computer [<name>] (--hour\|--day\|--month) [--cpus N] [--mem GiB]` | 1 台を期間中ずっと動かした場合の料金（month=730h、既定 shape は 2 cpu/8GiB）。`<name>` 省略で全 computer、`local` は常に 0 |
| `cad add agent <service>/<account>` | agent を登録（例 `claude/a12e00a7`） |
| `cad add computer <name> [--replace] [--file f.json \| -]` | computer を登録 |
| `cad rm agent <agent> \| computer <name>` | 削除 |

書き込みは一時ファイル → rename。検証に失敗したらファイルは変えない。起動中の cad は mtime を見て自動で読み直す（再起動不要）。

## HTTP API

既定 `127.0.0.1:7878`。`CAD_TOKEN` を設定すると `/healthz` 以外は `Authorization: Bearer <token>` が必要。

| メソッド・パス | 内容 |
|---|---|
| `GET /healthz` | 死活確認 |
| `GET /v1/meta?ns=<ns>` | 全 topic |
| `GET /v1/<topic>?ns=<ns>` | 1 topic（`policy` / `capacity` / `workers` / `usage` など） |
| `GET /v1/events?topics=a,b` | SSE。値が変わった topic だけ push |
| `POST /v1/place?ns=<ns>` | 配置を返す（下記） |
| `POST /v1/quota/<reviewer>` | レビュアーの quota を記録 |

## 配置（`POST /v1/place`）

配置は分類問題として 2 段で解く。

1. **task → class**: Orchestrator（LLM）が `classes[].criteria` を見て決める
2. **(class, metadata) → (agent, computer)**: cad が `rules` を上から評価し、最初に合ったものを返す

```bash
curl -s -X POST 'localhost:7878/v1/place?ns=default' \
  -d '{"class":"gate-heavy","self":"claude/a12e00a7"}'
# {"agent":"claude/a12e00a7","computer":"local","rule":0,"reason":null,"runner":{"mode":"subagent"}}
```

- `self` = 呼び出した Orchestrator 自身の Claude アカウント（`CLAUDE_CONFIG_DIR` の basename）。rule の `agent: "self"` に一致する
- 各 rule の agent は、usage の窓（5h / 7d）が `used% + classes[class].estPct ≤ 100 - reservePct` のときだけ使える。値が無い窓・取得できない agent は通す
- `runner.mode` が `subagent` の agent は self のときだけ、runner が無い agent は使わない
- 結果: `200`（agent, computer, rule, runner）/ `409`（窓で全滅、`defer_until` = 一番早く空く時刻）/ `422`（合う rule なし）/ `400`（class 不明、ns なし）

## 設定（`.agent/policy.json`）

| キー | 内容 |
|---|---|
| `agents` | 使う agent（`<service>/<account>`。account は aienv の store id） |
| `rules` | 決定木。`{"agent": "self" \| パターン, "computer": "local", "class": [..任意..]}` を上から評価 |
| `classes` | class ごとの `criteria`（分類基準）と `estPct`（1 task の消費見込み %）。**必須** |
| `runners` | service ごとの起動方法。`{"mode":"subagent"}` / `{"mode":"process","cmd":"opencode run --model {model}","model":"..."}` |
| `collect.usage.every` | usage の収集間隔（既定 60s） |
| `placement.reservePct` | Orchestrator 用に残す窓の % |
| `computers` | computer の属性（今の place では local 以外未使用） |

壊れた policy は読み込まず、直前の正しいものを使い続ける（ログに理由を出す）。

## 収集（collector）

| topic | 取り方 | 間隔 |
|---|---|---|
| `usage` | claude: `CLAUDE_CONFIG_DIR=~/.aienv/.store/<id> claude -p "/usage"` → `<store>/.claude.json` の `cachedUsageUtilization`。codex: `CODEX_HOME=~/.aienv/.store/<id> codex app-server` の `account/rateLimits/read`。どちらもリクエストを消費しない | `collect.usage.every` |
| `capacity` | 空きメモリ・CPU・slots（`CAD_SLOTS` で固定上限） | 2s |
| `workers` | `agent/providers/<name>/list` の出力 | 2s |
| `policy` | policy ファイル | 2s |

opencode の usage はまだ収集しない（`usage unknown` として通す）。

## 環境変数

| 変数 | 内容 |
|---|---|
| `CAD_ADDR` | listen アドレス（既定 `127.0.0.1:7878`） |
| `CAD_TOKEN` | API token。loopback 以外で listen するときは必須 |
| `CAD_POLICY` | policy ファイル（既定 `./.agent/policy.json`） |
| `CAD_SLOTS` | local の slots を固定（1 slot = 1 workspace） |
| `CAD_USAGE_EVERY` | usage 収集間隔。policy より優先 |
| `CAD_CLAUDE_BIN` / `CAD_CODEX_BIN` | CLI の場所（既定は aienv の shim を避けて探す） |
| `CAD_PROVIDERS_DIR` | provider スクリプトの場所（既定 `agent/providers`） |

## 開発

```bash
cd cad && go vet ./... && go test -count=1 ./...
```
