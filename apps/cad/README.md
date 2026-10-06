# cad

マシンに 1 つ常駐する小さなメタデータ daemon（Go、標準ライブラリのみ）。
agent（Claude / Codex / opencode のアカウント）の使用量などを定期収集して配信する。
task を「誰に・どこで」やらせるかは [orchd](../orchd/README.md) が cad の値を HTTP で読んで決める。設計は [#95](https://github.com/jsongold/codingagentenv/issues/95) / [#97](https://github.com/jsongold/codingagentenv/issues/97) / [#98](https://github.com/jsongold/codingagentenv/issues/98)。

## 起動

```bash
CAD_SLOTS=5 cad &                     # どの CWD でも apps/cad/config.json を読む                     # tools/cad が必要なら go build してから起動
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
| `cad show usage [--json] [--local]` | agent ごとの subscription usage（5h/7d、resets、fetched）。既定は起動中の cad の usage topic を取得し、届かなければ in-process で収集（stderr に注記）。`--local` は常に in-process で収集。`--json` は usage map をそのまま出力 |
| `cad add agent <service>/<account>` | agent を登録（例 `claude/a12e00a7`） |
| `cad add computer <name> [--replace] [--file f.json \| -]` | computer を登録 |
| `cad rm agent <agent> \| computer <name>` | 削除 |

書き込みは一時ファイル → rename。検証に失敗したらファイルは変えない。起動中の cad は mtime を見て自動で読み直す（再起動不要）。

## HTTP API

既定 `127.0.0.1:7878`。`CAD_TOKEN` を設定すると `/healthz` 以外は `Authorization: Bearer <token>` が必要。

| メソッド・パス | 内容 |
|---|---|
| `GET /healthz` | 死活確認（liveness。応答できれば 200） |
| `GET /healthz?ready` | readiness。usage を持っていれば 200 `{"ready":true,"usage":"snapshot"\|"collected"}`、まだなら 503 `{"ready":false,"reason":...}`。token 不要 |
| `GET /v1/meta?ns=<ns>` | 全 topic |
| `GET /v1/<topic>?ns=<ns>` | 1 topic（`policy` / `capacity` / `workers` / `usage` など） |
| `GET /v1/events?topics=a,b` | SSE。値が変わった topic だけ push |
| `POST /v1/quota/<reviewer>` | レビュアーの quota を記録 |

## 設定

場所は CWD に依存しない。app dir = `CAD_HOME` > 実行ファイルの実体（symlink を解決）が `<dir>/bin/cad` で `<dir>/config.json` があれば `<dir>` > CWD。

| 何 | 場所 |
|---|---|
| 設定（git 管理） | `CAD_CONFIG` > `CAD_POLICY`（旧名、別名として有効）> `<app>/config.json`（= `apps/cad/config.json`）。無ければ built-in の既定 |
| namespace の登録（gitignore） | `<app>/config/namespaces.json`。例は `apps/cad/config/namespaces.example.json`（まだ cad は読まない） |

`apps/cad/config.json` のキー：

| キー | 内容 |
|---|---|
| `agents` | 使う agent（`<service>/<account>`。account は aienv の store id）。orchd の `policy.json` にも写しがある |
| `collect.usage.every` | usage の収集間隔（既定 60s） |
| `computers` | computer の属性（`cad show cost`） |
| `gate` / `review` / `providers` | capacity・レビュアー（`agent/ai-review.ts`）・workers の provider |

`cad add/rm` は cad の知らないキーもそのまま残す。rules / classes / runners / modes は `apps/orchd/policy.json`（[orchd/README.md](../orchd/README.md)）。

壊れた policy は読み込まず、直前の正しいものを使い続ける（ログに理由を出す）。

## 収集（collector）

| topic | 取り方 | 間隔 |
|---|---|---|
| `usage` | claude: `CLAUDE_CONFIG_DIR=~/.aienv/.store/<id> claude -p "/usage"` → `<store>/.claude.json` の `cachedUsageUtilization`。codex: `CODEX_HOME=~/.aienv/.store/<id> codex app-server` の `account/rateLimits/read`。どちらもリクエストを消費しない。opencode: `~/.aienv/.store/<id>/opencode/auth.json`（default は `$XDG_DATA_HOME/opencode/auth.json`）の key で `GET https://opencode.ai/zen/go/v1/usage`（非公開 endpoint）。rolling/weekly/monthly → `fiveHour`/`sevenDay`/`monthly`、status `rate-limited` の window は `rateLimited: true` | `collect.usage.every` |
| `capacity` | 空きメモリ・CPU・slots（`CAD_SLOTS` で固定上限） | 2s |
| `workers` | `agent/providers/<name>/list` の出力 | 2s |
| `policy` | policy ファイル | 2s |

`monthly` は opencode 以外 null。orchd place は `monthly` も 5h/7d と同じ規則で見て、`rateLimited` の window は使用率によらず除外する。

## 環境変数

| 変数 | 内容 |
|---|---|
| `CAD_ADDR` | listen アドレス（既定 `127.0.0.1:7878`） |
| `CAD_TOKEN` | API token。loopback 以外で listen するときは必須 |
| `CAD_HOME` | app dir（既定は実行ファイルの `bin/` の 1 つ上。上の「設定」） |
| `CAD_CONFIG`（別名 `CAD_POLICY`） | 設定ファイル（既定 `<app>/config.json`） |
| `CAD_SLOTS` | local の slots を固定（1 slot = 1 workspace） |
| `CAD_USAGE_EVERY` | usage 収集間隔。policy より優先 |
| `CAD_CLAUDE_BIN` / `CAD_CODEX_BIN` | CLI の場所（既定は aienv の shim を避けて探す） |
| `CAD_PROVIDERS_DIR` | provider スクリプトの場所（既定 `agent/providers`） |

## 開発

```bash
cd cad && go vet ./... && go test -count=1 ./...
```

## 再起動・入れ替え時の引き継ぎ（#98 追記）

usage を集めるたびに `<state>/usage-snapshot.json`（state = `CAD_STATE_DIR` > `<app>/state`。`CAD_HOME` があれば `<CAD_HOME>/state`。gitignore）へ一時ファイル → rename で書く。起動時に 24 時間以内の snapshot があれば、全 agent を `stale: true`（`fetchedAt` は元のまま）にしてすぐ `usage` topic に出し、ready になる。最初の本物の収集で上書きされる。
