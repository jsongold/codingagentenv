# handoff: dev-env-upgrade
最終更新: 2026-09-28 01:10 JST
目的: ローカル Mac = Orchestrator で task を (agent, computer) に振り分けて並列開発。直近ゴールは「寝ている間も稼働させる」。
完了条件: (1) PR #22 merge 後、`ai` ラベル Issue から opencode が PR を出し CI が通る夜を確認、(2) dev-dispatch（#44）で place の結果どおり subagent / `opencode run` を自動起動。
決定事項（詳細 ADR-0010）:
- 配置 = 分類問題。1段目 task→class は Orchestrator(LLM、基準は policy.classes[].criteria)、2段目は cad の決定木（policy.rules、上から最初に合う）。LLM に配置させない（揺れる・検証不可）。
- 設定はすべて .agent/policy.json（classes / rules / runners / collect、hot reload）。日々変わるため。
- Claude は常に subagent（= Orchestrator 自身のアカウント、place に self を渡す）。窓が尽きたら opencode（OpenCode Go、model opencode-go/deepseek-v4-pro、`opencode run`）。codex はレビュー専用。
- Mac 睡眠時は GitHub Actions のみ（Fable 推奨）: owner が就寝前に手で `ai` ラベル、`wip` ロック、concurrency queue: max、timeout 60、share: false、action を SHA pin。cloud cad / heartbeat / 自動ラベル / opencode usage collector は作らない。
- usage: claude = `claude -p /usage` → .claude.json cachedUsageUtilization、codex = app-server account/rateLimits/read（無料）。agents: claude/a12e00a7, claude/b1c8ef41, codex/2e33b72a（plus、5h 窓なしが正）, opencode/996c87ae。claude/default・codex/default は除外。
- 調査は opencode（1本ずつ、並列は snapshot ロックで固まる）、レビューは Fable/Codex。main の変更は常に PR 経由。
却下: strategy/cost 並べ替え型 place（過剰）、LLM 配置、cloud cad（今は）、`/status` スクレイプ、OSS 採用。
現在の状態:
- main = c2d3cf6（PR #13-#21 merge 済）。checkout は docs/handoff-dev-env-upgrade（この handoff）。branch chore/gitignore-env（`.env` ignore、未 push）。
- PR #22 open（worktree ../codingagentenv-gha、head 12df258）: Codex P1×2 + Fable 指摘全修正済、actionlint ok、owner の merge 待ち。
- secret: OPENCODE_API_KEY 登録済。OPENCODE_GH_PAT 未登録。
- 担当 Task: #54 in_progress（PR #22）、#44 pending（dev-dispatch）。
次の一手:
1. owner に確認: PR #22 merge 可否、main 保護（ruleset "protect main" id 24066437 は disabled、PR 必須なし）を有効化するか、PAT 登録。
2. merge 後の実地テスト: `gh issue create --title "test: opencode sleep run" --body "README の配置ロジック節の誤字を1つ直して" --label ai` → `gh run list --workflow opencode-sleep.yml --limit 1` → PR 確認。
3. #44 dev-dispatch: place(self=CLAUDE_CONFIG_DIR basename) → runner.mode subagent なら Agent、process なら worktree で `opencode run --model <model>`。
注意・未解決:
- repo は PUBLIC、main は無保護（agent が PAT で main に直 push 可能）。
- chore/gitignore-env と docs/handoff-dev-env-upgrade を PR にするか未決。aienv 取り込み・コマンド名も未決。
- 未コミットの他変更あり（.claude/token-usage.jsonl ほか untracked）。
