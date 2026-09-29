---
name: igosleep
description: owner が寝る前に、cad-2 の登録に cloud worker セッション（CCO）があるのを確かめ、cad-2 の `orchd-sleep.timer` を動かして sleep に入り、/handoff して手元のセッションを /clear できる状態にする。/iwokeup の対。timer を動かす本番操作は owner が実行する。owner が `/igosleep` と打ったときだけ使う（自然文では起動しない）。
disable-model-invocation: true
---

# igosleep — 寝る前に sleep に入る

`/iwokeup` の対（#101、#102）。sleep の on/off は cad-2 の `orchd-sleep.timer` の start/stop そのもの。timer が動いている間、orchd が CCO（cloud worker セッション）を定期的に起こし、何をやるかは CCO がプロジェクトの文脈から決める。ai ラベル・`orchd mode`・`orchd wake` は使わない。cad-2 の操作は本番なので Claude は実行せず、owner にコピペで渡す。`!` の行は 1 行ずつ別の shell で動くので、変数に入れず各行に ssh をそのまま書く。

1. CCO のセッションが登録されているか確かめる（読み取りのみ）。owner に次の行を実行してもらう：
   ```
   ! gcloud compute ssh cad-2 --project suggestorder-dev --zone us-central1-a --tunnel-through-iap -- sudo cat /var/lib/cad/cad/config/namespaces.json   # container の /data/cad/config/namespaces.json
   ```
   `default.cloudWorkerSession` が空でなければ 2 へ。空・キーが無い・ファイルが無いときは、owner に「claude.ai/code で GitHub repo（`default.repo`）を選んで新しいセッションを作り、その ID を cad-2 の `namespaces.json` の `default.cloudWorkerSession` に登録して」と頼み、ここで止める（CLI の `claude --cloud` では作らない。bundle になり push できない）。セッションはこの 1 つだけを使う。アカウントごとに持つかは未決（#65）。
2. sleep に入る（timer を動かす）。reboot で止まらないよう `enable --now`：
   ```
   ! gcloud compute ssh cad-2 --project suggestorder-dev --zone us-central1-a --tunnel-through-iap -- sudo systemctl enable --now orchd-sleep.timer
   ! gcloud compute ssh cad-2 --project suggestorder-dev --zone us-central1-a --tunnel-through-iap -- systemctl is-active orchd-sleep.timer
   ```
   `active` なら 3 へ。違えば結果を伝えて止める（sleep 中に何も起きない）。
3. `/handoff` の手順で handoff を書き出してコミットし、「このセッションは `/clear` してよい。起きたら `/iwokeup`」と 1 行で伝えて終わる。
