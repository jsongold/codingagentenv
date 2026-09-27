# ADR-0010: 配置は (Agent, Computer) の組を選ぶ。判断は Orchestrator の class 分類だけにし、選択は `cad` の `POST /v1/place` が決定的に返す

- 日付：2026-09-27
- 状態：提案

## 背景
ADR-0008 / 0009 は「provider の固定一覧（`policy.providers.allowed`）」と「1 台の `cad`」を前提にし、provider の選択を `dev-dispatch` に置いた。Issue #10 で owner の意図と違うことが分かった：
- 実行主体（Agent：サービス × アカウント）と実行場所（Computer：この Mac、GCE、Cloud Run、sandbox など）は別物。サブスクの使用枠は Agent 単位、空き・料金・能力は Computer 単位で持つ。
- 同じ Mac で Claude Code の複数アカウントを運用している。アカウントは既に aienv（ディレクトリごとに設定ストアを切り替える shim）で管理している。
- 配置に要るデータ（policy・capacity・workers・quota）は既に `cad` が持っている。

設計メモ（`.claude/design/placement-strategy.md`、未コミット）は「slots=0 の Mac があるので local-first を既定にしない」としたが、この ADR の owner 決定（local slots は固定上限）で置き換える。

## 決定
- **判断は1つだけ**：Orchestrator（Claude）が task の class を分類して spec に書く。class は `light-edit` / `gate-heavy` / `needs-db` / `long-running` / `urgent` / `retry`。それ以外（Agent・Computer の選択、admission、検証）は決定的な情報（`cad` の値、aienv、policy、spec）から機械的に決まる。分類の誤りは Orchestrator が class を付け替えて再 dispatch して直す。検証も機械的（対象 repo の CI + spec の `done[]`）。
- **配置は (Agent, Computer) の組を選ぶ**。
  - Agent = サービス × アカウント（例 `claude/3f9a1c0e`、`codex/<id>`）。アカウント id は aienv の store id（8桁 hex。`aienv resolve <app> [dir]` の出力パスの basename、メタデータは `<store>/.aienv-meta`）。aienv の binding が無いディレクトリは `claude/default`（`~/.claude`）。
  - アカウントの正本は aienv。`cad` は読むだけで、登録・変更はしない。
  - aienv の shim は `CLAUDE_CODE_OAUTH_TOKEN` を unset する。よって ADR-0009 の「Worker は token 固定で認証」は成り立たない。Worker は `CLAUDE_CONFIG_DIR` 型の store を使うか、shim を通らずに `claude` を起動する（どちらにするかは実装 PR で決める）。
- **Agent の向き不向きは表で持つ**。policy に `classAgents`（class → 許可する Agent のパターン。例 `needs-db: ["claude/*"]`）を足す。task ごとには判断しない。
- **`cad` のエンドポイント `POST /v1/place?ns=<ns>` として実装する**。入力は spec（JSON）。task の状態を持たない純関数（ADR-0002 / 0008 と整合）。同じ spec と同じ `cad` の値なら同じ答え。`ns` は必須で、無ければ 400（Issue #10）。
  - `200 {agent, computer, costUSD, reason[]}`：採用した組とその見積り料金、各段で落とした候補の理由
  - `409 {defer_until, reason}`：admission / 使用枠の窓で今は置けない。`cad` は待たない・キューを持たない。Orchestrator は Task を pending のまま `DEFER:` を説明欄に書き、後で再 dispatch する
  - `422 {reason}`：条件を満たす組が無い（起動しない）
- **アルゴリズム**
  ```
  place(spec, ns):
    pol    = policy(ns)
    agents = pol.classAgents[spec.class]
    pairs  = agents × (spec.allow ∩ pol.computers)
    keep p where fits(p.computer, spec.resources)
               ∧ caps(p.computer, spec.class)            # needs-db → db
               ∧ cost(p) ≤ spec.maxCostUSD
               ∧ (p.computer ≠ local ∨ slots ≥ 1)
               ∧ (strategy ≠ safe ∨ ¬preemptible(p.computer))
               ∧ window(p.agent).left ≥ est[spec.class] + reservePct
    if empty: 409 if the window filter emptied it (defer_until = earliest resetsAt) else 422
    if any p.computer == local: candidates = those          # local-first
    sort by strategy key:
      cheap : cost, preemptible, coldStart
      fast  : coldStart, cost
      safe  : cost, coldStart
      ranked: spec.order
    tie-break: (policy.agentPriority のサービス順, agent, computer)   # 既定 ["claude","codex","opencode"]、一覧に無いサービスは最後
    return first, reasons dropped per stage
  ```
- **usage は `cad` の collector が集める**（owner 決定：メタデータを作って配るのは `cad` だけ）。topic `usage`、間隔 `CAD_USAGE_EVERY`（既定 60s）。当面は claude のみ（優先度 claude > codex > opencode）。`policy.agents` の `claude/*` ごとに `claude -p "/usage" --output-format stream-json --verbose` を並列に実行し（モデル呼び出しなし・約2秒。`claude/default` は `CLAUDE_CONFIG_DIR` なし、`claude/<id>` は `~/.aienv/.store/<id>`）、store の `.claude.json` の `.cachedUsageUtilization` だけを読む（他のキーはアカウント情報なので読まない・出さない）。失敗・キー無しの Agent は `error` 付きで残し、place はそれを `usage unknown` として扱う。resetsAt を過ぎた窓は 0% とみなす。SSE の変化判定から `fetchedAt` を除く。
- **client は `cad get meta -ns <ns>`**（`cad get <topic> -ns <ns>`）。実行中の `cad` の `GET /v1/meta?ns=`（`/v1/<topic>?ns=`）を 2 秒 timeout で1回叩いて JSON を出すだけ。`-ns` が無ければ exit 2（通信しない）、daemon に届かなければ exit 1。サーバ側も `GET /v1/meta`・`/v1/<topic>` は `ns`（`^[a-z0-9-]+$`）必須で、無ければ 400（`/healthz`・`/v1/events` は対象外）。
- **local-first を既定にする**。local の slots は `CAD_SLOTS=5`（policy `maxSlots` 5）の固定上限。稼働中の ws は数えない。メモリの取り合いは macOS に任せる。1 slot = 1 ws（Claude Code セッション1つとその Subagent）。
- **同時実行**：2つの place が同時に来ると同じ空きを二重に数えうる。lease（関門）は実害が出るまで入れない。
- **Orchestrator は Claude Code のまま**（Task list と `VERIFIED:` の hook が強制できるのはここだけ、ADR-0002 / 0004）。Codex・opencode・Gemini などは Worker またはレビュアーとして使う。

## 検討して却下した案
| 案 | 却下理由 |
|---|---|
| 配置専用の daemon を別に立てる | 必要なデータ（policy・capacity・workers・quota）は既に `cad` にある。二重収集になる |
| ML で配置する | 決定的でなく検証できない（ADR-0008）。学習データ（実行記録）もまだ無い |
| task ごとに Claude が Agent / Computer を判断する | 結果が揺れる。判断は class 分類の1点に絞り、残りは表と `cad` の値で決める |
| local の slots に稼働中の ws を数える | 数え方が揺れ、取りこぼしで詰まる。固定上限 + macOS 任せで足りる |

aienv を別 repo のまま使うか、この repo に取り込むかは却下ではなく未決（下記）。

## 影響
- 良い影響：配置の答えが `cad` への1回の呼び出しで再現できる。Claude の判断が class の1点に絞られ、誤りの直し方（付け替えて再 dispatch）も決まる。アカウントの正本が aienv 1つになる
- 受け入れたトレードオフ：同時の place で二重計上がありうる。local は固定上限なのでメモリが詰まれば遅くなる。aienv に依存する
- 更新が要る ADR：
  - ADR-0008：`policy.providers.allowed` → Agent × Computer（`pol.computers`）+ `classAgents` に置き換える。`cad` の API に `POST /v1/place`（書き込み系の2つ目）と `ns` 必須を足す。capacity.go のメモリ式の slots は後で削除（この ADR では実装しない follow-up）
  - ADR-0009：Worker の認証（token 固定 → aienv の store）。spec に `class`・`acceptance`・`permissions`・`constraints.no_prod_write` を足す。provider の選択を `dev-dispatch` から `cad` の place へ移す（`dev-dispatch` は結果を適用するだけ）。strategy 名を `cheap` / `fast` / `safe` / `ranked` に揃える
  - ADR-0002：task の入口を GitHub Issue にする（Issue #10）。内蔵 Task list は Orchestrator セッション内の実行管理

## 未決
- aienv のコマンド名（`codingenv aienv` か `caenv` か）と、aienv をこの repo に取り込むか。
- aienv の binding の外にある worktree（例 `../<repo>-pr-N`）が別アカウントを継承する問題：(a) 運用で避ける、(b) aienv が git worktree の親 repo を辿って binding を引く。
- codex / opencode の usage collector（codex は app-server の `account/rateLimits/read` が候補）。それまで codex/opencode の Agent は `usage unknown`。usage の無い Agent は落とさず reason に `usage unknown` を残す。Computer の静的な属性（cost・coldStart・preemptible・caps・上限）は policy に書く。window の閾値は policy `placement.reservePct` / `placement.estPct`（仮値）。
- lease（関門）を入れる基準。

## 再検討する条件
- 同時 place の二重計上で実害（Worker の起動失敗、Mac の詰まり）が出たとき → lease を入れる
- class の付け替え・再 dispatch が頻発するとき → class の定義か `classAgents` を見直す
- 実行記録が溜まり、`est[class]` や strategy key を実測で決められるようになったとき
- Orchestrator の強制（Task list・hook）が Claude Code 以外でもできるようになったとき
