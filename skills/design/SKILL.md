---
name: design
description: ロジックを含む依頼の境界（関数・モジュール・クラス・CLI など）を設計する。境界ごとに概要・Scope・Input / Output を書き、最後に Mermaid の Node Graph を載せる。/taskman から呼ばれ、出力の設計は /changegraph に渡る。DB スキーマとインフラには使わない。
argument-hint: "[やりたいこと | #issue]"
---

# design — 境界の連なりでコードを設計する

目的：後から読んだ人が、データの出どころと行き先を追えるコードにする。原則は harness の「コード設計」節。

## 手順
1. 全体を 1 行で書く：全体の Input → 最終的な Output。
2. その間を境界の連なりに割る。各境界を「新規 / 既存で Input・Output が変わる / 既存で変わらない」に分ける（変わらないものは名前だけ書く）。
3. 新規と、Input・Output が変わる既存の境界ごとにテンプレートを埋める。
4. 隣り合う境界の Output と Input が一致しているかを確認する。ずれていたら 2 に戻る。
5. 最後に Node Graph を Mermaid で書く。境界をノード、データの受け渡しを矢印にし、矢印にはデータの名前を付ける。
<<<<<<< HEAD
6. 設計を Issue か PR の本文に書く。どちらも無ければ harness.json の spec store に置く（Issues なら label を付けて Issue を作る。タイトルは `spec: <内容>`。files なら dir にファイルを作る）。未設定なら Issue か PR の本文だけでよい。
=======
6. 設計を Issue か PR の本文に書く。どちらも無ければ label `doc:spec` を付けて Issue を作る（`gh issue create --label doc:spec`、タイトルは `spec: <内容>`）。
   - 既存の `doc:spec` Issue を置き換えるときは、古い Issue を close せず open のまま、本文の状態行を「置き換え（→ #<新しい Issue>）」に直す。新しい Issue の状態行にも「（#<古い Issue> を置き換え）」と書く。
>>>>>>> origin/chore/spec-issues

## テンプレート
    ## 流れ
    注文の下書き → validate → 検証済みの注文 → save → 確定した注文

    ## 境界
    ### validate（新規）
    - 概要：
    - Scope：
    - Input：
    - Output：

    ## Node Graph
    ```mermaid
    flowchart LR
      draft([注文の下書き]) --> validate
      validate -- 検証済みの注文 --> save
      save -- 確定した注文 --> done([完了])
    ```

## やらないこと
- Task への分割（`/changegraph` の仕事）。
- 既存の境界の内部を設計し直す。
