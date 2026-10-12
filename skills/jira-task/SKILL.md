---
name: jira-task
description: Jira に Epic / Story / Task を作るときの、各レイヤーの役割と登録手順。チケットを積む・分ける・Jira に登録するときに使う。
argument-hint: "[やりたいこと | Jira Key]"
---

# jira-task — Jira のチケットを 3 レイヤーで作る

責任は、依頼を Epic / Story / Task に当てて Jira に登録するところまで。

## レイヤーの役割
| レイヤー | 役割 | やること | 親 |
|---|---|---|---|
| Epic | 大きめの目標。時間軸は長め（1〜2 週間程度） | 複数でもよい | なし |
| Story | Epic 内での追加機能やタスクの塊 | 1 つ | Epic |
| Task | 最小単位。これ以上分けない | 1 つ | Story |

- Story は Task の塊。3 つ以上の Task をまとめられる抽象度・汎用さで書く。Task が 3 つ未満にしかならない Story は細かすぎるので、1 段上でまとめ直す
- Story と Task は「やること」が 2 つになったら分ける
- Epic に入らない大きさ（1〜2 週間を大きく超える）なら Epic を分ける

## 手順
1. harness.json（project の `.claude/harness.json` > `~/.claude/harness.json`）の `tickets` から project キーを読む。`tickets.system` が `jira` でなければ、止めてユーザーに確認する。
2. その project の種別を `getJiraProjectIssueTypesMetadata` で確認する。種別名と階層は project ごとに違うので、記憶で決めない。Story の子にできるのは Story より 1 つ下の階層の種別だけ（例：Story と「タスク」が同じ階層の project では、Task レイヤーは Subtask 種別で作る）。
3. 依頼を上の表でレイヤーに当てる。既存の Epic / Story に入るものは、新しく作らずそこに足す。
4. 作る一覧（レイヤー・タイトル・親）を見せ、OK が出てから登録する。
5. 上のレイヤーから順に登録し、親を付ける。
6. 親子の紐付けを JQL（`parent = <Key>`）で確認し、Key を報告する。

## 書き方
- タイトルは「やること」を 1 つだけ書く
- 本文は冒頭に `目的：<1〜2行>`、続けて完了条件を 1〜3 行。それ以上は書かない

## やらないこと
- 要件を絞る（`/mrs`）、設計（`/design`）、実装
