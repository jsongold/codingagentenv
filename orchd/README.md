# orchd

task を「どの agent に・どの computer で」やらせるかだけを決める CLI（Go、標準ライブラリのみ）。[ADR-0011](../docs/decisions/0011-orchd-hatchet.md) で `cad` から分けた。判断のロジックは [ADR-0010](../docs/decisions/0010-placement.md)。

- `cad` を import しない。usage と capacity は起動中の cad から HTTP（`GET /v1/usage?ns=`、`GET /v1/capacity?ns=`）で読む
- 後で置き換え・削除する前提。**`rm -rf orchd tools/orchd` で消せる**（cad はそのまま動く）

## コマンド

```bash
tools/orchd place --class gate-heavy --self claude/a12e00a7   # [--ns default]
# {"agent":"claude/a12e00a7","computer":"local","rule":0,"reason":null,"runner":{"mode":"subagent"}}
tools/orchd show [rules|classes|runners]                     # 引数なし = 3 つとも
```

`tools/orchd` は `orchd/bin/orchd` が無いか古ければ `go build` してから実行する（`bin/codingenv install` で `~/.local/bin/orchd` に入る）。

## exit code（`place`）

| code | 意味 | stdout |
|---|---|---|
| 0 | 配置できた | `{agent, computer, rule, reason, runner}` |
| 1 | cad に届かない・cad のエラー・policy ファイルが壊れている | なし（stderr に理由） |
| 2 | 入力が不正（class 不明、`--self` / `--ns` の形式違い、不明なコマンド） | なし（stderr に usage） |
| 3 | 窓で全滅（旧 409）。`defer_until` = 最も早く空く時刻 | `{defer_until, reason}` |
| 4 | 合う rule なし（旧 422。local の slot 無しなど） | `{reason}` |

## 設定

policy ファイル：`ORCHD_POLICY` > `CAD_POLICY` > `./.agent/policy.json`。読むキーは `rules`・`classes`（必須）・`runners`・`placement.reservePct`・`agents`（パターン展開用）と、rule の computer 名の確認に `computers` の名前だけ。cad：`CAD_ADDR`（既定 `127.0.0.1:7878`）、`CAD_TOKEN`。

## 開発

```bash
cd orchd && go vet ./... && go test -count=1 ./...
```
