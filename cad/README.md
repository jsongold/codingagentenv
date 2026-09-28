# cad

マシンに 1 つ常駐する小さなメタデータ daemon（Go、標準ライブラリのみ）。
agent（Claude / Codex / opencode のアカウント）の使用量などを定期収集して配信する。
task を「誰に・どこで」やらせるかは [orchd](../orchd/README.md) が cad の値を HTTP で読んで決める。設計は [ADR-0008](../docs/decisions/0008-orchestrator-worker-cad.md) / [ADR-0010](../docs/decisions/0010-placement.md) / [ADR-0011](../docs/decisions/0011-orchd-hatchet.md)。

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
| `cad show [collect\|agents\|computers\|policy]` | policy のレコードを表示（引数なし = policy 全体。rules / classes / runners は `orchd show`） |
| `cad show cost --computer [<name>] (--hour\|--day\|--month) [--cpus N] [--mem GiB]` | 1 台を期間中ずっと動かした場合の料金（month=730h、既定 shape は 2 cpu/8GiB）。`<name>` 省略で全 computer、`local` は常に 0 |
| `cad show --usage [--json] [--local]` | agent ごとの subscription usage（5h/7d、resets、fetched）。既定は起動中の cad の usage topic を取得し、届かなければ in-process で収集（stderr に注記）。`--local` は常に in-process で収集。`--json` は usage map をそのまま出力 |
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
| `POST /v1/quota/<reviewer>` | レビュアーの quota を記録 |

## 設定（`.agent/policy.json`）

| キー | 内容 |
|---|---|
| `agents` | 使う agent（`<service>/<account>`。account は aienv の store id） |
| `collect.usage.every` | usage の収集間隔（既定 60s） |
| `computers` | computer の属性（`cad show cost`） |
| `rules` / `classes` / `runners` / `placement` | orchd が読む（[orchd/README.md](../orchd/README.md)）。cad は検証せず、`cad add/rm` でもそのまま残す |

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
