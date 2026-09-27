# ADR-0006: task 完了時にトークン消費を hook で集計・記録する

- 日付：2026-09-20
- 状態：採用

## 文脈
task 単位でどれだけトークンを使ったか（main / subagent、model別）を後から分析したい。既存の `hooks/harness-task-completed.sh`（ADR-0004）は TaskCompleted で発火し、`transcript_path`（main セッションの JSONL）を受け取れる（docs/context/token-saving-spec.md #4 で原文確認済み）。

依頼時点の指示は ADR番号を「0005」としていたが、0005 は既に `0005-repo-only-deploy-command.md`（採用）で使用済みだった。番号の重複は避け、この ADR は 0006 として追加する。

transcript の JSONL 行フォーマット（`.message.model` / `.message.usage.{input,output,cache_read_input_tokens,cache_creation_input_tokens}`）は公式 docs に記載がなく、実ファイルで確認した形に依存する。将来のフォーマット変更で壊れる可能性がある。

**差し戻しで判明した二重計上**：1つの assistant message は content block ごとに複数の JSONL 行へ分かれて書かれ、各行が同じ `.message.id` と（ほぼ同じ）usage を持つ。実測で usage 行90に対し distinct な `.message.id` は41。行ごとに単純加算すると turn あたりのトークン数が水増しされる。

## 決定
- `hooks/harness-task-completed.sh` に `log_token_usage` を追加し、completed を **許可する** 2つの経路（`CLAUDE_CODE_TASK_LIST_ID` 未設定で未強制のケース／VERIFIED 行があるケース）でだけ呼ぶ。exit 2 で拒否する経路では呼ばない。
- transcript の各行は、集計の前に `.message.id` ごとに1件へ畳む（同じ id が複数行ある場合は最後の行の usage を採用。ストリーミング中の値より最終行が確定値のため）。`.message.id` が無い行はそのまま1行=1件として扱う（フォーマット変更で全滅させないため）。畳んだ後のレコードを jq でモデル別に集計（`group_by(.model)` 相当）し、input/output/cache_read/cache_creation の合計とその model の件数（畳んだ後の件数）を出す。subagent は `${transcript_path%.jsonl}/subagents/*.jsonl` を全て連結して同じ集計にかけ、`subagents` バケットとして別に記録する。
- 出力は `$CLAUDE_PROJECT_DIR`（無ければ入力の `cwd`）直下の `.claude/token-usage.jsonl` に1行追記。**opt-in**：このファイルが既に存在する project でだけ書く。存在しなければ何もしない。global hook が他 repo に未追跡ファイルを撒かないため。
- 集計・書き込みの失敗（jq 無し、project_dir 不明、transcript 無し、jq のパースエラー、書き込み不可）はすべて `return 0` で無視し、既存の exit code / stderr 文言を一切変えない。jq フィルタは `?` で各フィールドアクセスを防御し、失敗時は空 `{}` にフォールバックする。
- **transcript_path が指す先が存在しない場合の設計判断**：ログ行自体は書く（`main: {}`, `subagents: {}` の空集計で）。理由：completed イベントの記録漏れよりも「measurement can fail, but the event still happened」を残す方が分析時に扱いやすい（file が無かった1行だけ後から除外すればよい）。ログを書かない案との二択だったが、コードがシンプルになる（早期 return の分岐が1つ減る）ことも決め手にした。
- この repo では `.claude/token-usage.jsonl` を git 管理し、計測履歴を残す（`.gitignore` に追加しない）。

## 検討して却下した案
| 案 | 却下理由 |
|---|---|
| OpenTelemetry（`CLAUDE_CODE_ENABLE_TELEMETRY`） | 公式仕様として存在するが、OTLP collector を個人ハーネスのために常駐させるのは重い。task 単位の集計には hook 側での transcript 読み込みで十分 |
| statusLine 経由で出力 | `statusLine` は他ツール（既存の外部ステータスライン設定）が占有済みで、統計出力の置き場として使えない |
| task の JSON メタデータ（`~/.claude/tasks/`）から使用量を読む | ADR-0004 と同じ理由で、ファイル形式が docs に無く不安定。TaskCompleted の payload にトークン使用量は含まれない（transcript を読むしかない） |
| 全 project で無条件に書き込む | 未追跡ファイルを他 repo に撒く（ADR-0004 の「全 project で強制する」却下と同じ理由）。opt-in ファイルの存在チェックで解決 |

## 影響
- 良い影響：task 単位のトークン消費が `.claude/token-usage.jsonl` に蓄積され、後から分析できる。集計失敗が completed の可否に一切影響しない
- 受け入れたトレードオフ：値はセッション累積であり、task 単体の消費は前後の行の差分を取らないと出せない。transcript フォーマットが変わると静かに空集計になる（気付きにくい）。opt-in ファイルの作成をユーザーが忘れると記録されない
- 再検討する条件：transcript フォーマット変更でテスト fixture の期待値が壊れたら、その時点の実ファイルで形を取り直す。複数 project で分析するようになったら、model 名の揺れ（バージョン違い）の正規化を検討する
