# SpecOpt：Web 調査と仮説（CAO-69）

目的：CA に最適な Spec（CA に渡す仕様。Task の書き方と、添えるコンテキストの形）を探すために、候補と公開されている実測を棚卸しし、検証する仮説を決める。

- 調査日：2026-10-12。収集は opencode、裏取りは一次ソースの原文（`curl` で取得）で行った
- ここに書いた数値は、すべて原文で確かめたもの。条件（agent・モデル・件数）を必ず添える
- 親：Epic CAO-2、Story CAO-4

## 1. 確かめた事実

### トークンはどこで使われるか

| 事実 | 条件 | 出どころ |
|---|---|---|
| 読み取り系の操作（cat / grep など）がトークンの 76.1% を占める（実行 12.1%、編集 11.8%） | Mini-SWE-Agent + Claude Sonnet 4.5、SWE-bench Verified | [arXiv 2601.16746](https://arxiv.org/abs/2601.16746) |
| 総入力 54.90B のうち、履歴の読み直し（prefix）が 52.56B、そのターンで足された分が 2.34B、出力は 186.9M。キャッシュのヒット率は 95.7% | Claude Code と Codex の実利用、43 人・4,265 セッション | [arXiv 2606.30560](https://arxiv.org/abs/2606.30560) |
| 同じタスクでも、実行ごとに総トークンが最大 30 倍ばらつく。正解率は中程度のコストで頭打ちになる | OpenHands、SWE-bench Verified 500 件 × 4 回 × 8 モデル | [arXiv 2604.22750](https://arxiv.org/abs/2604.22750) |
| Claude Code は、何もしなくても毎回 15,983〜20,330 トークン（システムプロンプト + 24 個のツール定義）を送る | 最小の依頼での固定分。PI.DEV は 1,147〜1,642 | [arXiv 2608.01347](https://arxiv.org/abs/2608.01347) |

### 依頼の書き方で何が変わるか

| 事実 | 条件 | 出どころ |
|---|---|---|
| 「複数の方法を検討せよ」という指示で、出力トークンが 2.52 倍（Claude Code）、2.70 倍（PI.DEV）になる | claude-sonnet-5、6 タスク、事後登録の追加実験 | [arXiv 2608.01347](https://arxiv.org/abs/2608.01347) |
| 冗長な繰り返しは約 1 倍、範囲・最小変更・停止条件の指定は 0.93〜1.02 倍で、どちらもほぼ影響しない | 同上 | 同上 |
| 具体的な依頼はファイルの読み取りを減らす、複雑なタスクでの plan mode はやり直しを防ぐ（実測なし、ベンダーの記述） | Claude Code の docs | [costs](https://code.claude.com/docs/en/costs) |

### 添えるコンテキストで何が変わるか

| 事実 | 条件 | 出どころ |
|---|---|---|
| 編集箇所以外を削った context は、ファイル全文より少ないトークンで同等以上に解ける（解決 1 件あたり 19.3K 対 93.7K。差は有意でない） | Claude Sonnet 4.6 の単発実行、70 件。編集箇所を正解から与えた上限値で、実運用の方式ではない | [arXiv 2607.09691](https://arxiv.org/abs/2607.09691) |
| 残りを outline やシグネチャで渡しても、削除するより良くならない（26.3K） | 同上 | 同上 |
| context file（AGENTS.md 類）を足しても成功率は有意に変わらず、推論コストは平均 20% 超増える | Claude Code を含む 4 agent、SWE-bench Lite 300 件ほか | [arXiv 2602.11988](https://arxiv.org/abs/2602.11988)（v3） |
| AGENTS.md があると、出力トークンの中央値が 16.58%、実行時間が 28.64% 下がる | Codex（gpt-5.2-codex）のみ、10 repo・124 PR | [arXiv 2601.20404](https://arxiv.org/abs/2601.20404) |
| context の渡し方を変えても、正答率は動かない | Claude Code と Codex、17 タスク・288 実行 | [arXiv 2607.27250](https://arxiv.org/abs/2607.27250) |
| context を圧縮すると入力トークンは 42% 減るが、解決率は 50.0% から 38.0% に下がる | GPT-5-mini、SWE-bench Verified の 100 件 | [Hrubec 2025](https://repositum.tuwien.at/bitstream/20.500.12708/224666/1/Hrubec%20Nicolas%20-%202025%20-%20Reducing%20Token%20Usage%20of%20Software%20Engineering%20Agents.pdf) |
| CLAUDE.md は 200 行以内が目安。`paths` つきの rules は、該当ファイルを扱うときだけ読み込まれる | Claude Code の docs | [memory](https://code.claude.com/docs/en/memory) |

### トークンを減らすことと、コストを減らすことは別

| 事実 | 条件 | 出どころ |
|---|---|---|
| ツール出力のトークンを 38.4% 減らした構成で、請求額は 6.8% 増えた。キャッシュの作成と読みが、請求額の約 80% を占める | Claude Code、103 タスク・7 repo・2,908 実行 | [arXiv 2607.12161](https://arxiv.org/abs/2607.12161) |

## 2. 見つからなかったもの

- Spec の形式（Spec Kit、OpenSpec、superpowers、PRP、EARS など）を、トークンと成功率で比べた公開の実測
- 「詳細な依頼と短い依頼」「ファイルの場所を渡すか」「受入テストを先に渡すか」を比べた公開の実測
- サブスクの利用枠が、トークンの内訳（キャッシュ読みなど）をどう数えるか

## 3. 候補

候補の説明は、opencode が一次ソース（README と docs）を読んでまとめたもの。原文で裏取りしたのは、OpenSpec の差分形式、Task Master、aider の repo map、Claude Code の docs だけで、ほかの説明は裏取りしていない。

### Spec の形式

| 候補 | CA に渡すもの | 一次ソース |
|---|---|---|
| 現行の Task 仕様（Baseline） | 1 ファイル 5 節：触るファイル、決定済みの変更、制約、担当、done コマンド | `skills/cco/task-spec.md` |
| Spec Kit | 機能ごとに spec / plan / tasks など 6〜7 ファイル | [github/spec-kit](https://github.com/github/spec-kit) |
| OpenSpec | 変更ごとに proposal / design / tasks と、spec の差分（ADDED / MODIFIED / REMOVED） | [Fission-AI/OpenSpec](https://github.com/Fission-AI/OpenSpec) |
| superpowers | 設計 → 2〜5 分単位の Task（ファイルパスと検証手順つき）→ Task ごとに新しい context で実行 | [obra/superpowers](https://github.com/obra/superpowers) |
| PRP | 1 ファイルに、必要な context（URL、ファイル、注意点）と検証コマンドを全部書く | [Wirasm/PRPs-agentic-eng](https://github.com/Wirasm/PRPs-agentic-eng) |
| Kiro specs | requirements（EARS 記法）/ design / tasks の 3 ファイル | [kiro.dev](https://kiro.dev/docs/specs/) |
| BMAD-METHOD | 規模に応じて brief → spec → architecture → story | [bmad-code-org/BMAD-METHOD](https://github.com/bmad-code-org/BMAD-METHOD) |
| Task Master | PRD → tasks.json（依存、詳細、テスト方針） | [eyaltoledano/claude-task-master](https://github.com/eyaltoledano/claude-task-master) |
| EARS / Gherkin | 受入基準の定型文 | [EARS](https://alistairmavin.com/EARS)、[Gherkin](https://cucumber.io/docs/gherkin/reference/) |
| plan mode | CA 自身が実装前に計画を書く | [permission-modes](https://code.claude.com/docs/en/permission-modes) |

### Spec に添えるコンテキスト

| 候補 | 中身 | 一次ソース |
|---|---|---|
| CLAUDE.md / AGENTS.md | 毎回読み込まれる規約とコマンド | [memory](https://code.claude.com/docs/en/memory)、[agents.md](https://agents.md/) |
| `paths` つきの rules | 該当ファイルを扱うときだけ読み込まれる規約 | [memory](https://code.claude.com/docs/en/memory) |
| 編集箇所だけの context | 触る箇所の抜粋 | [arXiv 2607.09691](https://arxiv.org/abs/2607.09691) |
| repo map | シンボルと依存の要約（既定 1k トークン） | [aider](https://aider.chat/docs/repomap.html) |
| シンボル検索（LSP / MCP） | 必要な定義だけを取り出すツール | [oraios/serena](https://github.com/oraios/serena) |
| Capability カタログ、golden template | API の署名と使用例、従うべき既存パターンの名指し | PRP のテンプレート |
| 受入テストの事前付与 | 合否が出るコマンド（Baseline が採用済み） | `skills/cco/task-spec.md` |

形式ごとの比較は、形式そのものではなく、形式が持つ要素（場所を渡すか、決め切るか、受入を渡すか、どれだけ書くか）に分けて検証する。

## 4. 仮説（検証前）

| # | 仮説 | 根拠 | 最小の検証 |
|---|---|---|---|
| H1 | 触るファイルと箇所を Spec に書くと、読み取りが減り、総トークンが下がる | 読み取りが 76.1%。編集箇所だけで足りる（上限値） | Target 節を「なし / ファイルパス / ファイルパス + 関数名」で比べる |
| H2 | 実装の方針を Spec で決め切ると、探索と出力が減る | 複数の方法の検討で出力 2.5 倍 | Change 節を「決定済み / 目的だけ」で比べる |
| H3 | 受入コマンドを先に渡すと、修正の往復が減る | ベンダーの記述のみ。実測なし | Observable acceptance を「あり / なし」で比べる |
| H4 | 多ファイル・全文投入型の Spec は、Baseline よりトークンを増やし、成功率は上げない | context file で成功率は不変、コスト 20% 増 | 同じタスクを「PRP 形式 / Baseline」で比べる |
| H5 | 毎ターン読み直される固定分（CLAUDE.md、ツール定義）を減らす方が、Spec 本文の工夫より効く | 入力の 95.7% が読み直し。固定分は 16K〜20K | CLAUDE.md を「現状 / 最小」で比べる |

### 検証の前提

- 同じタスクでも最大 30 倍ばらつくので、1 条件につき複数回実行して中央値で比べる
- 記録するのは、input / output / cache read / cache creation の 4 内訳、ターン数、合否。物差しを 1 つに決めるのは、内訳を見てからにする
