---
name: iwokeup
description: owner が起きたときに、cad-2 の `orchd-sleep.timer` を止めて sleep を解除し、夜間の結果（PR・owner 判断待ちの Issue コメント）を gh で一覧にして /pickup を促す。/igosleep の対。timer を止める本番操作は owner が実行する。owner が `/iwokeup` と打ったときだけ使う（自然文では起動しない）。
disable-model-invocation: true
---

# iwokeup — sleep 解除と夜間結果の一覧

`/igosleep` の対（#101、#102）。sleep の on/off は cad-2 の `orchd-sleep.timer` の start/stop そのもの。起きたら timer を止めて、手元の Orchestrator（このセッション）に戻す。ai ラベル・`orchd mode`・`orchd wake` は使わない。

1. sleep を解除する（本番操作なので Claude は実行しない。owner にコピペで渡す）。`stop` だけだと VM の再起動で timer が復活した実績があるので `disable --now`。先に開始時刻（手順 2 の `<SINCE>`）を控えてから止める。`!` の行は 1 行ずつ別の shell で動くので、変数に入れず各行に ssh をそのまま書く：
   ```
   ! gcloud compute ssh cad-2 --project suggestorder-dev --zone us-central1-a --tunnel-through-iap -- systemctl show orchd-sleep.timer -p ActiveEnterTimestamp   # sleep 開始時刻を控える（止める前）
   ! gcloud compute ssh cad-2 --project suggestorder-dev --zone us-central1-a --tunnel-through-iap -- sudo systemctl disable --now orchd-sleep.timer
   ! gcloud compute ssh cad-2 --project suggestorder-dev --zone us-central1-a --tunnel-through-iap -- 'n=0; while [ "$(systemctl is-active orchd-sleep.service)" = activating ] && [ $n -lt 60 ]; do n=$((n+1)); sleep 10; done; echo "timer=$(systemctl is-active orchd-sleep.timer) service=$(systemctl is-active orchd-sleep.service)"'
   ```
   最後の行は、timer を止めた時点で実行中だった `orchd-sleep.service`（oneshot。CCO へ指示を送る dispatch）が終わるのを最大 10 分待ち、状態を出す。`timer=inactive` かつ `service` が `activating` 以外なら 2 へ。どちらかが違えば、結果を伝えて 2 へは進まず owner の対応を待つ。
   service が終わっても、送った指示で CCO（cloud worker セッション）がまだ作業中のことがある。claude.ai/code でそのセッションが止まっているのを owner に確かめてもらってから 3 の `/pickup` に進む（手元と CCO が同時に同じ repo を触らないため）。次に寝るときは `/igosleep`（start し直す）。
2. 夜間の結果を一覧にする（`gh`、読み取りのみ）。`<repo>` と `<SINCE>`（手順 1 の時刻を `2026-09-29T22:00:00Z` 形式にしたもの。控えが無ければ前夜の就寝時刻を owner に聞く）を決め、下のコマンドに値を直接埋めて実行する。`<repo>` は次の順で決める：cad-2 の登録の `default.repo`、無ければ手元の `cad/config/namespaces.json`（無ければ `.example.json`）の `default.repo`、無ければ `gh repo view --json nameWithOwner -q .nameWithOwner`。
   ```bash
   gh pr list --repo <repo> --state all --limit 200 --search "updated:>=<SINCE>" --json number,title,state,isDraft,reviewDecision,mergeStateStatus,url,createdAt,mergedAt,updatedAt
   gh issue list --repo <repo> --state open --limit 200 --search "updated:>=<SINCE> comments:>0 no:milestone -label:doc:spec" --json number,title,url,updatedAt
   ```
   - PR：作成・更新・merge された順に `#n title`、state（`MERGED` / `OPEN` / `CLOSED`）、URL。`OPEN` は `isDraft`・`reviewDecision`・`mergeStateStatus` の値をそのまま添え、「レビュー待ち」などと決めつけない
   - Issue：owner 判断待ちの候補。各 Issue の最後のコメント（`gh issue view <n> --repo <repo> --comments`）を読み、質問・ブロック報告のものだけを 1 行の理由つきで挙げる（milestone 付きと `doc:spec` は検索で除いている）
   - 該当なしの組は省く。取れない範囲（CCO の内部状態など）は推測で書かない
3. 最後に「`/pickup` で文脈を戻してから、上の判断待ちを見る」と 1 行で促す。PR の merge・Issue の操作はここではしない。
