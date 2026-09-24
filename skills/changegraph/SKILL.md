---
name: changegraph
description: 複数 PR に分かれる作業の着手前に ChangeGraph（PR 単位のノード・触るファイル・依存）を作り、並列 worktree で安全に同時進行できる組（wave）を計算する。並列開発・大きめのリファクタ・Issue を複数 PR に割るときに使う。1 PR で終わる作業には使わない。
argument-hint: "[名前] [やりたいこと]"
---

# changegraph — 並列 PR の計画

目的：worktree 5〜10 本で並列開発してもコンフリクトを起こさず、PR を小さく保つ。形式は ccpm のタスク形式を最小化したもの。

## 手順
1. 対象コードを読み、作業を PR 単位のノードに割る。1 ノード = 1 PR = 差分目安 400 行以内。
   - 触るファイルは実際にコードを読んで列挙する（テストファイルも含める）。推測で書かない。
   - 共有ファイル（登録一覧・index・ルート一覧・設定・lock）を複数ノードが触るなら、その追記だけを先行ノードに切り出すか、自動登録に変える。
   - 大きいファイルへの書き足しは、新しいファイルに分けられないか先に検討する。
2. `.claude/changegraph/<名前>.yaml` に書く（プロジェクトにコミットしない）。

   ```yaml
   nodes:
     - id: c1
       pr_title: "..."
       files: [path/a.py, tests/test_a.py]
       depends_on: []
       done: false        # マージ済みになったら true
   ```
3. wave を計算する：`uv run --with pyyaml python ~/.claude/skills/changegraph/waves.py .claude/changegraph/<名前>.yaml`
   - ルール1：files が 1 つでも重なるノードは同じ wave に入らない（`serial` 行に理由が出る）。
   - ルール2：depends_on が全部 done か前の wave のノードだけが入る。
   - wave が 1 ノードずつしか並ばない、または serial が多いときは、手順 1 に戻って分割し直す。
4. ユーザーにノード一覧と wave を見せ、承認を得てから着手する。
5. wave ごとにノード数ぶん worktree（1 ノード = 1 worktree = 1 PR）を立てて並列実行する。マージされたノードは `done: true` にして再計算し、次の wave に進む。

## やらないこと
- 衝突相手を手で書く項目（conflicts_with）を持たない。重なりは files から計算する。
- 実行中に files の外を触る必要が出たら、黙って触らず ChangeGraph を直して再計算する。
