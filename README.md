# codingagentenv — 全プロジェクト共通の Claude Code ハーネス

**Mission**: コーディングエージェントを 24/7 動かし続け、コストを最小に抑えながら計算資源と AI の使用枠を使い切る（[docs/mission.md](docs/mission.md)）。

/clear 後も文脈を失わず、作業を GitHub Issue 経由で Subagent に実行させるためのハーネス。
ハーネス本体は global（`~/.claude/`）に置き、各 project はデータだけを持つ（#92）。テンプレートを各 repo にコピーする方式はやめた。

## global への展開（#93）
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
- `global/harness.json` を `~/.claude/harness.json` へ symlink する（同名の通常ファイルがあれば飛ばす）
- `~/.claude/settings.json` に SessionStart hook を追記する。旧版が入れた TaskCompleted hook と `env.CLAUDE_CODE_ENABLE_TODO_TOOLS` は取り除く（#103）
  - SessionStart：handoff が1件ならその全文、複数なら一覧を、harness.json の spec store の一覧（label 付きの open Issue か dir のファイル）とともに文脈に入れる（未設定なら何も出さない。gh が無い・通信できないときは Issue 一覧を省く）。handoff が無ければ旧 PROGRESS.md を後方互換で読む。handoff も PROGRESS.md も無い project では何もしない
- `~/.claude/CLAUDE.md` のハーネス節を `global/CLAUDE.harness.md` の内容に置き換える（マーカー区間。他の節は変更しない）

hook の登録と CLAUDE.md の節は、repo を直したあと install を再実行するまで反映されない。symlink の中身（skill と hook script）は即座に全 project に効く。install 後は Claude Code を再起動する。

注意：この repo を移動・削除すると symlink が切れ、全 project で skill と hook が使えなくなる。

## 各 project が持つもの（すべて任意）
- `.claude/handoff/<name>.md`、`docs/context/`。設計判断（spec）の置き場は `.claude/harness.json` で決める（下記）。旧 `PROGRESS.md` は後方互換で読む

これらが無い状態でも skill は動く。

## harness.json（spec の置き場）
spec（設計判断）の置き場を環境ごとに選ぶ。優先順位は project の `.claude/harness.json` > global の `~/.claude/harness.json`（正本は `global/harness.json`）> 未設定。未設定なら hook も skill も spec を探さない。`/setupca` で対話で作れる。

```json
{"spec":{"store":"issues","label":"doc:spec"}}
{"spec":{"store":"files","dir":"docs/decisions"}}
```

`store` を省略するか `null` にすると spec なし。

## この repo のファイル

| ファイル | 役割 | 読まれ方 | 更新頻度 |
|---|---|---|---|
| CLAUDE.md | 業務の要点・コマンド・他ファイルへの案内 | 起動時・/clear後に自動 | 月 |
| docs/context/business.md | 業務フロー・ルール・AIが誤解しやすい点 | 必要な時（CLAUDE.mdから案内） | 四半期〜月 |
| docs/context/glossary.md | 業務用語とコード上の名前の対応 | 必要な時 | 随時 |
| .github/ISSUE_TEMPLATE/spec.md | 設計判断（`doc:spec` Issue）のテンプレート | Issue を作る時 | 決定ごと |
| .claude/handoff/<name>.md | 進行中タスクの引き継ぎ（1セッション1ファイル） | pickupスキルと SessionStart hook | セッションごと |
| .claude/settings.json | この project の plugin・skill の設定 | 起動時に自動 | 固定 |
| skills/taskman/SKILL.md | 依頼を分類して Task を作る手順 | `/taskman` で呼ぶ | 固定 |
| skills/design/SKILL.md | 境界と Node Graph で設計する手順 | `/taskman` から呼ぶ | 固定 |
| skills/dispatch/SKILL.md | 次の Agent（Subagent）を呼ぶ手順 | `/dispatch` で呼ぶ | 固定 |
| skills/handoff/SKILL.md | /clear前に自分の handoff を書き出す手順 | `/handoff` で呼ぶ | 固定 |
| skills/pickup/SKILL.md | /clear後に読み直して理解を復唱する手順 | `/pickup` で呼ぶ | 固定 |
| skills/setupca/SKILL.md | harness.json を対話で作る手順 | `/setupca` で呼ぶ | 固定 |
| hooks/harness-session-start.sh | handoff（複数なら一覧）を文脈に入れる | SessionStart hook | 固定 |
| global/CLAUDE.harness.md | `~/.claude/CLAUDE.md` のハーネス節の正本 | `bin/codingenv install` で展開 | 随時 |
| global/harness.json | `~/.claude/harness.json`（spec の置き場）の正本 | `bin/codingenv install` で展開 | 随時 |
| bin/codingenv | global への展開・drift 検出・取り外し | 手で実行 | 固定 |
| tools/codex-localreview | Codex CLI でローカルレビューして PR にコメント（bot の quota 切れ時） | `codex-localreview <pr> <worktree>` | 固定 |
| tools/codex-probe | Codex の復帰を待ち、キュー（`<pr> <worktree>` 行）を順にローカルレビュー | バックグラウンドで実行 | 固定 |
| test/hooks.test.sh, test/harness.test.sh | hook・`bin/codingenv` のテスト | `bash test/<name>` | 固定 |

`/resume` は Claude Code の built-in コマンド（セッション履歴の再開）なので、スキル名には使わない。

## 運用ループ
1. `/taskman <やりたいこと>` → Task を作る → main loop が Issue・worktree を用意 → `/dispatch` が Subagent を呼ぶ → Subagent が実装・検証・コミット
2. 区切りで `/handoff`
3. `/clear`
4. `/pickup` → エージェントの復唱を確認・修正 → 作業再開

## 配置ロジック（#97）
調整するものはファイルに置く。配置（`classes`・`rules`・`modes`・`runners`）は `orchd/policy.json`、収集対象（`agents`・`computers`・`collect`）は `cad/config.json`。どちらも実行ファイルの場所から見つけるので CWD に依存しない。分類だけ Orchestrator（Claude）が行い、配置は `orchd place`（[orchd/README.md](orchd/README.md)。cad から usage・capacity を HTTP で読む）が policy の `rules`（順序付きの決定リスト、先勝ち）を上から評価して決定的に返す（同じ入力なら同じ出力）。

```
task spec
├─ 0. 分類（Orchestrator = Claude が spec に class を書く）
│     policy の classes から選ぶ（seed: light-edit / gate-heavy / needs-db / long-running / urgent / retry）
└─ 1. 配置
   ├─ rule 0: self（Orchestrator 自身の claude、subagent で実行）の窓が空いている → self × local
   ├─ rule 1: opencode の窓が空いている   → opencode × local
   └─ どれも窓で塞がっている → exit 3 defer（最も早い reset まで。キューは持たない）
      窓以外（local の slot 無しなど）で塞がっている → exit 4
```

配置の記録（policy）は `cad` の CLI で編集する。ファイルは daemon と同じ（`CAD_CONFIG` > `cad/config.json`。詳細は [cad/README.md](cad/README.md#設定)）。書き込みは atomic で、稼働中の `cad` は mtime で再読込する（再起動不要）。usage は daemon が定期収集する（claude は `claude -p /usage`、codex は `codex app-server` の `account/rateLimits/read`）（`CAD_USAGE_EVERY` > policy `collect.usage.every`、既定 60s）。メタデータは `cad get` で読む（`CAD_ADDR`・`CAD_TOKEN`、`-ns` 必須）。

```
tools/cad get meta -ns dev                                   # 実行中の cad から全 topic（JSON）
tools/cad get usage -ns dev                                  # 1 topic（usage / capacity / policy / workers / quota）
tools/cad show [collect|agents|computers|policy]              # 引数なし = policy 全体
tools/orchd show [rules|classes|runners]                     # 配置の設定（orchd が読む）
tools/orchd place --class gate-heavy --self claude/a12e00a7  # 配置を JSON で返す
tools/orchd dispatch --placement "$json"                      # placement の runner を実行する
tools/cad add agent claude/3f9a1c0e
tools/cad add computer gce-spot --file gce.json [--replace]  # または stdin / -
tools/cad rm agent|computer <key>
```

## トラブルシュート

- `/dispatch` などの skill が見つからない → `~/.claude/skills/` の symlink が切れていないか確認する（この repo を移動・削除すると切れる）。

AGENTS.md を使う他のツール（Codex、Cursorなど）と併用する場合は、CLAUDE.md の内容を AGENTS.md に置き、
Claude Code 側は AGENTS.md を読む設定・仕様に合わせてください（Claude Code は CLAUDE.md が無い場合に AGENTS.md を読む、と調査時点のドキュメントに記載）。
