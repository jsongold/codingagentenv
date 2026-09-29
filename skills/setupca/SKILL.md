---
name: setupca
description: ハーネスの設定データ（harness.json）を対話で作る・更新する。Global（自分の全 project）か個別（この project）かを選び、項目を 1 つずつ聞いて書く。「harness の設定」「spec の置き場を変えたい」と言われたら使う。
argument-hint: "[global | project]"
---

# setupca — harness.json を対話で作る

- Input：層（global / project）と各項目の値（対話で聞く）
- Output：
  - global：codingagentenv repo の `global/harness.json`（展開はユーザーが `bin/codingenv install`）
  - project：その project の `.claude/harness.json`
- 優先順位：project > global > 未設定（何もしない）

## 手順
1. 層を聞く（引数があれば使う）。
2. 対象ファイルを読む。あれば今の値を見せる。
3. 項目を 1 つずつ聞く。今の値があれば選択肢の先頭に置く。
   - `spec.store`：`issues` / `files` / なし
   - `issues` なら `spec.label`（例 `doc:spec`）
   - `files` なら `spec.dir`（例 `docs/decisions`）
4. 書く。global なら `bin/codingenv install` をユーザーに依頼する。

## やらないこと
- `~/.claude/` への直接書き込み。
- 設定を読む側の処理（hook・skill の仕事）。
