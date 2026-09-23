# ADR-0007: /handoff の保存先を PROGRESS.md 1本から .claude/handoff/<name>.md（1セッション1ファイル）に変える

- 日付：2026-09-21
- 状態：採用

## 文脈
並行セッションが同じ PROGRESS.md を上書きし合う。一方、Task list は project 単位（`CLAUDE_CODE_TASK_LIST_ID`）で全セッションが共有する。handoff だけが1本なので、あるセッションの /handoff が別セッションの文脈を消す。

hook が自動で「自分の」handoff を特定することもできない。`/clear` で `session_id` が変わるため、clear 前後のセッションを id で結び付けられない。

## 決定
- 保存先を `.claude/handoff/<name>.md` にする。1セッション1ファイル。`<name>` は kebab-case（`[a-z0-9-]`）。見出しは従来の PROGRESS.md と同じ8項目（最終更新／目的／完了条件／決定事項／却下した案／現在の状態／次の一手／注意・未解決の質問）、30行以内。
- `/handoff <name>` は名前つきで書く。省略時は、この会話で pickup / handoff 済みの名前（または hook が注入した handoff の続き）を再利用し、そうでなければセッションの目的から kebab-case で2〜4語の名前を自動生成する。既存と衝突したら `-2` を付け、他の handoff は上書きしない。「既存が1件なら更新」は並行セッションで別セッションのものを上書きしうるので採らない。`/pickup <name>` で読む。省略時は1件ならそれ、複数なら一覧を出して聞く。
- SessionStart hook は、有効な handoff（未記入テンプレートでないもの）が2件以上なら一覧（名前・最終更新・目的1行）だけを注入し、1件ならその全文（`head -n 60`）を注入する。0件で旧 PROGRESS.md があれば従来どおり注入する。他 project を壊さない後方互換で、ADR-0003 の「skill はファイルが無い状態でも動く」と整合する。
- どれが「自分の」handoff かは hook では決めない。一覧を出してユーザーが選ぶ。
- `/handoff` は自分の handoff ファイルだけをコミットする。他セッションの未コミット変更は巻き込まない。
- Task list は共有のまま。handoff の「現在の状態」に、そのセッションが担当する Task ID を書く。Task list 自体の分離は範囲外。
- 移行：この repo の PROGRESS.md は `.claude/handoff/default.md` に `git mv` する。他 project は旧 PROGRESS.md をフォールバックで読み、次の /handoff で移行する。

## 検討して却下した案
| 案 | 却下理由 |
|---|---|
| PROGRESS.md 内でセクションを分割する | 並行書き込みで同じファイルが衝突する。1ファイルが肥大する |
| git branch 名で自動選択する | 同一 branch の並行セッションを分けられない。branch を切り替えると迷子になる |
| 最新更新の1件を全文自動注入する | 別セッションのものを掴む |
| session_id をファイル名にする | `/clear` で変わるので、pickup で辿れない |

## 影響
- 良い影響：並行セッションが互いの handoff を上書きしない。コミットも自分のファイルだけになり、他セッションの変更を巻き込まない。旧 PROGRESS.md だけの project は従来どおり動く
- 受け入れたトレードオフ：pickup 時にユーザーが名前を選ぶ手間が増える。自動生成の名前の質はモデルの要約次第（変えたいときは `/handoff <name>` で明示する）。Task list は共有のままなので、handoff の Task ID に頼る運用になる
- 再検討する条件：Task の取り違えが実際に起きたら、Task list 自体の分離を検討する。Claude Code が `/clear` をまたいで安定したセッション識別子を提供するようになったら、名前の手動選択をやめる
