# cad on GCP (Container-Optimized OS)

cad + orchd + Claude Code を 1 つの image（`ghcr.io/jsongold/codingagentenv/cad`）にして、COS の VM で常駐させる。

- image：main への push ごとに GitHub Actions（`.github/workflows/image.yml`）が build し、`:main` と `:sha-<7桁>` を GHCR に push する
- VM：startup script（`startup.sh`、毎 boot root で実行）が image 内の `fetch-auth` を起動して Secret Manager から secret を volume に書き（下の「secrets」）、COS 既定の `live-restore: true`（swarm と非互換）を `/var/lib/docker/daemon.json` で false にして（初回のみ docker を再起動。COS の docker.service が起動前にこのファイルを `/etc/docker/daemon.json` へ copy する）、single-node Docker Swarm を init し（初回のみ。swarm の状態は `/var/lib/docker` に残る）、service `cad`（`--network host`、`/var/lib/cad:/data`）を作る。`cad-update.timer` が 5 分ごとに `docker service update --image …:main cad` を実行し、digest が変わっていれば health-gated に入れ替え、失敗すれば自動で前の image に rollback する（= main に追従）
- 旧来の `gcloud compute instances create-with-container`（COS の container 起動 agent）は deprecated なので使わない
- アクセスは IAP SSH のみ（firewall `allow-iap-ssh-cad`：tcp:22 from 35.235.240.0/20、tag `cad`）。cad は VM の `127.0.0.1:7878` だけで listen する（`--network host` + cad の既定 addr。非 loopback は `CAD_TOKEN` なしだと cad 自身が拒否する）
- 既定：project `suggestorder-dev`、zone `us-central1-a`、VM `cad-2`、`e2-micro`（env `PROJECT` `ZONE` `VM` `MACHINE` で上書き）

## container 内のレイアウト

| 何 | 場所 |
|---|---|
| バイナリ | `/app/cad/bin/cad`、`/app/orchd/bin/orchd`、`/home/cad/.local/bin/claude`（PATH 済み） |
| 既定の設定 | `/app/cad/config.json`、`/app/orchd/policy.json`（`CAD_HOME` / `ORCHD_HOME`） |
| volume（`/data` = `$HOME`、VM では `/var/lib/cad`） | `~/.aienv/.store/<id>`（claude / codex / opencode の store）、`~/.claude*`、`/data/orchd/state`（`ORCHD_STATE_DIR`）、`/data/cad/config/namespaces.json`（`/app/cad/config` の symlink 先） |
| agents の上書き | `/data/cad-config.json` があれば `CAD_CONFIG` にする（`deploy/entrypoint.sh`）。無ければ image の既定。`cad add/rm` で書き換えるならこちらを置く |

user は `cad`（uid 10001）。image の Claude Code は auto-update しない（`DISABLE_AUTOUPDATER=1`）。main に push されるたびに image の build で最新が入る。

## 作成（owner が Mac で、この順に）

```bash
deploy/gcp/secrets.sh       # 1. Secret Manager: API 有効化・SA cad-vm@ 作成・secret 作成と upload・secretAccessor 付与
deploy/gcp/create-vm.sh     # 2. firewall と VM（SA cad-vm@、scope cloud-platform、metadata cad-secrets）。既にあれば skip
S="gcloud compute ssh cad-2 --project suggestorder-dev --zone us-central1-a --tunnel-through-iap --"
$S sudo journalctl -u google-startup-scripts | grep 'cad-opencode'   # 3. "cad-opencode-<id>: wrote ..." を確認
$S "sudo docker exec \$(sudo docker ps -q -f label=com.docker.swarm.service.name=cad) cad show usage"   # opencode の usage が出る
# 4. Claude の /login（下の「Claude のログイン」）
```

既存の VM（SA なしで作った cad-2）を移行する場合は、2 の代わりに（SA の変更は VM の停止が必要）：

```bash
P="--project suggestorder-dev --zone us-central1-a"
gcloud compute instances stop cad-2 $P
gcloud compute instances set-service-account cad-2 $P --service-account cad-vm@suggestorder-dev.iam.gserviceaccount.com --scopes cloud-platform
gcloud compute instances add-metadata cad-2 $P --metadata-from-file startup-script=deploy/gcp/startup.sh,cad-secrets=deploy/gcp/secrets.list
gcloud compute instances start cad-2 $P
```

## secrets（Secret Manager）

- 一覧は `secrets.list`（`<service> <id>` → secret `cad-<service>-<id>`。今は `opencode 996c87ae`）。Mac の `secrets.sh` と VM の `startup.sh`（metadata `cad-secrets` 経由）が同じ一覧を使う。行を足したら `secrets.sh` と `add-metadata ... cad-secrets=deploy/gcp/secrets.list`
- `secrets.sh`（冪等、gcloud を表示してから実行、値は表示しない）：`secretmanager.googleapis.com` 有効化 → SA `cad-vm@` が無ければ作成 → secret が無ければ作成（automatic replication）→ 有効な version が無ければ `gcloud secrets versions add --data-file ~/.aienv/.store/<id>/opencode/auth.json` → `roles/secretmanager.secretAccessor` を **secret ごとに** SA へ付与（project 全体には付けない）
- VM 側（毎 boot と `cad-update.timer` の 5 分ごと）：`startup.sh` は `docker run --rm --pull always --network host -v /var/lib/cad:/data --entrypoint /app/bin/fetch-auth …/cad:main` を起動するだけ。取得は image 内の `fetch-auth`（`deploy/fetch-auth.sh`）：metadata server から一覧（`cad-secrets`）・project・SA の access token を取り、REST `secrets/<name>/versions/latest:access` を curl で呼び、`jq -r .payload.data | base64 -d` で `/data/.aienv/.store/<id>/opencode/auth.json`（VM の `/var/lib/cad/...`、container の `$HOME/.aienv/...`）へ atomic に書く（mode 600、container の uid 10001 で書く）。失敗したら log を出して既存のファイルを残す。値も token も log に出さない。一覧は env `CAD_SECRETS_LIST`（ファイルパス）でも渡せる
- COS には package manager が無く jq も無いので、JSON の解釈は jq / curl / base64 を持つ image 側でやる。`--network host` の container は VM の network namespace を共有する（[Docker: host network](https://docs.docker.com/engine/network/drivers/host/)）ので metadata server に VM と同じように届く（GKE も host network の Pod の要求は Compute Engine metadata server へ行くと明記：[Workload Identity](https://cloud.google.com/kubernetes-engine/docs/concepts/workload-identity)）。`--pull always` は service が digest 固定で local の `:main` tag を追わないため。registry に届かなければ取得は skip（既存ファイルは残る）
- ログ：boot 時は `journalctl -u google-startup-scripts`、timer は `journalctl -u cad-update`（`cad-opencode-<id>: wrote ...`）
- rotation：`ROTATE=1 deploy/gcp/secrets.sh` で新しい version を足す。VM は `latest` を読むので 5 分以内に反映（今すぐなら `$S sudo systemctl start cad-update`）。古い version は `gcloud secrets versions destroy <n> --secret cad-opencode-<id> --project suggestorder-dev` で消す（destroy 済みは無料）
- ローカル検証：`bash test/fetch-auth.test.sh`（偽の metadata / Secret Manager に対して decode・600・atomic・失敗時の保持・log に値が出ないことを確認）。image 内で：`docker build -t cad-local . && CAD_TEST_IMAGE=cad-local bash test/fetch-auth.test.sh`（Docker Desktop は host networking を有効にしておく）

## Claude のログイン

Claude の認証は Secret Manager に入れず volume に置く。対話の OAuth（/login）で作られ、refresh token が使うたびに回転して container 内の claude が書き戻すため、Secret Manager の値を正にすると古い token で上書きして壊す。アカウントごとに：

```bash
gcloud compute ssh cad-2 --project suggestorder-dev --zone us-central1-a --tunnel-through-iap -- -t \
  'sudo docker exec -it -e CLAUDE_CONFIG_DIR=/data/.aienv/.store/a12e00a7 $(sudo docker ps -q -f label=com.docker.swarm.service.name=cad) claude'
# /login → ブラウザで完了 → /exit
```

`CLAUDE_CONFIG_DIR=/data/.aienv/.store/<id>` が必要。cad の usage collector は `CLAUDE_CONFIG_DIR=$HOME/.aienv/.store/<id> claude` を実行し、`$HOME=/data` だから。付けないと `/data/.claude`（= `claude/default`）に入る。

## 確認

container 名は `cad.1.<task id>` なので、exec は service の label で引く。

```bash
S="gcloud compute ssh cad-2 --project suggestorder-dev --zone us-central1-a --tunnel-through-iap --"
$S sudo docker service ps cad                 # task 履歴（どの image が Running / Failed / Rollback か）
$S sudo docker service logs cad
$S sudo docker service inspect cad --pretty   # 現在の image、UpdateStatus、update/rollback 設定
$S "sudo docker exec \$(sudo docker ps -q -f label=com.docker.swarm.service.name=cad) cad show usage"
$S sudo systemctl status cad-update.timer
$S sudo journalctl -u cad-update               # 5 分ごとの update の結果（rollback もここに出る）
```

Mac から API を使う（別端末で張りっぱなし）：

```bash
gcloud compute ssh cad-2 --project suggestorder-dev --zone us-central1-a --tunnel-through-iap -- -N -L 17878:127.0.0.1:7878
CAD_ADDR=127.0.0.1:17878 cad get meta -ns default
```

## 更新・rollback・version の固定

仕組み（`startup.sh`）：image の `HEALTHCHECK`（`curl http://127.0.0.1:7878/healthz`、start-period 90s）が通るまで task は Running にならない。`--update-failure-action rollback --update-monitor 90s` なので、新しい task が 90s 以内に落ちる・unhealthy になると swarm が自動で前の spec（image）に戻す。`--network host` で 127.0.0.1:7878 を bind できるのは 1 container だけなので update / rollback とも `stop-first`（入れ替え中は数秒止まる）。

- 更新：main に merge すれば 5 分以内に VM が追従する。今すぐなら `$S sudo systemctl start cad-update`（終わるまで待ち、結果は `journalctl -u cad-update`）
- 手動 rollback：`$S sudo docker service rollback cad`（直前の spec に戻す。次の timer で :main に戻るので、止めたいなら下の固定も）
- 一時的に固定（次の reboot まで）：`$S sudo systemctl stop cad-update.timer` → `$S sudo docker service update --image ghcr.io/jsongold/codingagentenv/cad:sha-<7桁> cad`
- 恒久的に固定：`startup.sh` の `IMAGE` を `…/cad:sha-<7桁>` にして metadata を差し替え、`$S sudo systemctl start cad-update`（または reset）。timer は以後その tag に update する（= 何も変わらない）

```bash
gcloud compute instances add-metadata cad-2 --project suggestorder-dev --zone us-central1-a \
  --metadata-from-file startup-script=deploy/gcp/startup.sh
gcloud compute instances reset cad-2 --project suggestorder-dev --zone us-central1-a
```

`/etc` は tmpfs なので unit は boot ごとに startup script が作り直す。`/var/lib/cad` は stateful partition 上にあり boot disk がある限り残る（[COS: disks and file system](https://cloud.google.com/container-optimized-os/docs/concepts/disks-and-filesystem)）。

## 削除

```bash
gcloud compute instances delete cad-2 --project suggestorder-dev --zone us-central1-a   # volume（boot disk）ごと消える
gcloud compute firewall-rules delete allow-iap-ssh-cad --project suggestorder-dev        # 他に tag cad の VM が無ければ
```

## 費用（月額、us-central1）

e2-micro $6.11 + 外部 IP $3.65 ≈ $9.76（+ boot disk 10GB の standard PD）。

Secret Manager（[pricing](https://cloud.google.com/secret-manager/pricing)、billing account 単位の無料枠）：active version 6 個 / 月と access 10,000 回 / 月は無料。超過は active version $0.06 / 月（$0.000082192 / 時）、access $0.03 / 10,000 回。secret 1 つ × 5 分ごとは約 8,900 回 / 月で無料枠内（secret を増やすと枠を超える。2 つ目からは約 $0.03 / 月ずつ）。

## opencode worker VM（`worker-spot` / `worker-std` / `worker-spot-2`）

opencode の task を 1 件ずつ container で実行する VM を 1〜3 台持つ（`create-worker.sh` の引数 / 環境変数 `N`、既定 2、最大 3。ADR-0013 の再検討条件「並列度が 2 台で足りない」）。全台 e2-medium・COS・boot disk 10GB pd-balanced、tag `cad`（IAP SSH）、SA `cad-vm@`（scope cloud-platform）。名前は spot/std/spot の順で決め、既定の N=2 は元のままの `worker-spot`（Spot）+ `worker-std`（standard）、3 台目は 2 台目の standard ではなく安い Spot を増やして `worker-spot-2` にする。Spot は `--provisioning-model=SPOT --instance-termination-action=STOP`（preempt されても削除されず停止し、disk と cache 済み image が残る。[Spot VMs](https://cloud.google.com/compute/docs/instances/create-use-spot)）。どちらの種類を使うかは orchd の rule 順（`opencode@gce-spot` → `opencode@gce-std`、`orchd/README.md`）、同じ種類が複数台あれば `runners["opencode@gce-spot"].instance` のカンマ区切りリスト（例 `"worker-spot,worker-spot-2"`）から `orchd/vm.go` が `TERMINATED` の 1 台を選ぶ（全台使用中なら exit 5 → `orchd place --exclude gce-spot` で次の rule へ）。

- image：`Dockerfile.worker` → `ghcr.io/jsongold/codingagentenv/opencode-worker:main` と `:sha-<7桁>`（`image.yml` が main への push ごとに build）。git・gh・opencode（公式 install script）・`fetch-auth`。entrypoint `deploy/worker-run.sh`：env `ISSUE` `REPO` `MODEL`。`/data` の auth を読み、clone → branch `task/<ISSUE>`（push 済みなら再利用）→ `opencode run --auto --model $MODEL "<gh issue view の内容>"` → commit・push → `Closes #<ISSUE>` の PR が無ければ `gh pr create --base main`。opencode が失敗したら branch だけ push して PR は作らず非 0。push は opencode の終了後の 1 回だけなので、その前に preempt されると作業は失われる（Issue は queue に戻る、`skills/orchestrate`）
- 起動（毎 boot、`worker-startup.sh`）：`fetch-auth` で secret を `/var/lib/cad` へ → metadata `worker-task`（`<id> <issue> <repo> <model> <image>`。`orchd dispatch` が Compute API の setMetadata で置いてから start する）を読み、`docker run -d --rm --name opencode-worker-<issue> ...` → image を pull。task の id を `/var/lib/cad/task-done` に記録し、同じ id では再実行しない（reboot・手動 start。VM から metadata は消せないので消さない）。image が cache 済みなら pull は task の起動の後（cache の image で即起動し、pull は変わった layer だけ取って次回に効く）。cache が無い初回だけ先に pull する。orchd からの ssh は無い
- 停止（自動）：`worker-stop.service` が `docker events`（container の die）を見て、`opencode-worker-*` が終わり `stop-grace-seconds`（metadata、既定 10）後に 1 つも動いていなければ `shutdown -h now`。保険として `worker-idle.timer` が `idle-minutes`（metadata、既定 30）の間 worker container が無ければ止める（container が起動しなかった dispatch・作成直後の初回 boot）。guest OS からの shutdown は stop 扱いで instance は `TERMINATED` になり、`TERMINATED` の間は vCPU・メモリは課金されない（disk と外部 IP は課金）：[stop-start](https://cloud.google.com/compute/docs/instances/stop-start-instance)、[instance life cycle](https://cloud.google.com/compute/docs/instances/instance-life-cycle)
- grace：orchd は `TERMINATED` 以外の VM を使用中（exit 5）として扱うので、grace 中の VM に次の task は入らない（task は boot 時にだけ読む）。grace は短いほど idle の費用が減る。Spot の preempt 時の shutdown 猶予は best effort で最大 30 秒（[Spot VMs](https://cloud.google.com/compute/docs/instances/spot)）
- image の更新：cache 済みなら pull は task の起動の後なので、main の新しい image はその次の起動から使われる（1 回遅れ）
- image には git・gh・opencode しか無い。Go / Node などのテストが要る repo は、その toolchain を image に足すまで opencode がテストを実行できない
- 速度の選択：停止した VM の `start` を使う（suspend/resume はメモリの復元が再起動より遅いことがある、と MIG の standby pool の doc にある）。`--skip-guest-os-shutdown` は API からの stop/delete にだけ効くので、自分で shutdown するこの VM には使わない。並列度を上げたくなったら MIG の standby pool が次の候補（未実装）

### 作成（owner が 1 度だけ、この順に）

1. GitHub の fine-grained PAT を作る：Repository access = 対象 repo、Permissions = Contents: Read and write、Pull requests: Read and write、Workflows: Read and write（`.github/workflows/` を変える Issue の push に必要）、Issues: Read（`gh issue view`）、Metadata: Read
2. Secret Manager に入れ、SA に読ませる（`worker-auth.list` の `github worker` → secret `cad-github-worker`。値は stdin から、表示しない。opencode の `cad-opencode-996c87ae` は `secrets.sh` で作成・付与済み）：

```bash
P=suggestorder-dev
gcloud secrets create cad-github-worker --project $P --replication-policy automatic
read -rs T && printf %s "$T" | gcloud secrets versions add cad-github-worker --project $P --data-file=- ; unset T
gcloud secrets add-iam-policy-binding cad-github-worker --project $P \
  --member serviceAccount:cad-vm@$P.iam.gserviceaccount.com --role roles/secretmanager.secretAccessor
```

3. `deploy/gcp/create-worker.sh [N]`（`N` = 1〜3、既定 2。既にある VM は skip、後から `N` を増やせば足りない分だけ作る）。firewall `allow-iap-ssh-cad` は `create-vm.sh` が作る。初回 boot が image を pull し終えたら（serial port に `worker: ready`）表示される `gcloud compute instances stop ...` で止める。止めなくても 30 分で保険の timer が止める
4. 確認：`gcloud compute instances list --project suggestorder-dev --filter=labels.app=cad-worker`（全台 `TERMINATED`）
5. `N` を 3 にした（`worker-spot-2` を追加した）場合は `orchd/policy.json` の `runners["opencode@gce-spot"].instance` を `"worker-spot,worker-spot-2"` に直す（`create-worker.sh` がこのコマンドを最後に出す）

token の rotation は 2 の `versions add` だけ（VM は boot ごとに `latest` を読む）。

### 確認・ログ

```bash
W="gcloud compute ssh worker-spot --project suggestorder-dev --zone us-central1-a --tunnel-through-iap --"  # 台名を変えれば他の worker も同じ
$W sudo journalctl -u google-startup-scripts | grep -E 'worker:|cad-'   # secret の取得・task・pull
$W sudo docker ps --filter name=opencode-worker-
$W sudo journalctl -u worker-stop -u worker-idle                        # 自動停止の理由
```

Cloud Logging を有効にした後は下の「Cloud Logging」の `gcloud logging read` でも同じログが見える（VM が `TERMINATED` でも過去分は読める）。

### 費用（us-central1、動いている間だけ）

- `worker-std`：e2-medium $24.46 / 月（常時稼働した場合。≈ $0.0335 / 時）
- `worker-spot` / `worker-spot-2`：Spot は on-demand から最大 91% 引き、価格は最大 1 日 1 回変わる（[Spot VMs](https://cloud.google.com/compute/docs/instances/spot)、[Spot pricing](https://cloud.google.com/spot-vms/pricing)）。e2-medium の Spot は約 $0.01〜0.03 / 時の見込み（**推定**。公式の表で確認していない）
- 停止中も課金：boot disk 10GB pd-balanced × 台数（と外部 IP の扱いは [IP pricing](https://cloud.google.com/vpc/network-pricing#ipaddress) に従う）。task 1 件（起動 + 実行 30 分 + grace 10 秒）で std ≈ $0.02
- `cad/config.json` の `computers` には `gce-spot` の隣に `gce-std`（e2-medium standard の見積り、上の $24.46/月 ≈ $0.0335/時から算出、**推定**）を足してある。`gce-spot-2` は同じ `gce-spot` の記録を使う（同じ machine type・Spot）ので追加のエントリは不要

## Cloud Logging

COS に Ops Agent は使えない（[Ops Agent の対応 OS 一覧](https://cloud.google.com/monitoring/agent/ops-agent/supported-operating-systems)に COS は無く、Google のドキュメントも COS には [Cloud Logging 専用の手順](https://cloud.google.com/container-optimized-os/docs/how-to/logging)を案内している）。COS は代わりに組み込みの fluent-bit（[COS 109 以降](https://cloud.google.com/container-optimized-os/docs/how-to/logging)）がシステムログと `docker run` した container の標準出力・標準エラーを Cloud Logging に送る。有効化は instance metadata の `google-logging-enabled=true` だけで、`create-vm.sh`（cad-2）と `create-worker.sh`（worker-*）が新規作成時に付ける。docker の log driver は変えない（既定の `json-file` のまま。`gcplogs` は使わない）。

既存の VM（このメタデータを持たずに作った分）に足すには：

```bash
gcloud compute instances add-metadata cad-2 --project suggestorder-dev --zone us-central1-a \
  --metadata google-logging-enabled=true
for vm in worker-spot worker-std; do   # 3 台目があれば worker-spot-2 も足す
  gcloud compute instances add-metadata "$vm" --project suggestorder-dev --zone us-central1-a \
    --metadata google-logging-enabled=true
done
```

反映は次回 boot から（起動中の VM は再起動が要る。`gcloud compute instances reset <vm> --project suggestorder-dev --zone us-central1-a`）。

ログの確認：

```bash
gcloud logging read 'resource.type="gce_instance" AND resource.labels.instance_id="'$(gcloud compute instances describe worker-spot --project suggestorder-dev --zone us-central1-a --format='value(id)')'"' \
  --project suggestorder-dev --limit 50 --order asc
```

または Logs Explorer（console.cloud.google.com/logs）で `resource.type="gce_instance"` と `labels."compute.googleapis.com/resource_name"="worker-spot"`（VM 名）で絞る。

## IAM（cad-vm@ の権限、owner 確認・未検証）

以下は **未検証**（gcloud で実行して結果を確認していない）。適用は owner が行うこと。

`cad-vm@$PROJECT.iam.gserviceaccount.com` は次の 2 つの理由で権限が要る：

1. Cloud Logging への書き込み（上の「Cloud Logging」。VM 自身が自分のログを送る）：`roles/logging.logWriter`
2. `orchd/vm.go` が cad-2（cad image、`cad-vm@` で動く）から worker VM を REST API で操作する（`instances.get` / `instances.setMetadata` / `instances.start` / `zoneOperations.get`。`orchd/README.md` の「vm runner」）

```bash
P=suggestorder-dev
gcloud projects add-iam-policy-binding $P \
  --member serviceAccount:cad-vm@$P.iam.gserviceaccount.com --role roles/logging.logWriter
```

Compute 側は project 全体の `roles/compute.instanceAdmin.v1` は権限が広すぎる（全 VM の作成・削除まで含む）。worker VM だけに絞るなら instance レベルの IAM（[IAM condition でリソースを絞る](https://cloud.google.com/iam/docs/conditions-overview)）で `compute.instances.get` / `setMetadata` / `start` を worker VM 3 台（`worker-spot` `worker-std` `worker-spot-2`）にだけ付け、`compute.zoneOperations.get`（`orchd/vm.go` の `wait` が operation を poll する）は zone operation が instance の子リソースではなく instance 単位の IAM binding が効かないため、project レベルで別に付ける（2 つに分ける分、project レベルの方は zoneOperations.get だけの最小ロールにする）：

```bash
cat >/tmp/orchd-vm-role.yaml <<'YAML'
title: orchdVmDispatch
description: orchd dispatch (vm runner): start a stopped worker VM and set its task metadata
stage: GA
includedPermissions:
- compute.instances.get
- compute.instances.setMetadata
- compute.instances.start
YAML
gcloud iam roles create orchdVmDispatch --project $P --file /tmp/orchd-vm-role.yaml
for vm in worker-spot worker-std worker-spot-2; do
  gcloud compute instances add-iam-policy-binding "$vm" --project $P --zone us-central1-a \
    --member serviceAccount:cad-vm@$P.iam.gserviceaccount.com --role "projects/$P/roles/orchdVmDispatch"
done

cat >/tmp/orchd-zoneops-role.yaml <<'YAML'
title: orchdZoneOperations
description: orchd dispatch (vm runner): poll the setMetadata/start operation it started
stage: GA
includedPermissions:
- compute.zoneOperations.get
YAML
gcloud iam roles create orchdZoneOperations --project $P --file /tmp/orchd-zoneops-role.yaml
gcloud projects add-iam-policy-binding $P \
  --member serviceAccount:cad-vm@$P.iam.gserviceaccount.com --role "projects/$P/roles/orchdZoneOperations"
```

`cad-vm@` は自分自身にも `--service-account` として使われる（`create-vm.sh` / `create-worker.sh` の VM 作成時）ため、それらの VM 作成コマンドを実行する側（owner の gcloud、または CI）に `roles/iam.serviceAccountUser`（`cad-vm@` に対して）が要る場合がある。cad-2 上で `cad-vm@` が REST API から worker VM を操作するだけなら（VM を新たに作らない限り）`serviceAccountUser` は不要（**未検証**）。
