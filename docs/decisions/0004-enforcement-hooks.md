# ADR-0004: 文脈の復元と検証済み完了を hook で強制する

- 日付：2026-09-20
- 状態：採用

## 文脈
ハーネスは skill と CLAUDE.md の文章だけで、守るかどうかはモデル次第だった。抜けると困るのは2点：`/pickup` を打ち忘れて文脈なしで作業を始めること、未検証のまま task を completed にすること。

公式 docs（code.claude.com/docs/en/hooks.md の原文）で確認した事実：
- SessionStart は stdout を Claude の文脈に追加する。`source` は startup / resume / clear / compact / fork。毎セッション走るので速く保つこと。
- TaskCompleted は `TaskUpdate` で completed にするときに発火する。exit 2 で完了を阻止し、stderr がモデルに返る。matcher は無い。payload は `task_id`、`task_subject`、任意で `task_description`。metadata は含まれない。
- WebFetch の要約は TaskCompleted について「exit 2 は効かない」と誤って答えた。原文を直接読んで訂正した。docs の要約を鵜呑みにしない。

## 決定
- `hooks/harness-session-start.sh`：startup / clear / compact のとき、project の PROGRESS.md と ADR 一覧を文脈に入れ、`/pickup` の手順を促す。PROGRESS.md が無い、または未記入テンプレートの project では何も出さない。
- `hooks/harness-task-completed.sh`：description に `VERIFIED: <コマンド> -> <結果>`（または `検証:`）の行が無い task の completed を exit 2 で拒否する。`CLAUDE_CODE_TASK_LIST_ID` が設定された project だけで強制する（ADR-0003 でハーネス採用の目印）。
- 正本はこの repo の `hooks/`。`bash hooks/install.sh` が `~/.claude/hooks/` へ symlink し、`~/.claude/settings.json` に追記する（冪等、バックアップあり、既存 hook は変更しない）。テストは `bash test/hooks.test.sh`。

## 検討して却下した案
| 案 | 却下理由 |
|---|---|
| hook が task に書かれた検証コマンドを自分で実行する | task の文面はモデルが書く。hook 経由だと権限確認なしで任意コマンドが走る |
| task の JSON（`~/.claude/tasks/`）を読んで metadata で判定する | ファイル形式は docs に無い。payload の `task_description` は docs に載っている |
| 完了時に git の作業ツリーが clean であることを要求する | 並列 dispatch 中は他タスクの変更で常に dirty になり、誤検知する |
| PreToolUse で main の直接実装を止める | 小修正は main が直接やる決定（ADR-0003）と衝突し、誤検知が多い |
| 全 project で強制する | invoice など既存 skill の軽いタスク管理まで止めてしまう |

## 影響
- 良い影響：`/pickup` を忘れても文脈が入る。検証の記録が task に必ず残る
- 受け入れたトレードオフ：VERIFIED 行は自己申告で、嘘は防げない。防げるのは「検証の工程を飛ばすこと」だけ。description の追記と completed を別の `TaskUpdate` に分ける手間が増える
- 再検討する条件：同じ `TaskUpdate` で description と status を更新しても payload に反映されると確認できたら、2回に分ける指示を外す。自己申告で問題が起きたら、許可リスト方式でのコマンド実行を検討する
