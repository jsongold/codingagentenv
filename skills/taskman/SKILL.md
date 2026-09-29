---
name: taskman
description: 依頼から Task を作り出す。依頼を分類し、ロジックを含むなら /design で設計してから /changegraph で Task に割る。ロジックを含まない依頼は Task にせず、そのまま /dispatch に渡す。複数ステップの実装・調査を始める前に使う。
argument-hint: "[やりたいこと | #issue] [auto=true|false]"
---

# taskman — 依頼から Task を作り出す

責任は Task を作り出すところまで。配送（`/dispatch`）以降は扱わない。

流れ：
- ロジックあり：依頼 → classify → `/design` → 設計 → `/changegraph` → Task 一覧 → `/dispatch`
- ロジックなし：依頼 → classify → 依頼のまま `/dispatch`

## 手順
1. classify：依頼がロジックを含むかを判定する。skill・hook の手順のように、コード以外の形で書かれたロジックも含む。
   - 含まない（文言・docs・設定値・リネームなど）：依頼をそのまま `/dispatch` に渡して終わる。
   - 含む：手順 2 へ。
2. `/design` を呼び、設計を得る。
3. `/changegraph` に設計を渡し、Task 一覧を得る。
4. 承認の扱いを `auto` で切り替える。
   - `auto=false`（既定）：Task 一覧を見せ、OK が出るまで次に進まない。
   - `auto=true`：Task 一覧を見せて、そのまま次に進む。
5. Task 一覧を `/dispatch` に渡す。

## やらないこと
- 設計の中身を決める（`/design` の仕事）。
- Task への分割（`/changegraph` の仕事）。
- 登録・配送（`/dispatch` の仕事）。
