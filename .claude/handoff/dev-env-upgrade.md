# handoff: dev-env-upgrade
最終更新: 2026-09-27 16:30 JST
目的: ローカル = Control Plane (Orchestrator)、クラウド = Execution Plane (Worker) に分けた並列開発環境を作る。元アイデアは owner が貼った「dev dispatch + Worker image + クラウド Worker」構想 (GitHub 上で実行する前提)。
完了条件: Issue #10 の設計が ADR で確定 → c8 (dev-dispatch) で Cloud Run Jobs 1 台に 1 task を通し、PR が出て Orchestrator が検証できる。
決定事項:
- 動的な値は cad / 環境 / spec から決定的に取る (毎回 Prompt で取ると揺れて検証できない)。ルール文は方針のみ。
- cad = repo 内の Go 製メタデータアプリ (単一バイナリで Worker に配れる、常駐メモリが小さい)。マシンに 1 つ、リクエストで ns を指定、レコード最小単位は サービス×アカウント (同一マシンで複数アカウント運用)、capacity はマシン共有 (ns ごとに立てると二重に数える)。ADR-0008。
- 配置 = 分類 (Claude) + 割り当て (制約で絞る→規則で点数、決定的、ML なし)。小さく速く説明できるため。初期配置は予測せず、各 Subagent / Orch がこまめに cad を見る (消費量は事前に見積もれない)。
- merge 前チェックはアプリ依存 (対象 repo の CI が正、spec の done は事前確認)。ハーネスはリソースの関門だけ持ち、cad が担う (未実装)。
- キュー = GitHub Issue。task spec は JSON (Orch が生成する形式なので人の書きやすさ不要)。Worker の Agent は Claude Code のみ。最初の e2e は簡単な Cloud Run Jobs、project は一旦 suggestorder-dev。
- ロジックを含むものは擬似コードを owner に見せて承認後に実装 (シェルのロジックが読めなくなったため)。
却下した案: agent-gate (アプリ依存のチェックをハーネスが持つのは筋違い、自作ロックで P1 が止まらず) / cad を ns ごとに起動 (容量の二重計上) / OSS 採用 (4 要件を満たすものなし) / クラウドキュー自作 (GH Issue で足りる) / 機械学習の配置 (データ無し、規則で十分) / TS で cad (単一バイナリで配れない)。
現在の状態:
- merge 済: PR #2-#8, #11, #12 (#9 close)。open PR なし。担当 Task ID: なし (#30-#41 はすべて completed で Task list から消えている)。
- Issue #10 (設計見直し、未着手): Agent/Computer 登録、cad の ns・サービス×アカウント、cad の関門 (枠の貸し出し)、devcontainer 実行環境、キュー = GH Issue (ADR-0002 更新)、spec に acceptance / permissions / constraints.no_prod_write、将来 Dashboard / 分析。
- 設計メモ: .claude/design/placement-strategy.md (Fable)。ChangeGraph: .claude/changegraph/dev-env-upgrade.yaml (残り c8, c9)。
- follow-up (P2): bootstrap の結果未書き込み / SIGTERM 未転送 / stderr 溜め込み (c8 で対応)、cad の worker poll が同期、codex-local の quota に resetAt 無し。
次の一手:
1. Issue #10 を読み、ADR-0010 (Agent/Computer 登録、ns、サービス×アカウント、関門) の草案と擬似コードを書いて owner に見せる。ADR-0002 / 0008 / 0009 の更新点も列挙する。
2. 承認後、ChangeGraph を作り直して dispatch。その後 c8 (dev-dispatch + Cloud Run Jobs provider) で 1 task を通す。
注意・未解決の質問:
- global の install が一時 worktree ../codingagentenv-pr-6 を指している。消すと全プロジェクトの hook / skill が壊れる。main 相当の checkout (今は branch feature/ok = 8522968) で `bin/codingenv install` をやり直してから worktree を片付ける (owner 未実施)。
- 片付け待ちの worktree: ../codingagentenv-{c1,c2,c3,c4,c5,c6,c7,c10,c11,rm-gate,pr-6}。
- GitHub の既定ブランチが feat/task-queue のまま (PR は必ず --base main)。main に変えるか未回答。gh pr edit は失敗するので本文は gh api -X PATCH -F body=@file。
- merge は今の head に独立レビューがあることが条件 (古い head だと auto mode が拒否)。
- 未コミットの他変更あり (.claude/token-usage.jsonl、node_modules/、package-lock.json、.claude/changegraph/、.claude/design/)。
