# orchd

Orchestrator を支える小さな CLI（Go、標準ライブラリのみ）。Issue を取る（pick）、task を「どの agent に・どの computer で」やらせるか決める（place）、その資源に渡す（dispatch）。[ADR-0011](../docs/decisions/0011-orchd-hatchet.md) で `cad` から分けた。判断のロジックは [ADR-0010](../docs/decisions/0010-placement.md)。

- `cad` を import しない。usage と capacity は起動中の cad から HTTP（`GET /v1/usage?ns=`、`GET /v1/capacity?ns=`）で読む
- 後で置き換え・削除する前提。**`rm -rf orchd tools/orchd` で消せる**（cad はそのまま動く）

## コマンド

```bash
tools/orchd place --class gate-heavy --self claude/a12e00a7   # [--ns default] [--mode <m>] [--exclude gce-spot,...]。--class 省略可
# {"agent":"claude/a12e00a7","computer":"claude-cloud","rule":0,"mode":"auto","modeSource":"default","cadAddr":"127.0.0.1:17878","runner":{"mode":"cloud","cmd":["claude","-p","--cloud","{session}","--output-format","json"],"stdin":"/app/orchd/wake.md","session":"<id>"},"reason":[],"defer_until":""}
# 配置できなかったとき（deferred / 合う rule なし / cad 未 ready）も同じキー全部を出す：rule:-1, runner:{}, reason に理由, defer_until はあれば RFC3339
tools/orchd mode set urgent [--ns default] [--by owner]      # mode show / mode clear も同じ --ns
tools/orchd show [rules|classes|runners]                     # 引数なし = 3 つとも
tools/orchd pick [--ns default]                              # ai Issue を 1 件取って wip を付ける
tools/orchd dispatch --placement "$json"                      # "$json" = place の出力（- で stdin）。runner.cmd の {key} を runner の同名 field で置換して実行（shell なし）、runner.stdin を stdin に流す
tools/orchd status --issue 7 [--pid 1234]                      # Closes #7 の PR と process の生死
tools/orchd vm status --instance worker-spot --zone z1 --project p1   # {instance, status}（REST、gcloud 不要）
```

`tools/orchd` は `orchd/bin/orchd` が無いか古ければ `go build` してから実行する（`bin/codingenv install` で `~/.local/bin/orchd` に入る）。

## exit code（`place`）

| code | 意味 | stdout |
|---|---|---|
| 0 | 配置できた、または配置できなかった（窓で全滅・合う rule なし・cad 未 ready）。runner が `{}` なら何も配置していない | `{agent, computer, rule, mode, modeSource, cadAddr, runner, reason, defer_until}`（全キー常に出る。配置できなかったときは `rule:-1`, `runner:{}`。窓で全滅／cad 未 ready は `defer_until` に RFC3339） |
| 1 | cad に届かない（接続失敗は 2 秒おきに 3 回再試行してから）・cad のエラー・policy ファイルが壊れている | なし（stderr に理由） |
| 2 | 入力が不正（class・mode 不明、`--self` / `--ns` の形式違い、不明なコマンド） | なし（stderr に usage） |

`pick` / `dispatch` / `status`：0 = 成功（pick は該当なしでも 0、`{none, reason}`。dispatch は runner が `{}` でも 0 で何も実行せず `{}` を出す）、1 = gh / git / 実行したコマンドの失敗、2 = 入力が不正（`--issue`・`--placement`、runner にキーはあるが cmd が無い・`{key}` の値が無い）。

## Orchestrator との関係

NS ごとに `claude code (orchestrator) → orchd pick → orchd place → orchd dispatch → claude code (orchestrator)`。dispatch は Orchestrator が background の Subagent の中で実行し、完了は Subagent の通知で受け取る（subagent の runner はその Subagent が作業する。process / cloud は haiku の監視役 Subagent が `orchd status` を繰り返して PR を待つ）。orchd のコマンドは必ずその NS の Orchestrator（Claude Code のセッション、手順は `skills/orchestrate`）が呼び、結果の JSON を stdout で Orchestrator に返す。次の一手は毎回 Orchestrator が決める（orchd は常駐しない・状態を持たない）。

| コマンド | すること | 返すもの |
|---|---|---|
| `pick` | `ai` ラベル付きで `wip`・`ai-failed` の無い open Issue のうち最古を取り、`wip` を付ける | `{issue:{n,title,body}, classes:[{name,criteria}]}` |
| `place` | 下記。Orchestrator が選んだ class で資源を決める | placement（`runner` を含む） |
| `status` | `Closes #n` の PR（open を優先）と、`--pid` があればその process が生きているか。gh を 1 回呼ぶだけ | `{issue, pr, state, running?}` |
| `vm status` | 1 台の worker VM の現在の状態（`instances.get` を 1 回。gcloud は使わない） | `{instance, status}` |
| `dispatch` | `runner.cmd`（argv）の `{key}` を runner の同名 field で置換して実行する（shell なし、mode で分岐しない）。`runner.stdin`（path）があれば stdin に流す。cmd が無い runner（subagent・vm）は exit 2 | 実行したコマンドの stdout |

- repo・path は `--repo` / `--path` > namespace の登録（`ORCHD_NAMESPACES` > `cad/config/namespaces.json` > その `.example.json`。orchd は読むだけ）> CWD の git toplevel と `gh repo view`
- cloud の session：`place` が runner.cmd に `{session}` があれば `CLAUDE_CLOUD_SESSION` > 登録の `cloudWorkerSession`（owner が `claude --cloud` で 1 度作る）を `runner.session` に入れる。無ければ dispatch が exit 2
- 未実装（後で）：完了処理（PR 確認・`wip` 解除・worktree の片付け）、実行記録、同時数の上限、cad の SSE による defer の再開

## vm runner（opencode の cloud worker）

`runners["opencode@gce-spot"]` / `["opencode@gce-std"]` = `{mode: vm, instance, zone, project, image, model}`。`instance` は 1 台の名前、または同じ種類（Spot/standard）の VM をカンマ区切りで並べたもの（1〜3 台、`deploy/gcp/create-worker.sh`）。VM と image は `deploy/gcp/README.md` の「opencode worker VM」（#53。owner が `create-worker.sh` で作るまでは describe が失敗して exit 5 → `--exclude` で local に落ちる）。VM は普段 `TERMINATED`（停止）で、task が終わると自分で止まる。vm runner には `cmd` が無い（VM を起動する orchd のサブコマンドが無い）ので、今は `dispatch` すると exit 2。

access token：GCE 上（cad-2 の cad image 内）は metadata server（`GCE_METADATA_HOST` で上書き可）の default service account、それ以外（Mac）は `gcloud auth print-access-token`。API の base は `ORCHD_COMPUTE_URL`（既定 `https://compute.googleapis.com/compute/v1`、テスト用）。権限：token の主体に worker VM への `compute.instances.get` が要る。

完了は `orchd status --issue <n>`（PR）で待つ。VM が `TERMINATED` なのに PR が無ければ、preempt か worker の失敗（`skills/orchestrate`）。VM の状態は `orchd vm status --instance <i> --zone <z> --project <p>` で見る（`instances.get` を 1 回、`{instance, status}` を出す。gcloud は使わない）。`gce-std` は cad の `config.json` の computers（費用の記録）には未登録（価格を確認してから足す）。

## MODE

MODE は place が使う rule の一覧を切り替える（[ADR-0010](../docs/decisions/0010-placement.md)）。

- `auto`（既定）：top-level `rules`。local は最後の手段（今は self: Claude cloud → opencode: gce-spot → gce-std → self: local → opencode: local）
- `urgent`：`modes.urgent.rules`。local の rule を先に、その後に auto の rule（self: local → opencode: local → self: Claude cloud → opencode: gce-spot → gce-std）。`modes.<name>.rules` を足せば mode を増やせる
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

場所は CWD に依存しない。app dir = `ORCHD_HOME` > 実行ファイルの実体（symlink を解決）が `<dir>/bin/orchd` で `<dir>/policy.json` があれば `<dir>` > CWD。

| 何 | 場所 |
|---|---|
| policy（git 管理） | `ORCHD_POLICY` > `<app>/policy.json`（= `orchd/policy.json`） |
| 状態（gitignore） | `ORCHD_STATE_DIR` > `<app>/state`（MODE は `state/mode/<ns>.json`・`_global.json`） |

`orchd/policy.json` のキー：`rules`・`modes`・`classes`（必須）・`runners`・`placement.reservePct`・`placement.staleUsage`（`pass` 既定 = stale な agent は usage 不明と同じく配置し reason に `<agent>: usage stale` を足す／`block` = その agent を飛ばす）・`cadAddr`、それに `agents`（パターン展開用）と `computers`（rule の computer 名の確認用、名前の配列）。`agents` と `computers` は `cad/config.json` の写し（orchd は cad のファイルを読まない）。agent を足したら両方を直す。

cad の場所：`CAD_ADDR`（明示すれば常に優先）> mode の `cadAddr`（`auto` は top-level の `cadAddr`、他は `modes.<m>.cadAddr`）> `127.0.0.1:7878`。seed は `urgent` = `127.0.0.1:7878`（手元の cad。urgent は local で動くので local の値を信じる）、`auto` = `127.0.0.1:17878`（常時動く VM の cad。IAP tunnel 経由）。`CAD_TOKEN` も読む。

旧 `.agent/policy.json` と `CAD_POLICY` は orchd では読まない。旧 MODE 状態（`~/.config/codingagentenv/mode/`）は引き継がないので、必要なら `orchd mode set` し直す。

## 開発

```bash
cd orchd && go vet ./... && go test -count=1 ./...
```
