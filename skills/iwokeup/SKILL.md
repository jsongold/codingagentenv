---
name: iwokeup
description: owner が起きたときに、cad-2 の orchd mode を通常（sleep 解除）へ戻し、夜間の結果（PR・ai-failed・wip 残り・未着手）を `orchd issue list` で一覧にして /pickup を促す。/igosleep の対。引数 [ns] は任意（既定 default）。
argument-hint: "[ns]"
disable-model-invocation: true
---

# iwokeup — sleep 解除と夜間結果の一覧

`/igosleep` の対（ADR-0014）。sleep 中は cad-2 の timer が `orchd dispatch --pending` を回し、mode が `sleep` のときだけ動く。起きたら mode を通常に戻して、手元の Orchestrator（このセッション）に戻す。引数 `<ns>` は任意（既定 `default`）。

1. cad-2 の mode を通常へ戻す。通常 = mode ファイルなし（既定の `auto`、`orchd/README.md` の MODE）なので `set` ではなく `clear`。cad-2 には `deploy/gcp/README.md` の IAP SSH + `docker exec` で入る：
   ```bash
   S="gcloud compute ssh cad-2 --project suggestorder-dev --zone us-central1-a --tunnel-through-iap --"
   $S "sudo docker exec \$(sudo docker ps -q -f label=com.docker.swarm.service.name=cad) orchd mode clear --ns <ns>"
   $S "sudo docker exec \$(sudo docker ps -q -f label=com.docker.swarm.service.name=cad) orchd mode show --ns <ns>"   # mode が sleep でないこと
   ```
   - `show` が `sleep` のまま（`modeSource` が `file:global` など）なら、消さずに owner に伝える。ssh が通らなければ理由を伝え、2 へ進む（一覧は手元で取れる）。
2. 手元で `orchd issue list --ns <ns>` → `[{number, title, state, pr, prState}]`（milestone 付きの Issue は出ない）。Issue は ns の repo のもので、手元の cwd の repo とは限らない。`gh` で Issue を見るときは必ず `--repo <repo>` を付ける。`<repo>` は `/igosleep` と同じ順で決める：cad-2 の登録（`$S "sudo docker exec \$(sudo docker ps -q -f label=com.docker.swarm.service.name=cad) cat /data/cad/config/namespaces.json"`）の `<ns>.repo`、無ければ（ssh が通らないときも）手元の `cad/config/namespaces.json`（無ければ `.example.json`）の `<ns>.repo`、無ければ `gh repo view --json nameWithOwner -q .nameWithOwner`。次の順に短く一覧にする（該当なしの組は省く）：
   - PR あり：`#n title` と PR の URL・`prState`（`OPEN` はレビュー待ち）。state は問わない
   - ai-failed（PR なし）：owner の判断が要る。理由は Issue の最後のコメント（`gh issue view <n> --repo <repo> --comments`）から 1 行で
   - wip（PR なし）：止まっている可能性。`orchd: dispatched to <computer> at <時刻>` のコメント（同じく `gh issue view <n> --repo <repo> --comments`）があれば時刻も
   - pending：未着手のまま残った件数と番号
3. 最後に「`/pickup` で文脈を戻してから、ai-failed と wip を見る」と 1 行で促す。PR の merge・ラベルの付け替えはここではしない。
