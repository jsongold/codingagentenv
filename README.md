# Context Kit — /clear後も業務コンテキストを失わないためのテンプレート

リポジトリのルートにそのままコピーして、`<...>` の部分を埋めて使います。

| ファイル | 役割 | 読まれ方 | 更新頻度 |
|---|---|---|---|
| CLAUDE.md | 業務の要点・コマンド・他ファイルへの案内 | 起動時・/clear後に自動 | 月 |
| docs/context/business.md | 業務フロー・ルール・AIが誤解しやすい点 | 必要な時（CLAUDE.mdから案内） | 四半期〜月 |
| docs/context/glossary.md | 業務用語とコード上の名前の対応 | 必要な時 | 随時 |
| docs/decisions/0000-template.md | ADR（設計判断と却下した案） | 必要な時 | 決定ごと |
| PROGRESS.md | 進行中タスクの引き継ぎ | pickupスキル | セッションごと |
| .claude/settings.json | Task tools の有効化と Task list の共有 ID | 起動時に自動 | 固定 |
| .claude/skills/dispatch/SKILL.md | Task list に分解して Subagent に実行させる手順 | `/dispatch` で呼ぶ | 固定 |
| .claude/skills/handoff/SKILL.md | /clear前にPROGRESS.mdを書き出す手順 | `/handoff` で呼ぶ | 固定 |
| .claude/skills/pickup/SKILL.md | /clear後に読み直して理解を復唱する手順 | `/pickup` で呼ぶ | 固定 |

`/resume` は Claude Code の built-in コマンド（セッション履歴の再開）なので、スキル名には使わない。

## 運用ループ
1. `/dispatch <やりたいこと>` → main が Task list に分解し、Subagent に実行させ、検証してコミット
2. 区切りで `/handoff`
3. `/clear`
4. `/pickup` → エージェントの復唱を確認・修正 → 作業再開

別のプロジェクトにコピーしたら、`.claude/settings.json` の `CLAUDE_CODE_TASK_LIST_ID` をそのプロジェクト名に変えること。同じ ID だと Task list が混ざる。

## トラブルシュート

- Task tools (`TaskCreate` など) が見えない → `.claude/settings.json` の `env` に `CLAUDE_CODE_ENABLE_TODO_TOOLS=1` があるか確認する。代替のキューを自作しない (ADR-0002)。
- 別プロジェクトの Task list と混ざる → `CLAUDE_CODE_TASK_LIST_ID` をプロジェクトごとに変える。
- `~/.claude/tasks/` 配下は手で編集しない。状態変更は `TaskUpdate` で行う。

AGENTS.md を使う他のツール（Codex、Cursorなど）と併用する場合は、CLAUDE.md の内容を AGENTS.md に置き、
Claude Code 側は AGENTS.md を読む設定・仕様に合わせてください（Claude Code は CLAUDE.md が無い場合に AGENTS.md を読む、と調査時点のドキュメントに記載）。
