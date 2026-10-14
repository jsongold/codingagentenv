# 設計案: cloud session をアカウントごとに持つ（Issue #65 案 A, owner 2026-09-29 採用）

現状（origin/main）: `NS.CloudWorkerSession` 1 つ（orchd/task.go:36）。place が agent を決めた**後**で `cloudSession` = `CLAUDE_CLOUD_SESSION` > 登録の値を runner.session に入れる（orchd/main.go:154-160, task.go:134-140）。
agent 選択（orchd/place.go:81-128）は session を見ないので、session の無い/別アカウントでも選ばれ、dispatch が失敗する。
回避として sleep rule を `claude/a12e00a7` に固定済み（orchd/policy.json:17, commit b540b42）。

## 1. namespaces.json の新しい形
```json
{ "default": { "repo": "jsongold/codingagentenv", "path": "~/projects/codingagentenv", "orchestratorUrl": "",
    "cloudWorkerSessions": { "claude/a12e00a7": "session_01Ab…", "claude/b1c8ef41": "session_01Cd…" },
    "caps": { "concurrent": 2 } } }
```
- key は policy.agents と同じ `<service>/<account>`。値の無いアカウントは key ごと書かない（空文字も「無し」扱い）
- policy.json の sleep rule は `claude/*` に戻す（データ変更。固定は不要になる）

## 2. 候補選択と session 解決（擬似コード）
session の有無は「その agent を使えるか」なので、agent 選択（place, 純関数）の中で判定する。main は登録を渡すだけ。
```
placeCmd:
  reg := readNS(ns)                               // 変更: place() の前に読む（今は placed 後 main.go:155）
  spec.Sessions = reg.CloudWorkerSessions         // PlaceSpec に 1 フィールド追加
  p := place(pol, usage, spec, slots)             // 以降の出力処理は今のまま（main.go:154-160 の後付けは削除）
place() の候補ループ（place.go:86 で rn を得た直後）:
  if cmd に "{session}" を含む:
     s := spec.Sessions[a]
     if s == "": drop("%s: no cloud session"); continue    // 候補から外す。次の agent / 次の rule へ
     rn.Session = s                                        // 形は作る側（place）が埋める
  ...（usage 窓の判定は今のまま）
```
- 全候補が外れれば今と同じ 422 → PlaceOutput は rule -1・runner {}・exit 0（main.go:174）。409 も今のまま
- `CLAUDE_CLOUD_SESSION`: **削除を推奨**。1 つの値はどのアカウントのものか分からず、今回の不具合を env 経由で再現させる。repo 内の使用は orchd のみ（テストの Setenv を除く）

## 3. 旧 cloudWorkerSession の扱い
推奨: **一括移行（互換なし）**。理由: 実ファイルは cad-2 の 1 つ（gitignore）と example だけで、読むのは orchd だけ。互換の分岐を残すと「どのアカウントの session か」が再び曖昧になる。
- 旧形のまま動かした場合: session が 1 つも無い → claude-cloud は全候補 `no cloud session` で外れ、sleep は opencode の rule か runner {} になる（安全側）
- 移行は `/igosleep`（PR #72）がアカウントごとに `.default.cloudWorkerSessions["claude/<id>"]` を書く形に直すことで行う。旧 key は jq で `del` する

## 4. 変更ファイルと見積もり差分（orchd PR。PR #72 は別）
| ファイル | 内容 | 行 |
|---|---|---|
| orchd/task.go | NS.CloudWorkerSessions map、cloudSession 削除 | ~10 |
| orchd/place.go | PlaceSpec.Sessions、候補ループの session 判定 | ~10 |
| orchd/main.go | readNS を place 前へ、後付け削除、usage 文言 | ~12 |
| orchd/policy.json | sleep rule `claude/a12e00a7` → `claude/*` | 1 |
| cad/config/namespaces.example.json | `cloudWorkerSessions: {}` | 1 |
| orchd/README.md:30 | session 解決の説明 | 2 |
| orchd/task_test.go, place_test.go | 下記 | ~60 |
本体 約 35 行 + テスト 約 60 行。PR #72 の SKILL.md（jq の書き込み先と案内文）は別 PR で ~10 行。

## 5. テスト観点
- 両アカウントに session: sleep で `claude/a12e00a7` が選ばれ runner.session がその値、configDir も同じ id
- a12e00a7 に session 無し・b1c8ef41 に有り: b1c8ef41 が選ばれ、reason に `claude/a12e00a7: no cloud session`
- a12e00a7 が 5h 窓超過・b1c8ef41 に session: b1c8ef41 に切り替わる（枠を使い切る目的の本丸）
- どのアカウントにも session 無し: claude-cloud rule は外れ opencode rule へ。opencode も無理なら runner {}・rule -1・exit 0
- 旧形 `cloudWorkerSession` だけの登録: 上と同じ（落ちない・exit 0）
- `{session}` を使わない runner（opencode, subagent）は登録の有無に影響されない
- auto/urgent の `self`@claude-cloud: self に session が無ければ外れて次の rule（local）へ
- 壊れた namespaces.json: exit 1（今の挙動の範囲を place 全体に広げる点を確認）
- place() 単体（純関数）で Sessions を渡すテーブルテスト。既存 TestPlaceSleepFills* は新形の登録に置き換え
