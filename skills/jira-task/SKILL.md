---
name: jira-task
description: Jira に Epic / Story / Task を作るときの、各レイヤーの役割と登録手順。チケットを積む・分ける・Jira に登録するときは必ず使う。
argument-hint: "[やりたいこと | Jira Key]"
---

# jira-task — Jira のチケットを 3 レイヤーで作る

責任は、依頼を Epic / Story / Task に当てて Jira に登録するところまで。

レイヤーの表し方と起票の形は、ここに書いたものを使う。handoff など、ほかの文書にある起票ルールと食い違うときは、こちらを優先する。

## レイヤーの役割
| レイヤー | 役割 | やること | 親 | Jira の階層 |
|---|---|---|---|---|
| Epic | 大きめの目標。時間軸は長め（1〜2 週間程度） | 複数でもよい | なし | 一番上（hierarchyLevel 1） |
| Story | Epic 内での追加機能やタスクの塊 | 1 つ | Epic | 標準（hierarchyLevel 0） |
| Task | 最小単位。これ以上分けない | 1 つ | Story | サブタスク（hierarchyLevel -1） |

- Story は Task の塊。3 つ以上の Task をまとめられる抽象度・汎用さで書く。Task が 3 つ未満にしかならない Story は細かすぎるので、1 段上でまとめ直す
- Story と Task は「やること」が 2 つになったら分ける
- Epic に入らない大きさ（1〜2 週間を大きく超える）なら Epic を分ける

## 守ること
- レイヤーは種別と親子で表す。タイトルに `[epic]` `[story]` などの接頭辞を付けない
- 子のない Epic、子のない Story を作らない。Epic を作るときは Story まで、Story を作るときは Task まで、同じ登録で作る。「Task は着手時に分解する」と先送りしない
- Epic の直下に Task を置かない。必ず Story を挟む
- 種別名が Epic / Story でない project でも、上の表の階層が合う種別を使う（例：種別が「プロセス / タスク / サブタスク」の project では、Epic = プロセス、Story = タスク、Task = サブタスク）

## 手順
1. harness.json（project の `.claude/harness.json` > `~/.claude/harness.json`）の `tickets` から project キーを読む。`tickets.system` が `jira` でなければ、止めてユーザーに確認する。
2. その project の種別と階層を `getJiraProjectIssueTypesMetadata` で確認し、上の表の階層に当てる。種別名と階層は project ごとに違うので、記憶で決めない。
3. 依頼を上の表でレイヤーに当てる。既存の Epic / Story に入るものは、新しく作らずそこに足す。
4. すでにあるチケットを入れるときは、どのレイヤーに当たるかを決める。Epic の直下にある Task の粒度のチケットは、Story の下に移す。種別や親を変えられないときは、作り直してよいかをユーザーに確認する。
5. 作る一覧（レイヤー・種別・タイトル・親）を、Epic から Task まで見せ、OK が出てから登録する。
6. 上のレイヤーから順に登録し、親を付ける。
7. 親子の紐付けを JQL（`parent = <Key>`）で確認し、Key を報告する。子のない Epic / Story が残っていないことも確認する。

## 書き方
- タイトルは「やること」を 1 つだけ書く
- 本文は冒頭に `目的：<1〜2行>`、続けて完了条件を 1〜3 行。それ以上は書かない

## やらないこと
- 要件を絞る（`/mrs`）、設計（`/design`）、実装
