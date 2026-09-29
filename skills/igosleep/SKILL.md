---
name: igosleep
description: owner が寝る前に sleep loop を始める。cad-2 の登録簿に cloud worker セッション（CCO）があるか確かめ、cad-2 の `orchd-sleep.timer` を start し、/handoff して、Claude Code の残り枠と timer の次回実行を表示する。cad-2 の timer を動かす操作なので、owner が `/igosleep` と打ったときだけ使う（自然文では起動しない）。
disable-model-invocation: true
---

# igosleep — 寝る前に sleep loop を始める

sleep 中は cad-2 の `orchd-sleep.timer` が 5 分ごとに `orchd place --mode sleep | orchd dispatch --placement -` を実行し、CC に枠があれば cloud worker セッション（CCO）へ固定の指示（`orchd/wake.md`）を 1 通送る。何をやるかは CCO がプロジェクトの文脈から決める（ADR-0015）。timer 自体が on/off の切り替えで、namespace は `default` だけ（timer は `--ns` を付けない）。
cad-2 の操作は `deploy/gcp/README.md` の経路（IAP SSH + `docker exec`）を使う：

```bash
S="gcloud compute ssh cad-2 --project suggestorder-dev --zone us-central1-a --tunnel-through-iap --"
X='sudo docker exec -i $(sudo docker ps -q -f label=com.docker.swarm.service.name=cad)'
F=/data/cad/config/namespaces.json   # timer の orchd が読む登録（container 内）
```

1. CCO のセッションを確かめる：cad-2 の登録を読む。「ファイルが無い」は container 内で判定して `{}` にし、ssh・docker の失敗や JSON でない中身は止める（ここで止めて owner に伝え、何も書き込まない。`{}` で続けると他の ns を消す）：

   ```bash
   $S "$X sh -c 'if [ -f $F ]; then cat $F; else echo {}; fi'" >/tmp/igosleep-cur.json \
     && jq -e 'type == "object"' /tmp/igosleep-cur.json >/dev/null \
     || { rm -f /tmp/igosleep-cur.json; echo 'STOP: cad-2 の namespaces.json を読めない'; }
   ```

   `<repo>` は cad-2 の登録を優先して決める：`/tmp/igosleep-cur.json` の `default.repo`、無ければ手元の `cad/config/namespaces.json`（無ければ `.example.json`）の `default.repo`、無ければ `gh repo view --json nameWithOwner -q .nameWithOwner`。
   `default.cloudWorkerSession` は `session_` か `cse_` で始まる値だけを有効とする（既存の値も同じ基準）。
   - 有効な値がある：そのまま 2 へ。
   - 空・キーが無い・形式が違う：形式が違う既存値なら「登録済みの `<値>` はセッション ID ではない」と伝える。そのうえで owner に「claude.ai/code で新しいセッションを作り、GitHub repo `<repo>` を選んで開始し、URL 末尾の `session_…` を教えて」と頼み、返るまで待つ。CLI の `claude --cloud` では作らない（bundle になり push できない）。
   - 受け取ったら、読んだ登録に書き足して書き戻す（他のキー・他の ns は残す。1 の読み取りが STOP なら書かない）→ もう一度読んで確認する：

     ```bash
     jq --arg r <repo> --arg s <session> '.default.repo //= $r | .default.cloudWorkerSession = $s' \
       /tmp/igosleep-cur.json >/tmp/igosleep-ns.json \
       && jq -e '.default.cloudWorkerSession | startswith("session_") or startswith("cse_")' /tmp/igosleep-ns.json >/dev/null \
       && $S "$X sh -c 'cat >$F.tmp && mv $F.tmp $F'" </tmp/igosleep-ns.json
     ```
2. timer を start し、`active` になったことを確かめる：`$S sudo systemctl start orchd-sleep.timer && $S systemctl is-active orchd-sleep.timer`。`active` でなければ止めて owner に伝える（sleep 中に何も起きない）。reboot すると timer は止まる（`deploy/gcp/README.md` の「sleep loop」）。
3. `/handoff` の手順（`skills/handoff/SKILL.md`）で handoff を書き出してコミットする。
4. 表示して終わる（2 行）：
   - CC 残り枠：`$S "$X cad show usage"` の `claude/*` の行。5H・7D の残り（100 − 使用率）と RESETS。枠が無い間は CCO は起こされない（worker VM の opencode への切り替えは未対応、#78）ので、残りが少なければその旨も書く
   - timer：`$S systemctl list-timers orchd-sleep.timer` の次回（NEXT）

起きたら `/iwokeup`（timer を stop して夜間の結果を見る）、無ければ `$S sudo systemctl stop orchd-sleep.timer` で止めて `/pickup` で再開する。
