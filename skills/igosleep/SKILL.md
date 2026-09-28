---
name: igosleep
description: owner が寝る前に NS を sleep モードにする。cloud worker セッションの登録を確かめ、cad-2 で `orchd mode set sleep`、/handoff、流れる予定の ai Issue 件数と Claude Code の残り枠を表示する。「寝る」「sleep にして」と言われたら使う。
argument-hint: "[ns]"
disable-model-invocation: true
---

# igosleep — 寝る前にまとめて sleep モードへ

sleep 中は cad-2 の timer（`orchd-sleep.timer`）が `orchd dispatch --pending` を回し、LLM の Orchestrator は置かない（ADR-0014）。引数 `<ns>` は任意、既定 `default`（`[a-z0-9-]` 以外は拒否して聞き直す）。
cad-2 の操作は `deploy/gcp/README.md` の経路（IAP SSH + `docker exec`）を使う。以下 `S` と `X` はこの 2 つ：

```bash
S="gcloud compute ssh cad-2 --project suggestorder-dev --zone us-central1-a --tunnel-through-iap --"
X='sudo docker exec -i $(sudo docker ps -q -f label=com.docker.swarm.service.name=cad)'
F=/data/cad/config/namespaces.json   # timer の orchd が読む登録（container 内）
```

1. worker セッションを確かめる：cad-2 の登録を読む。「ファイルが無い」は container 内で判定して `{}` にし、ssh・docker の失敗や JSON でない中身は止める（ここで止めて owner に伝え、何も書き込まない。`{}` で続けると他の ns を消す）：

   ```bash
   $S "$X sh -c 'if [ -f $F ]; then cat $F; else echo {}; fi'" >/tmp/igosleep-cur.json \
     && jq -e 'type == "object"' /tmp/igosleep-cur.json >/dev/null \
     || { rm -f /tmp/igosleep-cur.json; echo 'STOP: cad-2 の namespaces.json を読めない'; }
   ```

   `/tmp/igosleep-cur.json` の `<ns>.cloudWorkerSession` を見る。
   - 空・キーが無い：owner に「claude.ai/code で新しいセッションを作り、GitHub repo `<repo>` を選んで開始し、URL 末尾の `session_…` を教えて」と頼み、返るまで待つ。CLI の `claude --cloud` では作らない（bundle になり push できない）。`<repo>` は timer が使う cad-2 の登録を優先する：`/tmp/igosleep-cur.json` の `<ns>.repo`、無ければ手元の `cad/config/namespaces.json`（無ければ `.example.json`）の `<ns>.repo`、無ければ `gh repo view --json nameWithOwner -q .nameWithOwner`。
   - 受け取ったら、読んだ登録に書き足して書き戻す（他のキー・他の ns は残す。上の読み取りが STOP なら書かない）→ もう一度読んで確認する：

     ```bash
     jq --arg n <ns> --arg r <repo> --arg s <session> '.[$n].repo //= $r | .[$n].cloudWorkerSession = $s' \
       /tmp/igosleep-cur.json >/tmp/igosleep-ns.json \
       && jq -e --arg n <ns> '.[$n].cloudWorkerSession | startswith("session_")' /tmp/igosleep-ns.json >/dev/null \
       && $S "$X sh -c 'cat >$F.tmp && mv $F.tmp $F'" </tmp/igosleep-ns.json
     ```
2. `$S "$X orchd mode set sleep --ns <ns> --by owner"` → `{"mode":"sleep",…}` を確認する。exit 2（mode 不明）なら cad-2 の image に `sleep` mode（PR #69）が未反映。ここで止めて owner に伝える。
3. `/handoff` の手順（`skills/handoff/SKILL.md`）で handoff を書き出してコミットする。
4. 表示して終わる（3 行）：
   - 流れる予定：手元で `orchd issue list --ns <ns>` の `state == "pending"` の件数と番号（`wip`・`ai-failed`・milestone 付きは流れない）。`class:` ラベルの無いものは既定の class で動く
   - CC 残り枠：`$S "$X cad show usage"` の `claude/*` の行。5H・7D の残り（100 − 使用率）と RESETS
   - timer：`$S systemctl is-active orchd-sleep.timer`（`active` でなければ sleep 中に何も流れないと警告する）

起きたら `orchd mode clear --ns <ns>`（同じく `$S "$X …"`）で sleep を解除し、`/pickup` で再開する。`ai-failed` は起きてから owner / Orchestrator が見る。
