# orchd

task を「どの agent に・どの computer で」やらせるかだけを決める CLI（Go、標準ライブラリのみ）。[ADR-0011](../docs/decisions/0011-orchd-hatchet.md) で `cad` から分けた。判断のロジックは [ADR-0010](../docs/decisions/0010-placement.md)。

- `cad` を import しない。usage と capacity は起動中の cad から HTTP（`GET /v1/usage?ns=`、`GET /v1/capacity?ns=`）で読む
- 後で置き換え・削除する前提。**`rm -rf orchd tools/orchd` で消せる**（cad はそのまま動く）

## コマンド

```bash
tools/orchd place --class gate-heavy --self claude/a12e00a7   # [--ns default] [--mode <m>]
# {"agent":"claude/a12e00a7","computer":"claude-cloud","rule":0,"reason":null,"runner":{"mode":"cloud","cmd":"claude --cloud"},"mode":"auto","modeSource":"default"}
tools/orchd mode set urgent [--ns default] [--by owner]      # mode show / mode clear も同じ --ns
tools/orchd show [rules|classes|runners]                     # 引数なし = 3 つとも
```

`tools/orchd` は `orchd/bin/orchd` が無いか古ければ `go build` してから実行する（`bin/codingenv install` で `~/.local/bin/orchd` に入る）。

## exit code（`place`）

| code | 意味 | stdout |
|---|---|---|
| 0 | 配置できた | `{agent, computer, rule, reason, runner, mode, modeSource}` |
| 1 | cad に届かない・cad のエラー・policy ファイルが壊れている | なし（stderr に理由） |
| 2 | 入力が不正（class・mode 不明、`--self` / `--ns` の形式違い、不明なコマンド） | なし（stderr に usage） |
| 3 | 窓で全滅（旧 409）。`defer_until` = 最も早く空く時刻 | `{defer_until, reason}` |
| 4 | 合う rule なし（旧 422。local の slot 無しなど） | `{reason}` |

## MODE

MODE は place が使う rule の一覧を切り替える（[ADR-0010](../docs/decisions/0010-placement.md)）。

- `auto`（既定）：top-level `rules`。local は最後の手段（今は self: Claude cloud → local、opencode: local）
- `urgent`：`modes.urgent.rules`。local の rule を先に、その後に auto の rule。`modes.<name>.rules` を足せば mode を増やせる
- `rule` の番号はその mode の一覧での位置。place の出力に `mode` と `modeSource`（`flag` / `file:ns` / `file:global` / `env` / `default`＝何も指定なし）が付く
- 優先順位：`--mode` > `$ORCHD_STATE_DIR/mode/<ns>.json` > `$ORCHD_STATE_DIR/mode/_global.json` > `ORCHD_MODE` > `auto`。不明な mode 名は exit 2
- ファイルに残すので context の圧縮やセッションの再起動で消えない（中身 `{"mode","since","by"}`、temp + rename で書く）
- runner は `runners["<service>@<computer>"]` を先に、無ければ `runners["<service>"]` を使う（例：`claude@claude-cloud` = `{mode: cloud, cmd: "claude --cloud"}`）

会話での流れ：

1. owner が「MODE=URGENT で処理して」と言う → Orchestrator が `orchd mode set urgent --ns <ns>`（全 ns なら `--ns` なし）
2. 以降の `orchd place --ns <ns>` は urgent（圧縮・再起動後も）。確認は `orchd mode show --ns <ns>`
3. owner が「解除」と言う → `orchd mode clear --ns <ns>`

セッション全体を起動時に決めるなら `ORCHD_MODE=urgent claude`（ファイルがあればそちらが優先）。

## 設定

MODE の状態：`ORCHD_STATE_DIR`（既定 `~/.config/codingagentenv`）。policy ファイル：`ORCHD_POLICY` > `CAD_POLICY` > `./.agent/policy.json`。読むキーは `rules`・`modes`・`classes`（必須）・`runners`・`placement.reservePct`・`agents`（パターン展開用）と、rule の computer 名の確認に `computers` の名前だけ。cad：`CAD_ADDR`（既定 `127.0.0.1:7878`）、`CAD_TOKEN`。

## 開発

```bash
cd orchd && go vet ./... && go test -count=1 ./...
```
