# orchd

task を「どの agent に・どの computer で」やらせるか決める（place）小さな CLI（Go、標準ライブラリのみ）。決めた runner を実行する（dispatch）。[ADR-0011](../docs/decisions/0011-orchd-hatchet.md) で `cad` から分けた。判断のロジックは [ADR-0010](../docs/decisions/0010-placement.md)。

- `cad` を import しない。usage と capacity は起動中の cad から HTTP（`GET /v1/usage?ns=`、`GET /v1/capacity?ns=`）で読む
- 後で置き換え・削除する前提。**`rm -rf orchd tools/orchd` で消せる**（cad はそのまま動く）

## コマンド

```bash
tools/orchd place --class gate-heavy --self claude/a12e00a7   # [--ns default] [--mode <m>] [--exclude gce-spot,...]。--class 省略可
# {"agent":"claude/a12e00a7","computer":"claude-cloud","rule":0,"mode":"auto","modeSource":"default","cadAddr":"127.0.0.1:17878","runner":{"mode":"cloud","cmd":["claude","-p","--cloud","{session}","--output-format","json"],"stdin":"/app/orchd/wake.md","session":"<id>"},"reason":[],"defer_until":""}
# 配置できなかったとき（deferred / 合う rule なし / cad 未 ready）も同じキー全部を出す：rule:-1, runner:{}, reason に理由, defer_until はあれば RFC3339
tools/orchd show [rules|classes|runners]                     # 引数なし = 3 つとも
tools/orchd dispatch --placement "$json"                      # "$json" = place の出力（- で stdin）。runner.cmd の {key} を runner の同名 field で置換して実行（shell なし）、runner.stdin を stdin に流す
```

`tools/orchd` は `orchd/bin/orchd` が無いか古ければ `go build` してから実行する（`bin/codingenv install` で `~/.local/bin/orchd` に入る）。

## exit code（`place`）

| code | 意味 | stdout |
|---|---|---|
| 0 | 配置できた、または配置できなかった（窓で全滅・合う rule なし・cad 未 ready）。runner が `{}` なら何も配置していない | `{agent, computer, rule, mode, modeSource, cadAddr, runner, reason, defer_until}`（全キー常に出る。配置できなかったときは `rule:-1`, `runner:{}`。窓で全滅／cad 未 ready は `defer_until` に RFC3339） |
| 1 | cad に届かない（接続失敗は 2 秒おきに 3 回再試行してから）・cad のエラー・policy ファイルが壊れている | なし（stderr に理由） |
| 2 | 入力が不正（class・mode 不明、`--self` / `--ns` の形式違い、不明なコマンド） | なし（stderr に usage） |

`dispatch`：0 = 成功（runner が `{}` でも 0 で何も実行せず `{}` を出す）、1 = 実行したコマンドの失敗、2 = 入力が不正（`--placement`、runner にキーはあるが cmd が無い・`{key}` の値が無い）。

- cloud の session：`place` が runner.cmd に `{session}` があれば `CLAUDE_CLOUD_SESSION` > namespace 登録（`ORCHD_NAMESPACES` > `cad/config/namespaces.json`）の `cloudWorkerSession`（owner が `claude --cloud` で 1 度作る）を `runner.session` に入れる。無ければ dispatch が exit 2

## vm runner（opencode の cloud worker）

`runners["opencode@gce-spot"]` / `["opencode@gce-std"]` = `{mode: vm, instance, zone, project, image, model}`（VM は `deploy/gcp/create-worker.sh` で作る）。cmd を持たないので `dispatch` は exit 2（VM 側で直接動かす想定）。

## MODE

MODE は place が使う rule の一覧を切り替える（[ADR-0010](../docs/decisions/0010-placement.md)）。

- `auto`（既定）：top-level `rules`。local は最後の手段（今は self: Claude cloud → opencode: gce-spot → gce-std → self: local → opencode: local）
- `urgent`：`modes.urgent.rules`。local の rule を先に、その後に auto の rule（self: local → opencode: local → self: Claude cloud → opencode: gce-spot → gce-std）。`modes.<name>.rules` を足せば mode を増やせる
- `rule` の番号はその mode の一覧での位置。place の出力に `mode` と `modeSource`（`flag` / `env` / `default`＝何も指定なし）が付く
- 優先順位：`--mode` > `ORCHD_MODE` > `auto`。不明な mode 名は exit 2
- runner は `runners["<service>@<computer>"]` を先に、無ければ `runners["<service>"]` を使う（例：`claude@claude-cloud` = `{mode: cloud, cmd: "claude --cloud"}`）

セッション全体を urgent にするなら `ORCHD_MODE=urgent claude`（`--mode` の方が優先）。

## 設定

場所は CWD に依存しない。app dir = `ORCHD_HOME` > 実行ファイルの実体（symlink を解決）が `<dir>/bin/orchd` で `<dir>/policy.json` があれば `<dir>` > CWD。

| 何 | 場所 |
|---|---|
| policy（git 管理） | `ORCHD_POLICY` > `<app>/policy.json`（= `orchd/policy.json`） |
| 状態（gitignore） | `ORCHD_STATE_DIR` > `<app>/state` |

`orchd/policy.json` のキー：`rules`・`modes`・`classes`（必須）・`runners`・`placement.reservePct`・`placement.staleUsage`（`pass` 既定 = stale な agent は usage 不明と同じく配置し reason に `<agent>: usage stale` を足す／`block` = その agent を飛ばす）・`cadAddr`、それに `agents`（パターン展開用）と `computers`（rule の computer 名の確認用、名前の配列）。`agents` と `computers` は `cad/config.json` の写し（orchd は cad のファイルを読まない）。agent を足したら両方を直す。

cad の場所：`CAD_ADDR`（明示すれば常に優先）> mode の `cadAddr`（`auto` は top-level の `cadAddr`、他は `modes.<m>.cadAddr`）> `127.0.0.1:7878`。seed は `urgent` = `127.0.0.1:7878`（手元の cad。urgent は local で動くので local の値を信じる）、`auto` = `127.0.0.1:17878`（常時動く VM の cad。IAP tunnel 経由）。`CAD_TOKEN` も読む。

旧 `.agent/policy.json` と `CAD_POLICY` は orchd では読まない。

## 開発

```bash
cd orchd && go vet ./... && go test -count=1 ./...
```
