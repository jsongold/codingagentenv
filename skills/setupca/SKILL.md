---
name: setupca
description: project ごとのルールを対話で決めて harness.json を作る・更新する。今聞くルールは tickets（チケット管理の場所と spec の見分け方）だけ。Global（自分の全 project の既定値）か個別（この project）かを選ぶ。「harness の設定」「チケット管理の場所を変えたい」「spec の見分け方を変えたい」と言われたら使う。
argument-hint: "[global | project]"
---

# setupca — project のルールを対話で決めて harness.json を作る

harness.json は project ごとのルールを書くファイル。トップレベルのキーがルールの種類で、今は `tickets` だけ。ほかのキーは読まずにそのまま残す。

- Input：層（global / project）と各項目の値（対話で聞く）
- Output：
  - global：codingagentenv repo の `global/harness.json`（既定値。展開はユーザーが `bin/codingenv install`）
  - project：その project の `.claude/harness.json`
- 優先順位：project > global > 未設定（何もしない）

## 手順
1. 層を聞く（引数があれば使う）。
2. 対象ファイルを読む。あれば今の値を見せる。
3. tickets の項目を 1 つずつ聞く。今の値があれば選択肢の先頭に置く。
   - `tickets.system`：`github` / `jira` / なし（`null`）
   - `jira` なら `tickets.project`（例 `BATCH`）
   - `github` / `jira` なら spec の見分け方 `tickets.spec.label`（例 `doc:spec`）。無しも選べる（spec を探さない）
4. 書く。ほかのトップレベルのキーは消さない。例：`{"tickets":{"system":"github","spec":{"label":"doc:spec"}}}`。global なら `bin/codingenv install` をユーザーに依頼する。

## やらないこと
- `~/.claude/` への直接書き込み。
- tickets 以外のルールを作ること（後で足す）。
- 設定を読む側の処理（hook・skill の仕事）。
