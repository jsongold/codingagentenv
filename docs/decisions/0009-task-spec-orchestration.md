# ADR-0009: Orchestrator と Worker の契約は task 1件ごとの YAML の task spec。provider は Orchestrator が strategy 付きで決め、`dev-dispatch` が決定的に適用する

- 日付：2026-09-27
- 状態：採用

## 文脈
ADR-0008 で Orchestrator（ローカルの main セッション）と Worker（ローカル／任意のクラウド）を分けた。Worker は文脈ゼロで始まるので、今は Subagent のプロンプトに書いている「目的・完了条件・触ってよいファイル・報告形式」（`skills/dispatch`）を、機械が読める形で Worker に渡す必要がある。加えて、資源・置き場所（provider）・予算・秘密情報の扱いも決める必要がある。

制約：
- タスク状態は内蔵 Task list だけが持つ（ADR-0002）。spec がキューやタスク状態を持つと ADR-0002 と衝突する。
- completed は main が検証し `VERIFIED:` 行を書いてから（ADR-0004）。
- 動的な値（capacity・workers）はプロンプトで取らず、`cad` が決定的に返す（ADR-0008）。
- 触るファイルは ChangeGraph で決まる（`skills/changegraph`）。

## 決定
- **task spec は Orchestrator と Worker の契約**。Task list の item 1件につき spec 1件（1:1、`taskId` を持つ）。キューではない。状態（pending / in_progress / completed）は spec に書かない。
- **形式は YAML**。Orchestrator が dispatch 時に `.claude/task-specs/<id>.yaml` へ生成する。コミットしない（ChangeGraph と同じ扱い）。例は `agent/task-spec.example.yaml`。
- **フィールド**（`version: 1`）

  | フィールド | 意味 |
  |---|---|
  | `version` | spec の版。互換が壊れたら上げる |
  | `id` | `<changegraph 名>-<node id>`。Worker の labels にも入れ、`cad` の workers と突き合わせる |
  | `taskId` | 内蔵 Task list の id |
  | `repo` / `base` / `branch` | clone 元、分岐元（例 `origin/main`）、作るブランチ |
  | `goal` | 目的（自然文） |
  | `context[]` | 先に読むファイル・ADR のパス |
  | `files[]` | 触ってよいファイル。ChangeGraph の node の `files` をそのまま写す |
  | `done[]` | 完了条件のコマンド。全部 exit 0 で完了 |
  | `resources{memoryMB,cpus,diskGB,timeoutMin}` | Worker に要る資源と壁時計の上限 |
  | `placement{allow[],maxCostUSD,strategy,order[]?,provider?}` | 置き場所。`allow` 省略時は `policy.providers.allowed`。`strategy` 省略時は policy の既定。`provider` は明示指定で strategy より優先 |
  | `agent{model}` | 実装者のモデル。ai-review が実装者の除外（`policy.review.excludeImplementer`）に使う |
  | `review{}` | 空なら `policy.review` に従う。task ごとの上書きだけを書く |
  | `output{pr,reportLines}` | PR を出すか、報告の最大行数 |
  | `secrets[]` | 要る秘密情報の**名前だけ**。値は書かない |

- **provider の選択**：Orchestrator が task ごとに `strategy`（`local-first` / `cheapest` / `fastest` / `ranked`＋`order[]`）か明示の `provider` を決めて spec に書く。`dev-dispatch` はそれを `cad` のデータ（`/v1/capacity`・`/v1/workers`・`/v1/policy`）に対して**決定的に**適用するだけ。判断もプロンプト時のデータ取得もしない。同じ spec と同じ `cad` の値なら同じ provider になる。条件を満たす provider が無ければ起動せず非 0 で終わる。
- **`done` は2回走る**：Worker が PR を出す前に1回、Orchestrator が `VERIFIED:` を書く前に1回（ADR-0004）。Worker の実行結果は信用しない。
- **Worker は Claude Code を headless で spec を入力に動かし**、PR を1本出す。報告は `output.reportLines` 行以内。

### フロー
1. Orchestrator：ChangeGraph の node から `TaskCreate` → spec を生成 → `TaskUpdate` で in_progress。
2. `dev-dispatch <spec>`：`cad` から値を読み、strategy で provider を決め、`agent/providers/<name>/start` に spec を渡す。
3. provider `start`：Worker（VM / job / sandbox / ローカル worktree）を起動し、secret store の値を環境変数で注入する。
4. Worker bootstrap（`agent/bootstrap.sh`、ChangeGraph c7）：`repo` を clone、`base` から `branch` を作る、`cad` を起動する。
5. 実装：`claude -p`（headless。`claude --help` で `-p/--print`・`--model`・`--output-format`・`--max-budget-usd`・`--permission-mode` の存在は確認済み。組み合わせと権限設定は未検証）に spec から組み立てたプロンプトを渡す。`files[]` 外の変更は PR 前に `git diff --name-only` で検出して失敗扱いにする。
6. `done[]` を実行。全部通ったら push して PR を作る。通らなければ PR を出さず報告だけ返す。
7. Orchestrator：PR のブランチを worktree に取り、`done[]` を実行 → 通れば `VERIFIED:` 追記。
8. ai-review（実装者 `agent.model` を除外、quota で自動フォールバック）→ 承認 + CI green（CI があれば）→ merge → 別の `TaskUpdate` で completed。

### Worker のライフサイクル
- 起動：`start` が返した worker id を Orchestrator が Task の metadata（または説明欄）に書く。状態は `cad` が `list` をポーリングして集める（pull、ADR-0008）。Worker から Mac へは push しない。
- タイムアウト：`resources.timeoutMin` を超えたら `dev-dispatch`（または provider 側の実行時間上限）が `stop` する。
- リトライ：自動リトライはしない。失敗・タイムアウト・消失は Task の説明欄に理由を書いて pending に戻し、Orchestrator が再 dispatch を決める（Task list に failed は無い、ADR-0002）。
- Spot の preemption：`list` で `stopped` か見えなくなった（`unknown`）Worker は失敗扱い。push 済みのブランチがあればそこから再開できる。
- 後片付け：PR を出した後、失敗・タイムアウトのいずれでも `stop` を呼ぶ。`list` に残り続ける Worker は `stop` の漏れとして Orchestrator に見せる。

### provider スクリプトの interface（`agent/providers/<name>/`）
- `start`：stdin に spec の JSON。stdout に1行の JSON `{"id": "...", "provider": "<name>"}`。exit 0 以外は起動失敗。
- `list`：引数なし。stdout に ADR-0008 の Worker の JSON 配列 `[{id, provider, state, startedAt, lastSeenAt, labels}]`。`labels.specId` に spec の `id` を入れる。`cad` が定期的に呼ぶので速く、副作用なしにする。
- `stop <id>`：冪等。存在しない id でも exit 0。
- 3つとも実行ファイル（言語は問わない）。認証は provider の CLI の設定に任せ、スクリプトは秘密情報を引数や stdout に出さない。

### 秘密情報
- spec には名前だけ。値は provider ごとの secret store（例：GCP Secret Manager、E2B の env、ローカルは Keychain / 環境変数）から `start` が注入する。
- spec・報告・ログ・PR に値を出さない。Worker の `GH_TOKEN` は対象 repo に限った最小権限にする。

### CI と `done` の役割
- `done` は Worker と Orchestrator が「この task が終わったか」を判定するためのもの。CI（ephemeral runner）は merge 条件で、main との合流後の回帰を見る（ADR-0008 のレビュー方針）。
- `done` と CI のコマンドは重なってよい。CI が無い repo では `done` と ai-review だけで merge できる。

### 費用の上限
- `resources.timeoutMin`：壁時計の上限。超えたら `stop`。
- `placement.maxCostUSD`：provider の概算単価 × `timeoutMin` がこれを超える provider は選ばない（`dev-dispatch` が選択時に判定）。
- Claude の API 費用は `claude --max-budget-usd` で別に絞れる（フラグの存在は確認済み、挙動は未検証）。

### 未決事項
- YAML のパース：Go / Node の標準ライブラリには YAML が無い。案は (a) `dev-dispatch` が dispatch 時に JSON に変換し、`start`・Worker・`cad` は JSON だけ読む、(b) 小さな依存を1つ入れる、(c) 最初から JSON で書く。この ADR では決めない。
- provider ごとの概算単価をどこに持つか（policy に足すか、provider スクリプトが返すか）。
- `maxCostUSD` に Claude の API 費用を含めるか、`--max-budget-usd` で別枠にするか。
- `fastest` の定義（起動待ち時間か、資源の大きさか）。
- `.claude/task-specs/` を `.gitignore` に入れるか（この ADR の変更範囲外）。
- Worker が `files[]` 外に触る必要が出たときの戻し方（今は失敗扱いにして Orchestrator が ChangeGraph を直す）。
- headless 実行の権限設定（`--permission-mode` / `--allowedTools` のどれで `files[]` と `done[]` だけに絞るか）。

## 検討して却下した案
| 案 | 却下理由 |
|---|---|
| spec に状態を持たせ、Worker が spec を取りに来る（pull 型キュー） | キューの自作になり ADR-0002 と衝突する |
| spec をコミットする | task ごとの使い捨てで、並列 worktree の衝突の元になる。正本は Task list と ChangeGraph |
| provider の選択を `dev-dispatch` の中で判断させる（LLM / ヒューリスティック） | 結果が揺れて検証できない（ADR-0008） |
| provider を policy で1つに固定する | task ごとに向き不向きがある（ADR-0008） |
| Worker の `done` の結果を信じて VERIFIED にする | 自己申告の completed は検証を素通りする（ADR-0002, 0004） |
| 秘密情報の値を spec に入れる | spec はファイルとして Worker に渡り、ログや報告に漏れる |

## 影響
- 良い影響：Subagent へのプロンプトの必須項目（目的・完了条件・触ってよいファイル・報告形式）が機械で検証できる形になる。provider の選択が再現できる。タスク状態は Task list だけに残り二重管理が起きない
- 受け入れたトレードオフ：spec はコミットしないので、マシンを移ると残らない（Task list と同じくマシンローカル）。`done` を2回走らせる分の時間と費用。provider ごとに start / list / stop の3本を書く手間
- 再検討する条件：未決事項の YAML パースを決めたとき。spec の項目が Task list の description と重複して手間になったとき。自動リトライが必要なほど preemption が多いとき
