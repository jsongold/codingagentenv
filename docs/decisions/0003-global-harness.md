# ADR-0003: ハーネスは global に置き、各 project はデータだけを持つ

- 日付：2026-09-20
- 状態：採用（PROGRESS.md の決定事項「skill は project 側 `.claude/skills/` に置く」を置き換え。ADR-0002 は有効のまま）

## 文脈
この repo で作ったハーネスを、テンプレートをコピーした repo だけでなく、自分の全 project に効かせたい。project 側に skill を置く従来の決定では、コピーしていない repo には何も適用されない。

2026-09-20 にこのマシンで確認した事実：
- `~/.claude/skills/` に dispatch / handoff / pickup という名前の skill は無く、名前の衝突は起きない。
- `~/.claude/skills/` 配下の symlink された skill ディレクトリは読み込まれる。既存の `find-skills` が symlink で、skill 一覧に出ている。
- `~/.claude/settings.json` に `env` キーは無かった。
- `CLAUDE_CODE_TASK_LIST_ID` が無いと Task list は `~/.claude/tasks/<session-uuid>/` に作られ、セッション間で共有されない。したがって ID は project ごとに設定する必要があり、global には置けない。

## 決定
- 3つの skill はこの repo の `skills/` に置き（正本、git 管理）、`~/.claude/skills/` へ symlink する。この repo で二重に読み込まれないよう `.claude/skills/` からは移動する。
- `CLAUDE_CODE_ENABLE_TODO_TOOLS=1` は `~/.claude/settings.json` の `env` に書く。
- `CLAUDE_CODE_TASK_LIST_ID` は各 project の `.claude/settings.json` に残す。無ければ `/pickup` が project ディレクトリ名で追加する。
- dispatcher のルールは `~/.claude/CLAUDE.md` に書く。強さ（ユーザー決定）：複数ステップの実装・調査は必ず `/dispatch` を通す。単一ファイルの小修正と質問への回答は main が直接行う。
- 各 project が持つのはデータだけ：PROGRESS.md、docs/decisions/、docs/context/、TASK_LIST_ID の1行。skill はこれらのファイルがまだ無い状態でも動くこと。

## 検討して却下した案
| 案 | 却下理由 |
|---|---|
| テンプレートを各 project にコピーする（従来方式） | コピーが乖離する。修正のたびに全 project で同じ作業を繰り返すことになる |
| 小さな変更でも常に dispatch する | 1行修正に対してトークンと時間のコストが大きすぎる。invoice / timesheet など既存 skill の邪魔になる |
| ユーザーが明示的に `/dispatch` を呼んだときだけ dispatch する（CLAUDE.md にルールを書かない） | main が複数ステップの作業を自分で実装してしまい、ハーネスが使われない |
| symlink せず skill を `~/.claude/skills/` にコピーする | `~/.claude` は git 管理外。履歴もバックアップも残らない |

## 影響
- 良い影響：直す場所が1か所になる。ハーネスの変更履歴が git に残る
- 受け入れたトレードオフ：この repo を移動・削除すると symlink が切れる。新しいマシンでは README の install 手順が必要。`~/.claude/CLAUDE.md` の個人的なレスポンススタイルのルールは依然としてバージョン管理されない
- 再検討する条件：ハーネスを他人と共有する必要が出たとき（そのときは plugin としてパッケージ化する）。2台目のマシンを追加するとき
