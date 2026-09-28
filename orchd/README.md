# orchd

Orchestrator を支える小さな CLI（Go、標準ライブラリのみ）。Issue を取る（pick）、task を「どの agent に・どの computer で」やらせるか決める（place）、その資源に渡す（dispatch）。[ADR-0011](../docs/decisions/0011-orchd-hatchet.md) で `cad` から分けた。判断のロジックは [ADR-0010](../docs/decisions/0010-placement.md)。

- `cad` を import しない。usage と capacity は起動中の cad から HTTP（`GET /v1/usage?ns=`、`GET /v1/capacity?ns=`）で読む
- 後で置き換え・削除する前提。**`rm -rf orchd tools/orchd` で消せる**（cad はそのまま動く）

## コマンド

```bash
tools/orchd place --class gate-heavy --self claude/a12e00a7   # [--ns default] [--mode <m>] [--exclude gce-spot,...]
# {"agent":"claude/a12e00a7","computer":"claude-cloud","rule":0,"reason":null,"runner":{"mode":"cloud","cmd":"claude --cloud"},"mode":"auto","modeSource":"default","cadAddr":"127.0.0.1:17878"}
tools/orchd mode set urgent [--ns default] [--by owner]      # mode show / mode clear も同じ --ns
tools/orchd show [rules|classes|runners]                     # 引数なし = 3 つとも
tools/orchd pick [--ns default]                              # ai Issue を 1 件取って wip を付ける
tools/orchd dispatch --issue 7 --placement "$json" [--ns default]   # "$json" = place の出力。- で stdin
tools/orchd status --issue 7 [--pid 1234]                      # Closes #7 の PR と process の生死
```

`tools/orchd` は `orchd/bin/orchd` が無いか古ければ `go build` してから実行する（`bin/codingenv install` で `~/.local/bin/orchd` に入る）。

## exit code（`place`）

| code | 意味 | stdout |
|---|---|---|
| 0 | 配置できた | `{agent, computer, rule, reason, runner, mode, modeSource, cadAddr}` |
| 1 | cad に届かない（接続失敗は 2 秒おきに 3 回再試行してから）・cad のエラー・policy ファイルが壊れている | なし（stderr に理由） |
| 2 | 入力が不正（class・mode 不明、`--self` / `--ns` の形式違い、不明なコマンド） | なし（stderr に usage） |
| 3 | 窓で全滅（旧 409）。`defer_until` = 最も早く空く時刻。または cad が未 ready（`GET /healthz?ready` が 503。再起動直後など）で reason `cad not ready`、`defer_until` = 今 + 2 分 | `{defer_until, reason}` |
| 4 | 合う rule なし（旧 422。local の slot 無しなど） | `{reason}` |

`pick` / `dispatch` / `status`：0 = 成功（pick は該当なしでも 0、`{none, reason}`）、1 = gh / git / claude / `docker run` の失敗、2 = 入力が不正（`--issue`・`--placement`、cloud の session が無い）、5 = computer unavailable（dispatch の vm：VM が停止中でない〈別 task が使用中〉・start できない〈Spot の容量不足など〉・API の失敗）。5 のとき Orchestrator は `orchd place --exclude <その computer>` で置き直す（`--exclude` の computer の rule は reason `rule <i>: computer <c> excluded` で飛ばす。複数はカンマ区切り）。

## Orchestrator との関係

NS ごとに `claude code (orchestrator) → orchd pick → orchd place → orchd dispatch → claude code (orchestrator)`。dispatch は Orchestrator が background の Subagent の中で実行し、完了は Subagent の通知で受け取る（subagent の runner はその Subagent が作業する。process / cloud は haiku の監視役 Subagent が `orchd status` を繰り返して PR を待つ）。orchd のコマンドは必ずその NS の Orchestrator（Claude Code のセッション、手順は `skills/orchestrate`）が呼び、結果の JSON を stdout で Orchestrator に返す。次の一手は毎回 Orchestrator が決める（orchd は常駐しない・状態を持たない）。

| コマンド | すること | 返すもの |
|---|---|---|
| `pick` | `ai` ラベル付きで `wip`・`ai-failed` の無い open Issue のうち最古を取り、`wip` を付ける | `{issue:{n,title,body}, classes:[{name,criteria}]}` |
| `place` | 下記。Orchestrator が選んだ class で資源を決める | placement（`runner` を含む） |
| `status` | `Closes #n` の PR（open を優先）と、`--pid` があればその process が生きているか。gh を 1 回呼ぶだけ | `{issue, pr, state, running?}` |
| `dispatch` | `runner.mode` ごとに渡す。`subagent`：worktree `<path>-task-<n>`（branch `task/<n>`、origin/main から。前回の worktree・branch が残っていれば再利用）を作る。`process`：同じ worktree で `runner.cmd`（`{model}` を置換）+ prompt をバックグラウンド起動（log は `<state>/task-<n>.log`）。`cloud`：`claude -p <prompt> --cloud <session> --output-format json`。`vm`：下の「vm runner」 | subagent：`{runner, worktree, prompt}`（Orchestrator が Agent tool で起動）。process：`{started, worktree, pid, log}`。cloud：claude の JSON。vm：`{started, instance, container, task, startSec}` |

- worktree は NS の repo の隣に作る（親ディレクトリの aienv binding が効く）
- repo・path は `--repo` / `--path` > namespace の登録（`ORCHD_NAMESPACES` > `cad/config/namespaces.json` > その `.example.json`。orchd は読むだけ）> CWD の git toplevel と `gh repo view`
- cloud の session：`CLAUDE_CLOUD_SESSION` > 登録の `cloudWorkerSession`（owner が `claude --cloud` で 1 度作る）。無ければ exit 2
- 未実装（後で）：完了処理（PR 確認・`wip` 解除・worktree の片付け）、実行記録、同時数の上限、cad の SSE による defer の再開

## vm runner（opencode の cloud worker）

`runners["opencode@gce-spot"]` / `["opencode@gce-std"]` = `{mode: vm, instance, zone, project, image, model}`。VM と image は `deploy/gcp/README.md` の「opencode worker VM」（#53。owner が `create-worker.sh` で作るまでは describe が失敗して exit 5 → `--exclude` で local に落ちる）。VM は普段 `TERMINATED`（停止）で、task が終わると自分で止まる。`dispatch` は：

Compute Engine REST API v1 を直接呼ぶ（gcloud・ssh は使わない。Go の標準ライブラリだけ）：

1. `instances.get` で状態と metadata を見る。`STOPPING` / `PENDING_STOP`（自己停止の途中）なら `TERMINATED` まで待つ
2. `TERMINATED` 以外（`RUNNING` / `PROVISIONING` / `STAGING` など＝別 task が使用中）は exit 5。VM 1 台で task は 1 つ。`TERMINATED` でも、5 分以内の `worker-task`（別の dispatch が setMetadata から start までの途中）がある、または `startup-script` が `worker-task` を読まない旧版なら exit 5
3. `instances.setMetadata` で metadata `worker-task` = `<id> <n> <repo> <model> <image>` を置く（他の item はそのまま。fingerprint 付きなので同時の dispatch は片方が 412 → exit 5）
4. `instances.start`、operation が `DONE` になるまで 5 秒おきに見る。失敗（Spot の容量不足など）したら instance を見直し、起動中（`PROVISIONING` / `STAGING` / `RUNNING`）なら成功扱い、`TERMINATED` なら `worker-task` を消して（best effort）exit 5
5. `{started, instance, container（opencode-worker-<n>）, task, startSec（dispatch 開始から start の完了まで、実測）}` を出す

1〜4 は合わせて 170 秒まで（各 HTTP 呼び出しは 30 秒まで。dispatch 全体で約 4 分以内）。VM は boot 時に `worker-task` を読み、`docker run -d --rm --name opencode-worker-<n> -v /var/lib/cad:/data -e ISSUE -e REPO -e MODEL <image>` を 1 度だけ実行する（`deploy/gcp/worker-startup.sh`。worker が Issue を読み、PR `Closes #n` を出す）。container が起動したかは orchd からは見えない（`orchd status` の PR で待つ）。

access token：GCE 上（cad-2 の cad image 内）は metadata server（`GCE_METADATA_HOST` で上書き可）の default service account、それ以外（Mac）は `gcloud auth print-access-token`。API の base は `ORCHD_COMPUTE_URL`（既定 `https://compute.googleapis.com/compute/v1`、テスト用）。権限：token の主体に worker VM への `compute.instances.get` / `setMetadata` / `start`（と `compute.zoneOperations.get`）が要る。cad-2 から使うなら SA `cad-vm@` に付ける（例 `roles/compute.instanceAdmin.v1`。SA 付き VM の setMetadata には `roles/iam.serviceAccountUser` も要る場合がある。未検証）。

完了は `orchd status --issue <n>`（PR）で待つ。VM が `TERMINATED` なのに PR が無ければ、preempt か worker の失敗（`skills/orchestrate`）。`gce-std` は cad の `config.json` の computers（費用の記録）には未登録（価格を確認してから足す）。

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
