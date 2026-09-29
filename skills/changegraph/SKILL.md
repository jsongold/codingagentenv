---
name: changegraph
description: 設計（/design の出力）を適切なタスク粒度に分割し、Task 一覧を出す。PR 単位のノードと触るファイルから、並列 worktree で安全に同時進行できる組（wave）も計算する。/taskman から呼ばれ、Task 一覧は /dispatch に渡る。
argument-hint: "[名前] [#issue | 設計のパス]"
---

# changegraph — 設計を Task に割る

目的：worktree 5〜10 本で並列開発してもコンフリクトを起こさず、PR と Task を小さく保つ。

- Input：設計（流れ・境界・Node Graph）
- Output：Task 一覧

## 手順
1. 設計の境界ごとに、触るファイルをコードを読んで列挙する（テストファイルも含める）。推測で書かない。
2. 境界を PR 単位のノードに束ねる。1 ノード = 1 PR = 差分目安 400 行以内。
   - 共有ファイル（登録一覧・index・ルート一覧・設定・lock）を複数ノードが触るなら、その追記だけを先行ノードに切り出すか、自動登録に変える。
   - 大きいファイルへの書き足しは、新しいファイルに分けられないか先に検討する。
3. `.claude/changegraph/<名前>.yaml` に書く（プロジェクトにコミットしない）。

   ```yaml
   nodes:
     - id: c1
       pr_title: "..."
       files: [path/a.py, tests/test_a.py]
       depends_on: []
       done: false        # マージ済みになったら true
   ```
4. wave を計算する：`uv run --with pyyaml python ~/.claude/skills/changegraph/waves.py .claude/changegraph/<名前>.yaml`
   - ルール1：files が 1 つでも重なるノードは同じ wave に入らない（`serial` 行に理由が出る）。
   - ルール2：depends_on が全部 done か前の wave のノードだけが入る。
   - wave が 1 ノードずつしか並ばない、または serial が多いときは、手順 2 に戻って分割し直す。
5. ノードを Task に割る。1 Task = Subagent 1 回で終わり、1 コミットで完結する大きさ。既定は 1 境界 = 1 Task。Task は design と同じ書式で書き、ノードと wave を添える。

   ```
   ### <Task 名>（c1 / wave 1）
   - 概要：
   - Scope：
   - Input：
   - Output：
   - 触るファイル：
   ```
6. Task 一覧を返す。

## やらないこと
- 設計（`/design` の仕事）。
- 承認（`/taskman` の `auto`）。
- 登録と配送（`/dispatch` の仕事）。
- 衝突相手を手で書く項目（conflicts_with）を持たない。重なりは files から計算する。
