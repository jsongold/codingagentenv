---
name: iwokeup
description: owner が起きたときに、cad-2 の `orchd-sleep.timer` を止めて sleep を解除し、夜間の結果（PR・owner 判断待ちの Issue コメント）を gh で一覧にして /pickup を促す。/igosleep の対。timer を止める本番操作は owner が実行する。owner が `/iwokeup` と打ったときだけ使う（自然文では起動しない）。
disable-model-invocation: true
---

# iwokeup — sleep 解除と夜間結果の一覧

`/igosleep` の対（ADR-0014, 0015）。sleep の on/off は cad-2 の `orchd-sleep.timer` の start/stop そのもの。起きたら timer を止めて、手元の Orchestrator（このセッション）に戻す。ai ラベル・`orchd mode`・`orchd wake` は使わない。

1. sleep を解除する（本番操作なので Claude は実行しない。owner にコピペで渡す）。`stop` だけだと VM の再起動で timer が復活した実績があるので `disable --now`。先に開始時刻（手順 2 の SINCE）を控えてから止める：
   ```
   ! S="gcloud compute ssh cad-2 --project suggestorder-dev --zone us-central1-a --tunnel-through-iap --"
   ! $S systemctl show orchd-sleep.timer -p ActiveEnterTimestamp     # sleep 開始時刻を控える（止める前）
   ! $S sudo systemctl disable --now orchd-sleep.timer
   ! $S systemctl is-active orchd-sleep.timer                        # inactive になること
   ! $S systemctl list-timers orchd-sleep.timer                      # 一覧に出ないこと
   ```
   `is-active` が `active` のままなら、結果を伝えて 2 へは進まず owner の対応を待つ。次に寝るときは `/igosleep`（start し直す）。
2. 夜間の結果を一覧にする（`gh`、読み取りのみ）。`<repo>` と `SINCE`（手順 1 の時刻を `2026-09-29T22:00:00Z` 形式にしたもの。控えが無ければ前夜の就寝時刻を owner に聞く）を決める。`<repo>` は `/igosleep` と同じ順：cad-2 の登録の `default.repo`、無ければ手元の `cad/config/namespaces.json`（無ければ `.example.json`）の `default.repo`、無ければ `gh repo view --json nameWithOwner -q .nameWithOwner`。
   ```bash
   gh pr list --repo <repo> --state all --search "updated:>=$SINCE" --json number,title,state,url,createdAt,mergedAt,updatedAt
   gh issue list --repo <repo> --state open --search "updated:>=$SINCE comments:>0" --json number,title,url,updatedAt
   ```
   - PR：作成・更新・merge された順に `#n title`、state（`MERGED` / `OPEN` はレビュー待ち / `CLOSED`）、URL
   - Issue：owner 判断待ちの候補。各 Issue の最後のコメント（`gh issue view <n> --repo <repo> --comments`）を読み、質問・ブロック報告のものだけを 1 行の理由つきで挙げる。milestone 付きの Issue は対象外
   - 該当なしの組は省く。取れない範囲（CCO の内部状態など）は推測で書かない
3. 最後に「`/pickup` で文脈を戻してから、上の判断待ちを見る」と 1 行で促す。PR の merge・Issue の操作はここではしない。
