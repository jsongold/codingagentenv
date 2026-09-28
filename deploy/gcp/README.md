# cad on GCP (Container-Optimized OS)

cad + orchd + Claude Code を 1 つの image（`ghcr.io/jsongold/codingagentenv/cad`）にして、COS の VM で常駐させる。

- image：main への push ごとに GitHub Actions（`.github/workflows/image.yml`）が build し、`:main` と `:sha-<7桁>` を GHCR に push する
- VM：startup script（`startup.sh`、毎 boot root で実行）が single-node Docker Swarm を init し（初回のみ。swarm の状態は `/var/lib/docker` に残る）、service `cad`（`--network host`、`/var/lib/cad:/data`）を作る。`cad-update.timer` が 5 分ごとに `docker service update --image …:main cad` を実行し、digest が変わっていれば health-gated に入れ替え、失敗すれば自動で前の image に rollback する（= main に追従）
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

## 作成

```bash
deploy/gcp/create-vm.sh     # firewall と VM を作る。既にあれば skip。削除はしない
```

## secrets

```bash
deploy/gcp/secrets.sh                 # opencode auth.json を VM の volume にコピー（中身は表示しない）
                                      # 引数: [opencode-id] [claude-id ...]
```

最後に Claude のログイン手順を表示する。アカウントごとに：

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
