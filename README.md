# codingagentenv — 全プロジェクト共通の Claude Code ハーネス

/clear 後も文脈を失わず、作業を Task list 経由で Subagent に実行させるためのハーネス。
ハーネス本体は global（`~/.claude/`）に置き、各 project はデータだけを持つ（ADR-0003）。テンプレートを各 repo にコピーする方式はやめた。

## global に置くもの（新しいマシンでの install）
1. skill を symlink する（正本はこの repo の `skills/`）。`<repo>` はこの repo の絶対パス。
   ```sh
   ln -s <repo>/skills/dispatch ~/.claude/skills/dispatch
   ln -s <repo>/skills/handoff  ~/.claude/skills/handoff
   ln -s <repo>/skills/pickup   ~/.claude/skills/pickup
   ```
2. `~/.claude/settings.json` の `env` に `"CLAUDE_CODE_ENABLE_TODO_TOOLS": "1"` を追加する。
3. `~/.claude/CLAUDE.md` に「ハーネス（全プロジェクト共通）」セクションを追加する。内容は ADR-0003 の「決定」、または既存マシンの `~/.claude/CLAUDE.md` から写す。

注意：この repo を移動・削除すると symlink が切れ、全 project で skill が使えなくなる。`skills/` 配下の編集は全 project に即座に効く。

## 各 project が持つもの（すべて任意）
- `PROGRESS.md`、`docs/decisions/`、`docs/context/`
- `.claude/settings.json` の `CLAUDE_CODE_TASK_LIST_ID`（無ければ `/pickup` が project ディレクトリ名で追加する）

これらが無い状態でも skill は動く。

## この repo のファイル

| ファイル | 役割 | 読まれ方 | 更新頻度 |
|---|---|---|---|
| CLAUDE.md | 業務の要点・コマンド・他ファイルへの案内 | 起動時・/clear後に自動 | 月 |
| docs/context/business.md | 業務フロー・ルール・AIが誤解しやすい点 | 必要な時（CLAUDE.mdから案内） | 四半期〜月 |
| docs/context/glossary.md | 業務用語とコード上の名前の対応 | 必要な時 | 随時 |
| docs/decisions/0000-template.md | ADR（設計判断と却下した案） | 必要な時 | 決定ごと |
| PROGRESS.md | 進行中タスクの引き継ぎ | pickupスキル | セッションごと |
| .claude/settings.json | Task list の共有 ID のみ | 起動時に自動 | 固定 |
| skills/dispatch/SKILL.md | Task list に分解して Subagent に実行させる手順 | `/dispatch` で呼ぶ | 固定 |
| skills/handoff/SKILL.md | /clear前にPROGRESS.mdを書き出す手順 | `/handoff` で呼ぶ | 固定 |
| skills/pickup/SKILL.md | /clear後に読み直して理解を復唱する手順 | `/pickup` で呼ぶ | 固定 |

`/resume` は Claude Code の built-in コマンド（セッション履歴の再開）なので、スキル名には使わない。

## 運用ループ
1. `/dispatch <やりたいこと>` → main が Task list に分解し、Subagent に実行させ、検証してコミット
2. 区切りで `/handoff`
3. `/clear`
4. `/pickup` → エージェントの復唱を確認・修正 → 作業再開

`CLAUDE_CODE_TASK_LIST_ID` は `/pickup` がディレクトリ名で設定する。同じ ID の project があると Task list が混ざる。

## トラブルシュート

- Task tools (`TaskCreate` など) が見えない → `~/.claude/settings.json` の `env` に `CLAUDE_CODE_ENABLE_TODO_TOOLS=1` があるか確認する。代替のキューを自作しない (ADR-0002)。
- 別プロジェクトの Task list と混ざる → 各 project の `.claude/settings.json` の `CLAUDE_CODE_TASK_LIST_ID` が重複していないか確認する。
- `/dispatch` などの skill が見つからない → `~/.claude/skills/` の symlink が切れていないか確認する（この repo を移動・削除すると切れる）。
- `~/.claude/tasks/` 配下は手で編集しない。状態変更は `TaskUpdate` で行う。

AGENTS.md を使う他のツール（Codex、Cursorなど）と併用する場合は、CLAUDE.md の内容を AGENTS.md に置き、
Claude Code 側は AGENTS.md を読む設定・仕様に合わせてください（Claude Code は CLAUDE.md が無い場合に AGENTS.md を読む、と調査時点のドキュメントに記載）。
