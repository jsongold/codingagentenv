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
- **判断は1つだけ**：Orchestrator（Claude）が task の class を分類して spec に書く。class は `policy.classes` の名前（seed は `light-edit` / `gate-heavy` / `needs-db` / `long-running` / `urgent` / `retry`）。それ以外（Agent・Computer の選択、admission、検証）は決定的な情報（`cad` の値、aienv、policy、spec）から機械的に決まる。分類の誤りは Orchestrator が class を付け替えて再 dispatch して直す。検証も機械的（対象 repo の CI + spec の `done[]`）。
- **配置は (Agent, Computer) の組を選ぶ**。
  - Agent = サービス × アカウント（例 `claude/3f9a1c0e`、`codex/<id>`）。アカウント id は aienv の store id（8桁 hex。`aienv resolve <app> [dir]` の出力パスの basename、メタデータは `<store>/.aienv-meta`）。aienv の binding が無いディレクトリは `claude/default`（`~/.claude`）。
  - アカウントの正本は aienv。`cad` は読むだけで、登録・変更はしない。
  - aienv の shim は `CLAUDE_CODE_OAUTH_TOKEN` を unset する。よって ADR-0009 の「Worker は token 固定で認証」は成り立たない。Worker は `CLAUDE_CONFIG_DIR` 型の store を使うか、shim を通らずに `claude` を起動する（どちらにするかは実装 PR で決める）。
- **配置は 2 段の分類問題**。段 1（task 文 → class）は Orchestrator（LLM）。段 2（(class, メタデータ) → (Agent, Computer)）は policy の `rules`（データとして持つ順序付きの決定リスト）を評価する決定的なコード。task ごとには判断しない。v0.0.1 で strategy/cost 方式（`classAgents`・`agentPriority`・strategy・cost・coldStart・preemptible・local-first の特例）を置き換えた。`policy.computers` の属性は記録として残すが place は使わない。
- **place の置き場所は ADR-0011 で `cad` から `orchd`（`orchd place --class <c> [--self ..] [--ns ..]`、exit 0 / 3 = 409 / 4 = 422 / 2 = 400）に移した**。以下の `POST /v1/place` は移す前の記述で、判定の中身は同じ。当初は **`cad` のエンドポイント `POST /v1/place?ns=<ns>` として実装した**。入力は `{"class": ..., "self"?: "<service>/<account>"}`（`policy.classes` に無い・空の class、形式違いの self は 400。他のフィールドは無視）。task の状態を持たない純関数（ADR-0002 / 0008 と整合）。同じ spec と同じ `cad` の値なら同じ答え。`ns` は必須で、無ければ 400（Issue #10）。
  - `200 {agent, computer, rule, reason[], runner}`：採用した組、採用した rule の番号、落とした候補の理由、Agent のサービスの起動方法（`policy.runners`）
  - `409 {defer_until, reason}`：使用枠の窓だけで塞がった rule があり今は置けない。`cad` は待たない・キューを持たない。Orchestrator は Task を pending のまま `DEFER:` を説明欄に書き、後で再 dispatch する
  - `422 {reason}`：条件を満たす組が無い（起動しない）
- **アルゴリズム**（policy.rules = `[{agent: <path.Match パターン>, computer: <名前>, class?: [...]}]`、先勝ち）
  ```
  place(spec, ns):
    for i, r in pol.rules:
      skip if r.class ∧ spec.class ∉ r.class              # "rule i: class"
      skip if r.computer ∉ pol.computers ∪ {local}
      skip if r.computer == local ∧ slots < 1             # "rule i: no local slot"
      r.agent == self: spec.self ∉ pol.agents → skip        # "rule i: self unknown"
      for a in ({spec.self} if r.agent == self else sorted(pol.agents) matching r.agent):
        usage 無し / error / stale → return 200 (a, r.computer, i)   # "usage unknown"
        各窓（null は無視、resetsAt 経過は 0%）: used% + classes[class].estPct ≤ 100 − reservePct
          → 全部 ok なら return 200 (a, r.computer, i)
          → 超えた窓は "a: 5h window" / "a: 7d window"
    窓だけで塞がった rule があれば 409 (defer_until = 塞がった Agent が空く最も早い時刻) else 422
  ```
  既定の seed は `[{self, local}, {opencode/*, local}]`（Orchestrator 自身の claude を使い切ったら opencode）。
- **usage は `cad` の collector が集める**（owner 決定：メタデータを作って配るのは `cad` だけ）。topic `usage`、間隔 `CAD_USAGE_EVERY` > policy `collect.usage.every`（既定 60s）。対象は claude と codex（優先度 claude > codex > opencode。opencode は未対応で `usage unknown`）。`policy.agents` の `claude/*` ごとに `claude -p "/usage" --output-format stream-json --verbose` を並列に実行し（モデル呼び出しなし・約2秒。`claude/default` は `CLAUDE_CONFIG_DIR` なし、`claude/<id>` は `~/.aienv/.store/<id>`）、store の `.claude.json` の `.cachedUsageUtilization` だけを読む（他のキーはアカウント情報なので読まない・出さない）。失敗・キー無しの Agent は `error` 付きで残し、place はそれを `usage unknown` として扱う。resetsAt を過ぎた窓は 0% とみなす。SSE の変化判定から `fetchedAt` を除く。`codex/*` は Agent ごとに `codex app-server`（`CODEX_HOME=~/.aienv/.store/<id>`、`codex/default` は `CODEX_HOME` なし＝`~/.codex`。`OPENAI_API_KEY`・`CODEX_ACCESS_TOKEN` は子に渡さない。bin は `CAD_CODEX_BIN` > `/opt/homebrew/bin/codex` > PATH）を起動し、stdio の JSON-RPC で `initialize` → `initialized` → `account/rateLimits/read` を送って `rateLimitsByLimitId` の全 snapshot の `primary/secondary` だけを読む（map が無い・空なら `rateLimits`）（モデル呼び出しなし・約0.5秒・timeout 20s）。窓は長さで振り分ける（300分 → fiveHour、10080分 → sevenDay、それ以外の長さは無視。同じ長さが複数あれば usedPercent の最大。どの limit にも無い窓は `null` で、place はその窓で絞らない）。app-server が失敗したら `$CODEX_HOME/sessions/**/rollout-*.jsonl` の最新ファイルの最後の `token_count` の `rate_limits.primary/secondary` を同じ規則で読み、`stale` を付ける（place は使わない）。両方失敗なら `error`。
- **client は `cad get meta -ns <ns>`**（`cad get <topic> -ns <ns>`）。実行中の `cad` の `GET /v1/meta?ns=`（`/v1/<topic>?ns=`）を 2 秒 timeout で1回叩いて JSON を出すだけ。`-ns` が無ければ exit 2（通信しない）、daemon に届かなければ exit 1。サーバ側も `GET /v1/meta`・`/v1/<topic>` は `ns`（`^[a-z0-9-]+$`）必須で、無ければ 400（`/healthz`・`/v1/events` は対象外）。
- local の slots は `CAD_SLOTS=5`（policy `maxSlots` 5）の固定上限。稼働中の ws は数えない。メモリの取り合いは macOS に任せる。1 slot = 1 ws（Claude Code セッション1つとその Subagent）。
- **同時実行**：2つの place が同時に来ると同じ空きを二重に数えうる。lease（関門）は実害が出るまで入れない。
- **Orchestrator は Claude Code のまま**（Task list と `VERIFIED:` の hook が強制できるのはここだけ、ADR-0002 / 0004）。Codex・opencode・Gemini などは Worker またはレビュアーとして使う。

## 設定はファイル（.agent/policy.json → 各 app のディレクトリ）
> 追記（2026-09-28, owner 決定）：共有の `.agent/policy.json` は廃止。各 app が自分のディレクトリに設定と状態を持つ。orchd = `orchd/policy.json`（rules・modes・classes・runners・placement、パターン展開用に agents と computer 名の写し）・状態 `orchd/state/`。cad = `cad/config.json`（agents・computers・collect・gate・review・providers）・`cad/config/namespaces.json`（gitignore、例は `namespaces.example.json`）。場所は CWD に依存せず `<APP>_HOME` > 実行ファイルの 1 つ上（`<app>/bin/..`）> CWD で決める。mode ごとに `cadAddr` を持てる（urgent = local の cad、auto = VM の cad）。以下の `.agent/policy.json` は当時の記述。

仕様も精度も日々変わるので、調整するものはすべて `.agent/policy.json`（`CAD_POLICY` で差し替え可）に置き、コードには置かない。稼働中の `cad` は mtime で再読込する（壊れたファイルは log に出して直前の良い policy を使い続ける）。`cad show [classes|rules|runners|collect|agents|computers|policy]` で読む。
- `classes`：`{name: {criteria, estPct}}`。place が受け付ける class の一覧（コードの固定一覧を置き換え）。`criteria` は Orchestrator が分類に使う基準、`estPct` は 1 task が使う使用枠の見積もり（旧 `placement.estPct`）。class の追加はファイルの編集だけで済む。`classes` の無いファイルは `orchd` が拒否する（exit 1。ADR-0011 以降 `cad` は検証しない）
- `rules`：順序付きの決定リスト（上記）。`agent` は `path.Match` のパターンか `self`
- `runners`：`{service: {mode, cmd?, model?}}`。`mode` は `subagent`（Orchestrator の Task subagent）か `process`（`cmd` を起動。`{model}` は `model` に置換）。`process` は `cmd` 必須（`orchd` が読み込み時に検証）。place の 200 に選んだ Agent のサービスの runner を入れる。runner の無いサービスの Agent は place で `<agent>: no runner` として飛ばす。`subagent` の Agent は self のときだけ使える（それ以外は `<agent>: subagent runs only as self` で飛ばす。よって `claude/*` の rule も実質 self にしか一致しない）。cad 自身は起動しない（読み取り専用のデータ）
- `collect.usage.every`：usage collector の間隔（Go の duration）。スケジュールのたびに読むので再起動不要。`CAD_USAGE_EVERY` があればそちらが優先。不正な値は直前の良い値を使い、設定を消すと既定の 60s に戻る。前回の実行時刻 + 現在の間隔で判定するので、短くした間隔は次の tick から効く
- `placement.reservePct`・`agents`・`computers`：従来どおり

**self（owner 決定）**：Claude の task は常に Orchestrator セッションの subagent として動く。よって Claude の Agent は常に Orchestrator 自身のアカウントで、他の claude アカウントを選んでも使えない。Orchestrator は place に `"self": "<service>/<account>"` を渡し、rule の `agent: "self"` はそれだけに一致する（`policy.agents` に無い・渡されない場合は rule を飛ばす）。窓の判定は他の Agent と同じ。runner は `claude: {mode: subagent}`。

## 戦略（owner 決定・2026-09-27）
- **通常運用**：Claude を local で使い切るまで使い、使用枠が尽きたら opencode にフォールバックする（opencode のモデルは可変。現在は DeepSeek）。`rules` を `[{self, local}, {opencode/*, local}]` にする（順序が優先度）。
- **codex はレビュー専用**：実装 Agent としては使わない。`policy.agents` には残す（レビュー枠の usage 収集のため）が、どの rule にも一致させない＝`place` が codex を選ぶことはない。
- ~~**Mac がスリープしたら** GitHub Actions が opencode を起動して PR を作る。これは `cad` の外で扱い、rules には書かない（下記）。~~ 2026-09-28 撤去（下記）。

## Mac 睡眠時（GitHub Actions）
2026-09-28 撤去: Orchestrator は常駐し停止時は owner が再開するため、寝ている間の Actions 経路と Actions の実行先は不要（PR #22 は merge 後に無効化・撤去）

## MODE（owner 決定・2026-09-28）
- **MODE** で rule の優先順位を切り替える。`auto`（既定）= top-level `rules`：設計どおり local を最後の手段にする。今は `[{self, claude-cloud}, {self, local}, {opencode/*, local}]`。上記「戦略」の通常運用（local 優先）はこれで置き換える
- `urgent` = `modes.urgent.rules`：local の rule を先頭に置き、その後に auto の rule を並べる。owner が会話で「MODE=URGENT で処理して」と言ったら Orchestrator が `orchd mode set urgent --ns <ns>` を実行し、「解除」で `orchd mode clear --ns <ns>`
- context 圧縮・再起動で消えないようファイルに保存する（`$ORCHD_STATE_DIR/mode/<ns>.json` / `_global.json`）。起動時の環境変数 `ORCHD_MODE` でもセッション全体に効かせられる。優先順位は `--mode` > ns ファイル > global ファイル > `ORCHD_MODE` > `auto`。詳細は [orchd/README.md](../../orchd/README.md#mode)
- runner は computer にも依存する：`runners["<service>@<computer>"]` を先に、無ければ `runners["<service>"]`。`claude@claude-cloud` = `{mode: cloud, cmd: "claude --cloud"}`（Orchestrator が cloud セッションを起動する）

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
  - ADR-0008：`policy.providers.allowed` → Agent × Computer（`pol.computers`）+ `rules` に置き換える。`cad` の API に `POST /v1/place`（書き込み系の2つ目）と `ns` 必須を足す。capacity.go のメモリ式の slots は後で削除（この ADR では実装しない follow-up）
  - ADR-0009：Worker の認証（token 固定 → aienv の store）。spec に `class`・`acceptance`・`permissions`・`constraints.no_prod_write` を足す。provider の選択を `dev-dispatch` から `cad` の place へ移す（`dev-dispatch` は結果を適用するだけ）。spec の placement（strategy 等）は place では使わない
  - ADR-0002：task の入口を GitHub Issue にする（Issue #10）。内蔵 Task list は Orchestrator セッション内の実行管理

## 未決
- aienv のコマンド名（`codingenv aienv` か `caenv` か）と、aienv をこの repo に取り込むか。
- aienv の binding の外にある worktree（例 `../<repo>-pr-N`）が別アカウントを継承する問題：(a) 運用で避ける、(b) aienv が git worktree の親 repo を辿って binding を引く。
- opencode の usage collector。それまで opencode の Agent は `usage unknown`。usage の無い Agent は落とさず reason に `usage unknown` を残す。window の閾値は policy `placement.reservePct` / `classes.<name>.estPct`（仮値）。
- lease（関門）を入れる基準。

## 再検討する条件
- 同時 place の二重計上で実害（Worker の起動失敗、Mac の詰まり）が出たとき → lease を入れる
- class の付け替え・再 dispatch が頻発するとき → class の定義か `rules` を見直す
- 実行記録が溜まり、`est[class]` や rules を実測で決められるようになったとき
- Orchestrator の強制（Task list・hook）が Claude Code 以外でもできるようになったとき
