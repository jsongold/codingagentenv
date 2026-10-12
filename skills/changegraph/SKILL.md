---
name: changegraph
description: 設計（/design の出力）を適切なタスク粒度に分割し、Task 一覧を出す。PR 単位のノードと触るファイルから、並列 worktree で安全に同時進行できる組（wave）も計算する。/taskman から呼ばれ、Task 一覧は /cco に渡る。
argument-hint: "[名前] [#issue | 設計のパス]"
---

# changegraph — 設計を Task に割る

目的：worktree 5〜10 本で並列開発してもコンフリクトを起こさず、PR と Task を小さく保つ。

- Input：設計（流れ・境界・Node Graph）
- Output：Task 一覧

## 手順
1. 設計の境界ごとに、触るファイルをコードを読んで列挙する（テストファイルも含める）。推測で書かない。
2. 境界を PR 単位のノードに束ねる。1 ノード = 1 PR = 差分目安 400 行以内（基準は下の「Task に分ける基準」）。
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
4. wave を計算する（条件は下の「並列にしてよい条件」）：`uv run --with pyyaml python ~/.claude/skills/changegraph/waves.py .claude/changegraph/<名前>.yaml`
   - ルール1：files が 1 つでも重なるノードは同じ wave に入らない（`serial` 行に理由が出る）。
   - ルール2：depends_on が全部 done か前の wave のノードだけが入る。
   - wave が 1 ノードずつしか並ばない、または serial が多いときは、手順 2 に戻って分割し直す。
5. ノードを Task に割る。1 Task = Subagent 1 回で終わり、1 コミットで完結する大きさ。既定は 1 境界 = 1 Task（基準は下の「Task に分ける基準」）。Task は design と同じ書式で書き、ノードと wave を添える。

   ```
   ### <Task 名>（c1 / wave 1）
   - 概要：
   - Scope：
   - Input：
   - Output：
   - 触るファイル：
   ```
6. Task 一覧を返す。

## Task に分ける基準
- 大きさ：1 Task = worker 1 回で終わり、1 コミットで完結する。1 PR（ノード）の差分は 400 行以内（テスト・lock ファイル除く）。超えるなら分ける。
- 切り口：既定は 1 境界 = 1 Task。対象は、設計の「新規」の境界と「Input・Output が変わる既存」の境界。
- 完了の観測：Task ごとに done コマンドを 1 つ書ける（完了が単独で観測できる）。書けないなら切り方を変える。
- まとめる条件：同じファイルを触る Task、または片方だけでは done コマンドが通らない Task は、合わせて大きさの上限に収まるなら 1 つにまとめる。収まらないなら、共有する部分を先行 Task に切り出す。

## 並列にしてよい条件
2 つの Task は、次の 2 つを両方満たすときだけ並列にしてよい（同じ wave に入れてよい）。1 つでも満たさなければ直列にする。
1. 編集範囲：触るファイル（`files`）が 1 つも重ならない。テストファイル・共有ファイル・lock も `files` に入れて比べる。
2. 依存関係：どちらも相手に依存しない。相手の Output を Input にする Task は依存している（`depends_on` に書く）。`depends_on` を間接にたどって相手に届く場合も依存とみなす。

判定は `waves.py` が `files` と `depends_on` から行う。結果を手で上書きしない。

例（3 Task。依存はどれも無し）：

```yaml
nodes:
  - {id: A, files: [src/a.py, tests/test_a.py], depends_on: []}
  - {id: B, files: [src/b.py, tests/test_b.py], depends_on: []}
  - {id: C, files: [src/a.py, tests/test_c.py], depends_on: []}
```
- A と B：files が重ならず、依存も無い → 並列（wave 1）。
- A と C：`src/a.py` が重なる → 直列（C は wave 2。`serial A <-> C` と出る）。

## やらないこと
- 設計（`/design` の仕事）。
- 承認（`/taskman` の `auto`）。
- 登録と配送（`/cco` の仕事）。
- 衝突相手を手で書く項目（conflicts_with）を持たない。重なりは files から計算する。
