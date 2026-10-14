# subagent に渡す条件の候補（CAO-129）

目的：subagent（worker / 配送役）に Task を渡すときの条件のうち、完了率と token 量に効きそうなものを候補として一覧にする。後続の CAO-130 が、この一覧の候補ごとに実験する。

棚卸しの観点：Task の粒度、渡す文脈の量、仕様の節の構成、done コマンドの形、報告の長さ、モデル、worker に決めさせる範囲、質問の許可。現行の条件は `skills/cco/SKILL.md` と `skills/cco/task-spec.md` に従い、変える案は「現行は〜」と明記する。

### C1：Task の粒度（触るファイル数・変更行数）
なぜ効きそうか：触るファイルが増えると worker の探索と読み込みが増え、done を通す前に詰まる点も増える。現行は 1 Task = 1 コミット・PR の差分は 400 行以内が目安だが、Task あたりの Target 数の上限は決めていない。
水準：Target 1 個 / 3 個 / 6 個
測り方：token は worker の session jsonl を `bash tools/tokens <worker の session id>` の total 列で取る。Target 数は配送に渡した仕様ファイルの `## Target` 節にある `` - `…` `` の行数で数える。変更行数は worker のコミットの `git show --stat` の insertions + deletions。完了率は main が done を再実行した exit code（初回で 0 だったか）。
### C2：渡す文脈の量（仕様本文の行数）
なぜ効きそうか：仕様は worker の最初の user 入力になり、その長さがそのまま入力 token に乗る。現行は「渡すもの」の 7 項目だけを決めていて、本文の行数の目安は無い。
水準：仕様 20 行 / 60 行 / 120 行
測り方：仕様ファイルの行数は `wc -l`。worker の session jsonl で最初の `type` が `user` の行の `message.content`（文字列）の長さを入力量とする（この session では 6941 文字）。完了率は C1 と同じ done の初回 exit code。
### C3：仕様の Change の書き方（散文か、番号付きの手順か）
なぜ効きそうか：節の数は 5 つに固定したまま、Change の中身の書き方だけを変える。手順を番号で書くと worker が順に進められ、読み直しと試行錯誤が減るかもしれない。
水準：散文 / 番号付き手順 3 件 / 番号付き手順 6 件
測り方：worker の session jsonl で `type` が `assistant` の行の `message.content` にある `tool_use` ブロックの件数（tool 呼び出し回数）。token は `tools/tokens` の total 列と calls 列。完了率は C1 と同じ。
### C4：done コマンドの形（1 本のテストか、grep の連結か）
なぜ効きそうか：done の失敗は差し戻しの主因で、差し戻しごとに worker と main の token が増える。現行は「コピペで実行でき、exit code か出力で合否がわかる」とだけ決めていて、形は決めていない。
水準：既存テスト 1 本を指す / grep の連結 3 行 / 出力と exit code の両方を見る複合
測り方：main が done を再実行した結果（exit code と出力）を、会話に出た tool_result から取る。差し戻し回数は同じ Task の dispatch の数（`worker_done` に付ける `--dispatch-id` の値の種類数）。token は C1 と同じ。
### C5：報告の長さ上限（3 文か、10 行か、上限なしか）
なぜ効きそうか：現行は cco が「10 行以内」と定め、Orca の brief は「3 文の executive summary」を指定していて、上限が 2 つ効いている。報告は main の入力 token に乗るので、短いほど main の token は減るが、完了判定に要る情報が落ちるかもしれない。
水準：3 文 / 10 行 / 上限なし
測り方：worker の `orca orchestration send --type worker_done` の `--body` の文字数（worker の session jsonl の `Bash` の `tool_use` の `input.command` から取る）。main 側の受信は `orca orchestration check --json` の tool_result の長さ。完了率は報告の内容と done 再実行の結果が一致した割合。
### C6：worker のモデル（haiku / sonnet / opus）
なぜ効きそうか：モデルで単価と完了率が変わる。現行は worker を `--agent claude` で起動し、worker の model は決めていない（配送役は sonnet。Agent の `model` 入力に出る）。
水準：haiku / sonnet / opus（同じ Task を 3 水準で比べる）
測り方：`tools/tokens` の models 列（実際に使われたモデル名）と total 列。モデルの指定方法は CAO-130 で `orca orchestration worker-start` の help を実物で確認してから決める（ここでは断言しない）。完了率は初回 done の exit code。
### C7：worker に決めさせる範囲（決定済みの手順か、目的と制約だけか）
なぜ効きそうか：現行は「Change には決定済みの内容を書き、worker に決めさせない」と定めている。これを変える案：Change を目的と制約だけにして実装の仕方は worker に任せると、探索の token は増えるが、設計の手戻りは減るかもしれない。
水準：決定済みの手順（現行） / 目的と制約だけ / 目的と制約に候補案を 2 つ添える
測り方：worker の session jsonl の `tool_use` 件数と、`tools/tokens` の total 列（C3 と同じ取り方）。設計のずれは worker のコミットの `git show --stat` の変更ファイル数で見る。完了率は初回 done の exit code。
### C8：質問の許可（質問させない / 配送役が答える / main まで上げる）
なぜ効きそうか：質問の往復は、worker・配送役・main のそれぞれで token を使う。現行は worker が `orca orchestration ask` で質問し、配送役が仕様から答え、答えられないものだけ main に上げる。
水準：質問させない（推測して進め、報告に書く） / 配送役が答える（現行） / main まで上げる
測り方：worker の session jsonl で `Bash` の `input.command` に `orca orchestration ask` を含む件数。配送役の session にある同じ件数。token は worker・配送役・main の各 unit の total。完了率は C1 と同じ。

## token 量の取り方

この節は、実物のファイルを開いて確かめた結果だけを書く。

単位と場所：
- main：その session の jsonl。`~/.claude/projects/<作業ディレクトリをエンコードしたパス>/<session-id>.jsonl`。この worktree では `-Users-yasumasa-takemura-orca-workspaces-codingagentenv-cao-52-ccoinorca-orca-claude-code-coding-agent-o/`。ただし session 全体の値なので、dispatch の分だけを取るには前後で切る必要がある（行の `timestamp` で切る案は CAO-130 で確かめる）。
- 配送役：main の session の `<session-id>/subagents/agent-<id>.jsonl`。同じ場所の `agent-<id>.meta.json` の `description`・`model`・`toolUseId` で識別する。`toolUseId` は main の `assistant` 行にある `Agent` の `tool_use` の `id` と一致し（この session の `toolu_015kikCHiWSebri2VXmdo1Vr`）、そこの `input.model` と `input.description` にも同じ値がある。
- worker：Orca の worker が作業した worktree のエンコードしたパスの下にある session jsonl。この CAO-129 の worker の session `0758c055-d0b2-4007-b7c8-204c2c3e528c.jsonl` は、この worktree のパスの下にあることを確かめた。

足す field：`type` が `assistant` の行の `message.usage` にある `input_tokens`・`output_tokens`・`cache_read_input_tokens`・`cache_creation_input_tokens`。`message.model` で model を取る。

重複の罠：1 回の API 呼び出しは content block ごとに複数の `assistant` 行になり、`message.id` が同じ行の `usage` は input と cache が同じで、output だけが増える。この session では `message.id` が `bSWTQL2P` で始まる 2 行は output が 16 → 228、`bp7deGzZ` で始まる 3 行は cache_read_input_tokens が 3 行とも 38712。全行を単純に足すと input と cache を重複して数える。`agent/tokens/aggregate.ts` の `calls()`（26〜43 行目）は `(message.id, requestId)` で束ね、output_tokens が最大の行を採る。

確かめた実行：`bash tools/tokens 0758c055-d0b2-4007-b7c8-204c2c3e528c 8826623b-1eb6-4c4b-8cee-de97da8ab7b0` の結果は、この CAO-129 の worker の session が total 873,060（12 calls）、main の session が total 1,202,079（15 calls）、配送役 `agent-af73df8485438114b`（description「Deliver CAO-129 via Orca worker」、sonnet-5-5）が total 189,336（4 calls）。

使わないもの：repo の `.claude/token-usage.jsonl` は、この worktree にも git の追跡にも無い。repo のどのコードもこのファイルに書いていない（`git grep` で確認）。handoff に untracked とあるだけで、field は確かめていないので、取り方には使わない。

未確認：Orca の `check --json` の field 名（`outcome` や `dispatchId` の JSON 上の名前）は、この worker では読んでいない。完了率はこれに頼らず、main が done を再実行した結果で取る。
