# handoff: dev-env-upgrade
最終更新: 2026-09-28 18:00 JST
目的: Mission（docs/mission.md）= 数十の project で agent が 24/7 開発、コスト最小・資源と使用枠を使い切る。この repo はそのツールの詰め合わせ。全体 TODO は Issue #39（v0.2: #33 #34 #35 #36 #37 #38 #41 #52、v0.3: #26 #27、v0.4: #40）。
完了条件（直近）: AUTO で Claude Code cloud の Orchestrator（CCO）が orchd を使って ai Issue を PR にできる。
決定事項（詳細は ADR-0010/0011/0012、orchd/README.md、skills/orchestrate/SKILL.md）:
- NS = project。Orchestrator は NS ごと（必須）、常駐、落ちたら owner が再開、Remote Control の URL ですぐ attach。MODE: AUTO=CCO（cloud）、URGENT=手元の local。
- 役割: Issue 管理と class 分類 = Orchestrator（LLM）、容量・usage = cad、割り振り = orchd（pick / place / dispatch / status / mode、答えを JSON で返す）。流れ `orchestrator → orchd pick → place → dispatch → orchestrator`、完了は subagent の通知。Issue の状態は GitHub のラベルだけ（ルールは skill）。
- 振り分け = policy の決定木（判断は分類だけ）。AUTO: claude-cloud → opencode×gce-spot → opencode×gce-std → local。cap はガードレール（lease はやらない）。NS ごとの予算は数値を持ち実績を後で追う（#52）。
- 実行先: claude-cloud = NS の worker cloud セッションへ `claude -p --cloud`。opencode = 停止中の e2-medium Spot / 通常 VM を必要時に start → docker run → タスク後すぐ自動停止（#53 merged）。
- デプロイ: GCP suggestorder-dev、cad-2（e2-micro COS、Swarm 1 台、GHCR :main を 5 分ごとに追従・ヘルス失敗で自動ロールバック、IAP SSH のみ、外部 IP あり）。秘密は Secret Manager（イメージ内 fetch-auth）。Claude は cad ユーザーで VM に /login 済み（a12e00a7）。main は ruleset で PR 必須。この repo は Claude が merge してよい。
- 調査は opencode（`opencode run`、並列不可）、判断確認は Fable。
却下: Hatchet（v0.4 で再検討）、GitHub Actions 経路（撤去）、lease、LLM による配置、cloud の VM を常時起動、Cloud Run（VM が安い）、suspend/resume（再起動より遅い）。
現在の状態:
- main = 6bb15dd。open PR なし。担当 Task: #63（VM）#69（worker、merged 済みなら完了確認）#70（orchd serve、一時停止中。worktree ../codingagentenv-serve は未使用なので削除可）。
- 未決の最大論点: **CCO → orchd の通信方式**。案 A = GitHub を伝言板（公開なし、推奨）/ B = 直接 HTTPS API（開放 443 か Cloudflare Tunnel+Access か Tailscale）/ C = MCP / D = Pub/Sub / E = VM がループ。Fable に全文脈（scratchpad の fable-brief-cco-orchd.md）で判断を依頼中（結果未受領）。
- VM 共通の不足: cad/orchd イメージに gh 無し、VM に GitHub token 無し（#53 の worker 用 PAT の仕組みを流用予定）。
次の一手:
1. Fable の結果（CCO→orchd の方式）を owner に提示し決めてもらう。未着なら同じ依頼を brief で再実行。
2. 決まった方式で実装（共通: イメージに gh、GitHub PAT を Secret Manager → VM）。
3. owner 作業待ち: `bash deploy/gcp/secrets.sh`（opencode + GitHub PAT）、cad-2 の SA 付け替えと `add-metadata startup-script=deploy/gcp/startup.sh`、`deploy/gcp/create-worker.sh`、NS の cloud worker セッション作成（`claude --cloud`）→ cad/config/namespaces.json の cloudWorkerSession、`bin/codingenv install`（skills/orchestrate の symlink）。
4. 最初の実走: この repo で ai Issue を 1 件、`orchd mode set urgent --ns default` で local から通す。
注意・未解決: cad-2 の startup ログに swarm join token が出る（2377 は非公開）。古い VM cad-1 は停止中、削除は owner 確認後。未コミットの他変更あり（.claude/token-usage.jsonl ほか untracked）。
