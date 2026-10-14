# subagent に渡す条件の測定結果（CAO-130）

目的：CAO-129 の条件候補 C1〜C8 を、同じ benchmark Task で 1 度に 1 条件だけ変えて測り、token 量と初回の done 通過に差が出るかを記録する。

- 全 run は 20 行（測定 19 本、未測定 1 行）。1 水準あたり n=1。B0 だけ n=3。
- 測り方は `docs/research/subagent-conditions.md` の「token 量の取り方」に従う。worker_total_tokens は `bash tools/tokens <worker の session id>` の total。first_done_exit は配送役が 1 回目に実行した done コマンドの exit code。
- benchmark Task：bash の小さな関数 6 つを `bench/` 配下に書き、repo の外にある受け入れテスト 1 本（`PASS 6/6`、exit 0）を通す。成果物は捨てる。worker は条件ごとに新しい worktree で起動し、コミット・push・PR はしない。
- worker の既定モデルは haiku（`models` 列で確認）。C6 だけ `worker-start --model` で指定した。

## B0 の揺れ

- B0 3 本の worker_total_tokens：min 295,537 〜 max 683,579（median 352,938）。
- B0 3 本の worker_calls：min 5 〜 max 11。
- 同じ条件でも max は min の 2.31 倍。揺れの幅（max - min）は 388,042。

## 条件ごとの結果

### B0（基準）

| 水準 | run | worker_total_tokens | worker_calls | tool_uses | first_done_exit | redo_count | models | spec_lines | report_chars | notes |
|---|---|---|---|---|---|---|---|---|---|---|
| B0 基準（Target 3 個・約 60 行・散文・done は test.sh 1 本・報告 10 行以内・配送役が答える） | 1 | 683,579 | 11 | 12 | 0 | 0 | claude-haiku-5-5 | 63 | 351 | wave1; sid=f4e33973-e120-479e-89c8-6707935a536f; full_test_exit=0; n=1 |
| B0 基準（Target 3 個・約 60 行・散文・done は test.sh 1 本・報告 10 行以内・配送役が答える） | 2 | 352,938 | 6 | 7 | 0 | 0 | claude-haiku-5-5 | 63 | 267 | wave2; same condition as run 1 (spread check); sid=2de3fed2-8bb8-48d3-b6e8-f2ea5e155bb2; full_test_exit=0; ask_count=0; files=misc.sh,num.sh,text.sh; n=1 |
| B0 基準（Target 3 個・約 60 行・散文・done は test.sh 1 本・報告 10 行以内・配送役が答える） | 3 | 295,537 | 5 | 7 | 0 | 0 | claude-haiku-5-5 | 63 | 351 | wave2; same condition as run 1 (spread check); sid=3e778b1c-11df-4f0c-a522-7b731fab2bd5; full_test_exit=0; ask_count=0; files=misc.sh,num.sh,text.sh; n=1 |

### C1：Task の粒度（Target のファイル数）

| 水準 | run | worker_total_tokens | worker_calls | tool_uses | first_done_exit | redo_count | models | spec_lines | report_chars | notes |
|---|---|---|---|---|---|---|---|---|---|---|
| Target 1 個 | 1 | 300,716 | 5 | 5 | 0 | 0 | claude-haiku-5-5 | 61 | 308 | wave1; sid=903c12e3-9cc6-4f34-8766-d203a72fe45c; full_test_exit=0; n=1 |
| Target 6 個 | 1 | 433,347 | 7 | 11 | 0 | 0 | claude-haiku-5-5 | 66 | 328 | wave1; sid=e486707e-9b31-4ef1-80c5-87ab22d3c5d5; full_test_exit=0; n=1 |

### C2：渡す文脈の量（仕様本文の行数）

| 水準 | run | worker_total_tokens | worker_calls | tool_uses | first_done_exit | redo_count | models | spec_lines | report_chars | notes |
|---|---|---|---|---|---|---|---|---|---|---|
| 仕様 約 20 行 | 1 | 358,935 | 6 | 8 | 0 | 0 | claude-haiku-5-5 | 23 | 178 | wave1; sid=c63dee96-b9f1-48ee-a7d7-a017c3a32d18; full_test_exit=0; n=1 |
| 仕様 約 120 行 | 1 | 354,162 | 6 | 7 | 0 | 0 | claude-haiku-5-5 | 115 | 352 | wave1; sid=b2eed84f-f50d-4a88-9d8e-692a622af83c; full_test_exit=0; n=1 |

### C3：Change の書き方

| 水準 | run | worker_total_tokens | worker_calls | tool_uses | first_done_exit | redo_count | models | spec_lines | report_chars | notes |
|---|---|---|---|---|---|---|---|---|---|---|
| 番号付き手順 3 件 | 1 | 354,384 | 6 | 8 | 0 | 0 | claude-haiku-5-5 | 65 | 390 | wave1; sid=3964847e-2654-44af-85d9-f77d5d675542; full_test_exit=0; n=1 |
| 番号付き手順 6 件 | 1 | 989,530 | 16 | 15 | 0 | 0 | claude-haiku-5-5 | 68 | 389 | wave1; sid=28872401-4387-4f1f-b49a-6bc7efd0243d; full_test_exit=0; n=1 |

### C4：done コマンドの形

| 水準 | run | worker_total_tokens | worker_calls | tool_uses | first_done_exit | redo_count | models | spec_lines | report_chars | notes |
|---|---|---|---|---|---|---|---|---|---|---|
| grep の連結 3 行 | 1 | 347,293 | 6 | 5 | 0 | 0 | claude-haiku-5-5 | 65 | 535 | wave1; sid=2426b908-4ecc-45fa-8ab3-93a0e89e3397; full_test_exit=0; n=1 |
| 出力と exit code の複合 | 1 | 290,945 | 5 | 7 | 0 | 0 | claude-haiku-5-5 | 63 | 440 | wave1; sid=dfff9c19-e75d-4501-abda-3767cc4440dc; full_test_exit=0; n=1 |

### C5：報告の長さ上限

| 水準 | run | worker_total_tokens | worker_calls | tool_uses | first_done_exit | redo_count | models | spec_lines | report_chars | notes |
|---|---|---|---|---|---|---|---|---|---|---|
| 報告 3 文 | 1 | 415,575 | 7 | 9 | 0 | 0 | claude-haiku-5-5 | 63 | 369 | wave2; report limit 3 sentences; report was 3 sentences; sid=1d29c334-6291-4ba0-b7b7-ced5eba75696; full_test_exit=0; ask_count=0; files=misc.sh,num.sh,text.sh; n=1 |
| 報告の上限なし | 1 | 289,563 | 5 | 5 | 0 | 0 | claude-haiku-5-5 | 63 | 296 | wave2; report limit removed; sid=d7fdb456-934d-45fb-93c9-e87e55f8e2cb; full_test_exit=0; ask_count=0; files=misc.sh,num.sh,text.sh; n=1 |

### C6：worker のモデル

| 水準 | run | worker_total_tokens | worker_calls | tool_uses | first_done_exit | redo_count | models | spec_lines | report_chars | notes |
|---|---|---|---|---|---|---|---|---|---|---|
| worker = sonnet（B0 は haiku） | 1 | 166,001 | 3 | 2 | 0 | 0 | claude-sonnet-5-5 | 63 | 148 | wave2; worker-start --model sonnet; launch.effective.model=sonnet; sid=209f91d7-65e7-4aa0-8ecd-2e4cd86e7b58; full_test_exit=0; ask_count=0; files=misc.sh,num.sh,text.sh; n=1 |
| worker = opus | 1 | 277,709 | 5 | 4 | 0 | 0 | claude-opus-5-5 | 63 | 456 | wave2; worker-start --model opus; launch.effective.model=opus; sid=77137286-0238-43f2-ba31-66846fe1fc7c; full_test_exit=0; ask_count=0; files=misc.sh,num.sh,text.sh; n=1 |

### C7：worker に決めさせる範囲

| 水準 | run | worker_total_tokens | worker_calls | tool_uses | first_done_exit | redo_count | models | spec_lines | report_chars | notes |
|---|---|---|---|---|---|---|---|---|---|---|
| 目的と制約だけ | 1 | 349,969 | 6 | 6 | 0 | 0 | claude-haiku-5-5 | 61 | 376 | wave2; Change = purpose+constraints only (no file split / procedure); worker chose 1 file bench/b_funcs.sh; Target line says new files under bench/; sid=1dc6ff69-2b54-4ab5-a2a7-85f4d1f8549e; full_test_exit=0; ask_count=0; files=b_funcs.sh; n=1 |
| 目的と制約 + 候補案 2 つ | 1 | 354,215 | 6 | 8 | 0 | 0 | claude-haiku-5-5 | 61 | 419 | wave2; purpose+constraints plus 2 candidates (A 1 file, B 3 files); worker chose B (3 files); sid=1272a075-5c0e-4019-8d5b-de600af6f6d5; full_test_exit=0; ask_count=0; files=misc.sh,num.sh,text.sh; n=1 |

### C8：質問の許可

| 水準 | run | worker_total_tokens | worker_calls | tool_uses | first_done_exit | redo_count | models | spec_lines | report_chars | notes |
|---|---|---|---|---|---|---|---|---|---|---|
| 配送役が答える（未決定点あり） | 1 | 291,506 | 5 | 8 | 0 | 0 | claude-haiku-5-5 | 63 | 399 | wave2; spec adds 1 undecided point (b_max with no args); questions allowed (baseline); worker did not ask and did not mention it in report; so no reply was needed; sid=423b0e40-87da-4c96-a30c-798b23549632; full_test_exit=0; ask_count=0; files=misc.sh,num.sh,text.sh; n=1 |
| 質問させない（未決定点あり） | 1 | 530,958 | 9 | 8 | 0 | 0 | claude-haiku-5-5 | 64 | 462 | wave2; same undecided point; spec forbids asking and requires reporting the guess; worker reported b_max no-arg prints empty line; sid=07e61bd7-e6fb-4b79-a446-39d9b9c5a584; full_test_exit=0; ask_count=0; files=misc.sh,num.sh,text.sh; n=1 |
| main まで上げる | 1 | NA | NA | NA | NA | NA | NA | NA | NA | wave2; 未測定：run 中に main へ問い合わせる手段が無い |

## 読めること

- 前提：1 水準 n=1。B0 の揺れの幅（max - min = 388,042）より、B0 の median との差が小さい水準は「差とは言えない」と読む。B0 の揺れは、1 本（run 1）だけ大きく外れたことで広がっている。
- 完了率：測定した 19 本のうち first_done_exit が 0 だったのは 19 本、redo_count はすべて 0。全 run が初回で通ったので、完了率では条件の差が出ていない（wave 1 の 9 本も同じ）。差が出うるのは token 量だけで、この benchmark Task の難しさでは完了率を測れていない。

### C1：Task の粒度（Target のファイル数）
- Target 1 個：worker_total_tokens 300,716（B0 の median の 0.85 倍）：B0 の median との差 52,222 が B0 の揺れの幅 388,042 より小さい。差とは言えない
- Target 6 個：worker_total_tokens 433,347（B0 の median の 1.23 倍）：B0 の median との差 80,409 が B0 の揺れの幅 388,042 より小さい。差とは言えない

### C2：渡す文脈の量（仕様本文の行数）
- 仕様 約 20 行：worker_total_tokens 358,935（B0 の median の 1.02 倍）：B0 の median との差 5,997 が B0 の揺れの幅 388,042 より小さい。差とは言えない
- 仕様 約 120 行：worker_total_tokens 354,162（B0 の median の 1.00 倍）：B0 の median との差 1,224 が B0 の揺れの幅 388,042 より小さい。差とは言えない

### C3：Change の書き方
- 番号付き手順 3 件：worker_total_tokens 354,384（B0 の median の 1.00 倍）：B0 の median との差 1,446 が B0 の揺れの幅 388,042 より小さい。差とは言えない
- 番号付き手順 6 件：worker_total_tokens 989,530（B0 の median の 2.80 倍）：B0 の median より 636,592 高く、B0 の揺れの幅 388,042 を超える。差の可能性はあるが n=1

### C4：done コマンドの形
- grep の連結 3 行：worker_total_tokens 347,293（B0 の median の 0.98 倍）：B0 の median との差 5,645 が B0 の揺れの幅 388,042 より小さい。差とは言えない
- 出力と exit code の複合：worker_total_tokens 290,945（B0 の median の 0.82 倍）：B0 の median との差 61,993 が B0 の揺れの幅 388,042 より小さい。差とは言えない

### C5：報告の長さ上限
- 報告 3 文：worker_total_tokens 415,575（B0 の median の 1.18 倍）：B0 の median との差 62,637 が B0 の揺れの幅 388,042 より小さい。差とは言えない
- 報告の上限なし：worker_total_tokens 289,563（B0 の median の 0.82 倍）：B0 の median との差 63,375 が B0 の揺れの幅 388,042 より小さい。差とは言えない

### C6：worker のモデル
- worker = sonnet（B0 は haiku）：worker_total_tokens 166,001（B0 の median の 0.47 倍）：B0 の median との差 186,937 が B0 の揺れの幅 388,042 より小さい。差とは言えない
- worker = opus：worker_total_tokens 277,709（B0 の median の 0.79 倍）：B0 の median との差 75,229 が B0 の揺れの幅 388,042 より小さい。差とは言えない
- 注意：token 数は model の違いで単価が違う。この表は token 数だけで、費用の比は読めない。

### C7：worker に決めさせる範囲
- 目的と制約だけ：worker_total_tokens 349,969（B0 の median の 0.99 倍）：B0 の median との差 2,969 が B0 の揺れの幅 388,042 より小さい。差とは言えない
- 目的と制約 + 候補案 2 つ：worker_total_tokens 354,215（B0 の median の 1.00 倍）：B0 の median との差 1,277 が B0 の揺れの幅 388,042 より小さい。差とは言えない
- 注意：worker が選んだファイル構成は notes にある。done は repo の外のテストなので、構成の違いは done に影響しない。

### C8：質問の許可
- 配送役が答える（未決定点あり）：worker_total_tokens 291,506（B0 の median の 0.83 倍）：B0 の median との差 61,432 が B0 の揺れの幅 388,042 より小さい。差とは言えない。worker の質問（`orca orchestration ask`）は 0 回
- 質問させない（未決定点あり）：worker_total_tokens 530,958（B0 の median の 1.50 倍）：B0 の median との差 178,020 が B0 の揺れの幅 388,042 より小さい。差とは言えない。worker の質問（`orca orchestration ask`）は 0 回
- main まで上げる：未測定：run 中に main へ問い合わせる手段が無い
- 注意：どちらの worker も質問しなかったので、質問の往復が token に与える影響はこの run では測れていない。「質問させない」と「配送役が答える」の差は、質問が発生しなかった run 同士の揺れである。

## 未測定・限界

- C8「main まで上げる」：run 中に配送役が main と対話する手段が無いので実行していない（NA）。
- n=1 の水準がほとんどで、B0 の揺れ以内の差は区別できない。差を言うには同条件を複数本回す必要がある。
- main と配送役の token は測っていない（worker の token だけ）。報告を短くしたときの main 側の節約は読めない。
- benchmark Task は easy で全 run が初回で通る。完了率に効く条件は、より難しい Task でないと測れない。
