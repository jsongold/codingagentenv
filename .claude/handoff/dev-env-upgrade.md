# handoff: dev-env-upgrade
最終更新: 2026-09-27 19:40 JST
目的: ローカル Mac = Orchestrator、Worker を local / クラウドに振り分ける開発環境。直近ゴールは「寝ている間も稼働させる」。
完了条件: (1) cad place v0.0.1（決定木）が main、(2) Mac 睡眠時に GitHub Actions + opencode が `ai` ラベル Issue から PR を出し CI が通る夜を確認。
決定事項（詳細 ADR-0010）:
- 配置 = 分類問題。1段目 task→class は Orchestrator(LLM)、2段目 (class, metadata)→(agent, computer) は cad がコードで決定木評価。ルールは policy.rules のデータ（上から最初に合う）。strategy/cost 並べ替えは過剰で廃止。
- 戦略: local で claude（窓に余裕の間）→ 尽きたら opencode（OpenCode Go、model 動的・直近 deepseek）→ 全滅 defer。codex はレビュー専用。
- Mac 睡眠時: GitHub Actions のみ（Fable 推奨 A）。owner が就寝前に手で `ai` ラベル、`wip` をロック、concurrency 1、timeout 60。この repo だけで1週間試す。cloud cad / heartbeat / 自動ラベル / opencode usage collector は作らない。
- usage は cad が定期収集: claude = `claude -p /usage` → <store>/.claude.json cachedUsageUtilization（無料）、codex = `codex app-server` account/rateLimits/read（無料、byLimitId 走査、欠損窓は null）。
- agents: claude/a12e00a7, claude/b1c8ef41, codex/2e33b72a（plus、5h 窓なしが正しい）, opencode/996c87ae。claude/default（a12e00a7 と同一）・codex/default（~/.codex team、別アカウント）は除外。
- slots は CAD_SLOTS=5 固定（1 slot = 1 ws）、メモリは macOS 任せ。aienv は repo 取り込み検討中（未決）。
- 調査・情報収集は opencode（`opencode run`）に任せ、main が一次ソースで検証。並列起動は snapshot ロック競合で固まる→1本ずつ。
却下: OSS 採用（ADR-0008）、配置専用 daemon、ML 配置、LLM に配置判断させる、稼働 ws のカウント、cloud cad（今は）、`/status` 画面スクレイプ。
現在の状態:
- main = 260ef96（PR #13-#19 merge 済、v0.1.0 は owner 指示で Codex レビュー免除）。main checkout は docs/handoff branch。
- 担当 Task: #52 in_progress（place v0.0.1、subagent が worktree ../codingagentenv-rules / branch feat/place-rules で実装中、PR は出すが merge しない＝owner レビュー待ち）。#44 pending（c8 着手時）。
次の一手:
1. #52 の PR を確認: `gh pr list --head feat/place-rules`。`cd ../codingagentenv-rules/cad && go test -count=1 ./...`、owner に確認コマンドを渡し、承認後 merge。
2. GHA workflow PR（Fable 案）: `.github/workflows/opencode.yml`（issues labeled `ai`、`wip` ロック、concurrency 1、timeout 60、anomalyco/opencode/github@latest、model opencode-go/deepseek-v4-pro、use_github_token: true + PAT）、Orchestrator 規則「ai/wip 付き Issue は拾わない」、ADR 追記。owner が secret `OPENCODE_API_KEY` と fine-grained PAT（contents/PR/issues write）を登録。
注意・未解決:
- 要確認: GITHUB_TOKEN で作った PR は CI を起動しない（Fable 指摘、docs 未確認）。OpenCode Go の CI 利用規約（未確認）、`/zen/go/v1/usage`（source のみ、docs 無し）。
- 未決: aienv 取り込みとコマンド名（codingenv aienv / caenv）、worktree がバインド外で別アカウント継承する問題。
- 片付け待ち worktree 多数（`git worktree list`）。GitHub 既定ブランチは feat/task-queue のまま（PR は必ず --base main、本文編集は gh api PATCH）。
- 未コミットの他変更あり（token-usage.jsonl、node_modules、package-lock.json、.claude/changegraph、.claude/design、research_notes）。
