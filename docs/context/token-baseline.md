# トークン消費ベースライン

計測日: 2026-09-20
対象: `~/.claude/projects/-Users-yasumasa-takemura-projects-codingagentenv/` の transcript 5セッション（すべて 2026-09-20、Step 3〜5 の施策が効く前に開始したもの）。
方法: ADR-0006 と同じ集計（`.message.usage` を `message.id` で畳んで model 別に合計）。数値のみ、会話本文は転記しない。
単位: トークン。`cache_read` = cache_read_input_tokens、`cache_creation` = cache_creation_input_tokens。

## セッション開始時のコンテキスト（常時ロード分の目安）

最初の assistant message の input + cache_read + cache_creation。

| session | 初回コンテキスト |
|---|---|
| 0305ffdb | 46,577 |
| 772568c9 | 47,380 |
| 8dc0eb26 | 47,162 |
| c0253305 | 48,000 |
| f5ffc464 | 46,249 |

平均 約 47,000。system prompt・ツール定義・skill 一覧・CLAUDE.md・hook 注入の合計で、Step 3（plugin / skill の非表示）の効果はここに出る。

## main セッション（すべて claude-fable-5-1）

| session | messages | input | output | cache_read | cache_creation |
|---|---|---|---|---|---|
| 0305ffdb | 30 | 60 | 24,079 | 2,206,589 | 74,134 |
| 772568c9 | 51 | 103 | 29,659 | 4,520,169 | 86,958 |
| 8dc0eb26 | 13 | 28 | 7,143 | 663,880 | 34,546 |
| c0253305 | 29 | 62 | 11,005 | 1,907,029 | 57,537 |
| f5ffc464 | 88 | 175 | 62,982 | 10,731,041 | 163,801 |

## Subagent

| session | files | model | messages | output | cache_read | cache_creation |
|---|---|---|---|---|---|---|
| 0305ffdb | 3 | claude-fable-5-1 | 21 | 14,885 | 818,380 | 113,799 |
| 772568c9 | 4 | claude-haiku-4-5 | 20 | 11,134 | 925,721 | 114,490 |
| 772568c9 | 4 | claude-sonnet-5 | 67 | 48,783 | 5,055,886 | 167,858 |
| c0253305 | 3 | claude-haiku-4-5 | 28 | 20,676 | 847,343 | 73,641 |
| f5ffc464 | 1 | claude-haiku-4-5 | 10 | 6,253 | 432,694 | 60,834 |

## 読み取れること

- 消費の大半は cache_read。1 message ごとに、その時点のコンテキスト全体が読み直される。message 数 × コンテキスト長で効くので、main のコンテキストを短く保つこと（報告の定型化、Explore への委譲）と往復回数を減らすことが最も効く。
- main は 1 message あたり平均 約 9万〜12万 の cache_read（f5ffc464: 10.7M / 88）。セッションが長いほど 1 message が高くなる。
- Subagent は 1 体あたり cache_creation が 約 3万〜6万（起動時の固定費）。5行の置換を haiku に出した例でも合計 約 3万。文言が決まった1ファイルの修正は main が直接やるほうが安い。
- 0305ffdb は Subagent が main と同じ最上位 model で走っていた。Step 4（3段の model 振り分け）の対象。

## 比較のしかた

- 常時ロード分: 再起動後の新セッションで初回コンテキストを同じ方法で測り、約 47,000 と比べる。
- task 単位: `.claude/token-usage.jsonl`（ADR-0006）の連続する2行の差分。
