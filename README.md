# codingagentenv — 全プロジェクト共通の Claude Code ハーネス

/clear 後も文脈を失わず、作業を Task list 経由で Subagent に実行させるためのハーネス。
ハーネス本体は global（`~/.claude/`）に置き、各 project はデータだけを持つ（ADR-0003）。テンプレートを各 repo にコピーする方式はやめた。

## global への展開（ADR-0005）
編集は repo 内だけで行い、`~/.claude/` 配下は直接編集しない。展開はユーザーが次のコマンドで行う。`jq` が必要。

```sh
bin/codingenv install     # 展開。冪等。settings.json と CLAUDE.md のバックアップを取り、既存の設定は変更しない
bin/codingenv status      # 展開漏れ・drift の検出（読み取りのみ。漏れがあれば exit 1）
bin/codingenv uninstall   # install が入れたものだけを取り除く
```

初回だけ `bin/codingenv install` と打つ。以降は install が `~/.local/bin/codingenv` へ symlink を張るので、どこからでも `codingenv install|status|uninstall` で呼べる。`~/.local/bin` が PATH に無ければ install が警告する。同名の通常ファイルがあれば上書きせず失敗する。

install が行うこと：
- `bin/codingenv` を `~/.local/bin/codingenv`（`BIN_DIR` で変更可）へ symlink する
- `tools/*` を同じ `BIN_DIR` へ同名で symlink する（同名の通常ファイルがあればその tool だけ飛ばす）
- `skills/*` と `hooks/harness-*.sh` を `~/.claude/skills/`、`~/.claude/hooks/` へ symlink する
- `~/.claude/settings.json` に `env.CLAUDE_CODE_ENABLE_TODO_TOOLS=1` と hook 2つ（ADR-0004）を追記する
  - SessionStart：handoff が1件ならその全文、複数なら一覧を ADR 一覧とともに文脈に入れる。handoff が無ければ旧 PROGRESS.md を後方互換で読む。handoff も PROGRESS.md も無い project では何もしない
  - TaskCompleted：description に `VERIFIED: <コマンド> -> <結果>` の行が無い task の completed を拒否する。`CLAUDE_CODE_TASK_LIST_ID` がある project だけ
- `~/.claude/CLAUDE.md` のハーネス節を `global/CLAUDE.harness.md` の内容に置き換える（マーカー区間。他の節は変更しない）

hook の登録と CLAUDE.md の節は、repo を直したあと install を再実行するまで反映されない。symlink の中身（skill と hook script）は即座に全 project に効く。install 後は Claude Code を再起動する。

注意：この repo を移動・削除すると symlink が切れ、全 project で skill と hook が使えなくなる。

## 各 project が持つもの（すべて任意）
- `.claude/handoff/<name>.md`、`docs/decisions/`、`docs/context/`。旧 `PROGRESS.md` は後方互換で読む
- `.claude/settings.json` の `CLAUDE_CODE_TASK_LIST_ID`（無ければ `/pickup` が project ディレクトリ名で追加する）

これらが無い状態でも skill は動く。

## この repo のファイル

| ファイル | 役割 | 読まれ方 | 更新頻度 |
|---|---|---|---|
| CLAUDE.md | 業務の要点・コマンド・他ファイルへの案内 | 起動時・/clear後に自動 | 月 |
| docs/context/business.md | 業務フロー・ルール・AIが誤解しやすい点 | 必要な時（CLAUDE.mdから案内） | 四半期〜月 |
| docs/context/glossary.md | 業務用語とコード上の名前の対応 | 必要な時 | 随時 |
| docs/decisions/0000-template.md | ADR（設計判断と却下した案） | 必要な時 | 決定ごと |
| .claude/handoff/<name>.md | 進行中タスクの引き継ぎ（1セッション1ファイル） | pickupスキルと SessionStart hook | セッションごと |
| .claude/settings.json | Task list の共有 ID のみ | 起動時に自動 | 固定 |
| skills/dispatch/SKILL.md | Task list に分解して Subagent に実行させる手順 | `/dispatch` で呼ぶ | 固定 |
| skills/handoff/SKILL.md | /clear前に自分の handoff を書き出す手順 | `/handoff` で呼ぶ | 固定 |
| skills/pickup/SKILL.md | /clear後に読み直して理解を復唱する手順 | `/pickup` で呼ぶ | 固定 |
| hooks/harness-session-start.sh | handoff（複数なら一覧）を文脈に入れる | SessionStart hook | 固定 |
| hooks/harness-task-completed.sh | 未検証の completed を拒否する | TaskCompleted hook | 固定 |
| global/CLAUDE.harness.md | `~/.claude/CLAUDE.md` のハーネス節の正本 | `bin/codingenv install` で展開 | 随時 |
| bin/codingenv | global への展開・drift 検出・取り外し | 手で実行 | 固定 |
| tools/testdb | 共有テスト Postgres（`up`）と worktree ごとの DB URL（`url <worktree>`）。コンテナ名・image・port 等は `TESTDB_*` | Subagent / gate の steps | 固定 |
| tools/codex-localreview | Codex CLI でローカルレビューして PR にコメント（bot の quota 切れ時） | `codex-localreview <pr> <worktree>` | 固定 |
| tools/codex-probe | Codex の復帰を待ち、キュー（`<pr> <worktree>` 行）を順にローカルレビュー | バックグラウンドで実行 | 固定 |
| test/hooks.test.sh, test/harness.test.sh | hook・`bin/codingenv` のテスト | `bash test/<name>` | 固定 |

`/resume` は Claude Code の built-in コマンド（セッション履歴の再開）なので、スキル名には使わない。

## 運用ループ
1. `/dispatch <やりたいこと>` → main が Task list に分解し、Subagent に実行させ、検証してコミット
2. 区切りで `/handoff`
3. `/clear`
4. `/pickup` → エージェントの復唱を確認・修正 → 作業再開

`CLAUDE_CODE_TASK_LIST_ID` は `/pickup` がディレクトリ名で設定する。同じ ID の project があると Task list が混ざる。

## 配置ロジック（設計中・未実装。ADR-0010 で確定）
分類だけ Claude が行い、配置は `dev-dispatch` が cad の値から決定的に選ぶ（同じ入力なら同じ出力）。詳細は `.claude/design/placement-strategy.md`。

```
task spec
├─ 0. 分類（Orchestrator = Claude が spec に class を書く）
│     light-edit / gate-heavy / needs-db / long-running / urgent / retry
└─ 1. 配置（dev-dispatch。判断しない）
   ├─ admission（サブスク窓。cad の usage topic、サービス × アカウント単位）
   │   ├─ 5h 窓: 使用% + 稼働数 × est + est > 100 − reservePct → defer(resetsAt)
   │   ├─ 7d 窓: 均等ペース超過                                → defer(resetsAt)
   │   └─ 稼働 Worker ≥ maxWorkers                            → defer(5 分後)
   │      defer = 非 0 で返すだけ。キューは持たない（Task は pending のまま）
   ├─ Agent = サービス × アカウント（アカウントは aienv の store id。未決）。claude を使い切ったら opencode にフォールバック（モデルは可変・現在 DeepSeek）。codex はレビュー専用で配置されない
   ├─ provider 明示指定あり → feasible なら採用 / 不可なら exit 2
   └─ 候補 = spec.allow ∩ policy.providers.allowed
       ├─ feasible で絞る: memory・cpu・timeout・needs-db なら caps.db・maxCostUSD・safe なら非 preemptible
       ├─ local が feasible → local（最優先。slots は CAD_SLOTS=5 の固定上限、稼働数は数えず macOS に任せる）
       ├─ それ以外は strategy で並べる
       │   cheap（既定）: cost → preemptible → cold start
       │   fast        : cold start → cost
       │   safe        : cost → cold start（preemptible は除外済み）
       │   ranked      : spec の order 順
       ├─ 同点は provider 名の辞書順
       └─ 候補なし → exit 2（起動しない）
```

配置の記録（policy）は `cad` の CLI で編集する。ファイルは daemon と同じ（`CAD_POLICY` > `.agent/policy.json`）。書き込みは atomic で、稼働中の `cad` は mtime で再読込する（再起動不要）。usage は daemon が定期収集する（claude は `claude -p /usage`、codex は `codex app-server` の `account/rateLimits/read`）（`CAD_USAGE_EVERY`、既定 60s）。メタデータは `cad get` で読む（`CAD_ADDR`・`CAD_TOKEN`、`-ns` 必須）。

```
tools/cad get meta -ns dev                                   # 実行中の cad から全 topic（JSON）
tools/cad get usage -ns dev                                  # 1 topic（usage / capacity / policy / workers / quota）
tools/cad show [agents|computers|classAgents|policy]         # 引数なし = policy
tools/cad add agent claude/3f9a1c0e
tools/cad add computer gce-spot --file gce.json [--replace]  # または stdin / -
tools/cad add classagent needs-db 'claude/*'
tools/cad rm agent|computer|classagent <key> [pattern]
```

## トラブルシュート

- Task tools (`TaskCreate` など) が見えない → `~/.claude/settings.json` の `env` に `CLAUDE_CODE_ENABLE_TODO_TOOLS=1` があるか確認する。代替のキューを自作しない (ADR-0002)。
- 別プロジェクトの Task list と混ざる → 各 project の `.claude/settings.json` の `CLAUDE_CODE_TASK_LIST_ID` が重複していないか確認する。
- `/dispatch` などの skill が見つからない → `~/.claude/skills/` の symlink が切れていないか確認する（この repo を移動・削除すると切れる）。
- `~/.claude/tasks/` 配下は手で編集しない。状態変更は `TaskUpdate` で行う。

AGENTS.md を使う他のツール（Codex、Cursorなど）と併用する場合は、CLAUDE.md の内容を AGENTS.md に置き、
Claude Code 側は AGENTS.md を読む設定・仕様に合わせてください（Claude Code は CLAUDE.md が無い場合に AGENTS.md を読む、と調査時点のドキュメントに記載）。
