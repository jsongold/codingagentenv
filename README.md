# Context Kit — /clear後も業務コンテキストを失わないためのテンプレート

リポジトリのルートにそのままコピーして、`<...>` の部分を埋めて使います。

| ファイル | 役割 | 読まれ方 | 更新頻度 |
|---|---|---|---|
| CLAUDE.md | 業務の要点・コマンド・他ファイルへの案内 | 起動時・/clear後に自動 | 月 |
| docs/context/business.md | 業務フロー・ルール・AIが誤解しやすい点 | 必要な時（CLAUDE.mdから案内） | 四半期〜月 |
| docs/context/glossary.md | 業務用語とコード上の名前の対応 | 必要な時 | 随時 |
| docs/decisions/0000-template.md | ADR（設計判断と却下した案） | 必要な時 | 決定ごと |
| PROGRESS.md | 進行中タスクの引き継ぎ | resumeスキル／hook | セッションごと |
| .claude/skills/handoff/SKILL.md | /clear前にPROGRESS.mdを書き出す手順 | `/handoff` で呼ぶ | 固定 |
| .claude/skills/resume/SKILL.md | /clear後に読み直して理解を復唱する手順 | `/resume` で呼ぶ | 固定 |

## 運用ループ
1. 作業 → 区切りで `/handoff`
2. `/clear`
3. `/resume` → エージェントの復唱を確認・修正 → 作業再開

AGENTS.md を使う他のツール（Codex、Cursorなど）と併用する場合は、CLAUDE.md の内容を AGENTS.md に置き、
Claude Code 側は AGENTS.md を読む設定・仕様に合わせてください（Claude Code は CLAUDE.md が無い場合に AGENTS.md を読む、と調査時点のドキュメントに記載）。
