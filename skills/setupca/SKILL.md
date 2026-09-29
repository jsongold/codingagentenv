---
name: setupca
description: project ごとのルールを対話で聞き、skill が読むスキーマどおりに harness.json へ書く。今のルールは tickets（チケット管理の場所と spec の見分け方）だけ。「harness の設定」「チケット管理の場所を変えたい」「spec の見分け方を変えたい」と言われたら使う。
argument-hint: "[global | project]"
---

# setupca — project のルールを対話で聞き、harness.json に書く

harness.json は project ごとのルールを書くファイル（project の `.claude/harness.json` > global の既定値 `global/harness.json`）。今のルールは `tickets` だけ。読む側は検証しないので、正しい形はここで書くことで担保する。

チケット管理の場所と spec の見分け方を対話で聞き、答えを下のスキーマに入れて書く。ほかのキーは消さない。global の正本は repo の `global/harness.json` で、書くのはこちら（`~/.claude/harness.json` は symlink なので直接書かない）。書いたら `bin/codingenv install` をユーザーに依頼する。

| キー | 意味 | 読む側 |
|---|---|---|
| `tickets.system` | `"github"`、ほかのチケット管理名（例 `"jira"`）、無しなら `null` | pickup・design・handoff・wake.md |
| `tickets.spec.label` | spec を見分ける label（github では spec 一覧に必須） | pickup・design・handoff・wake.md（github） |
| `tickets.project` | github 以外のときの project キー | pickup・design・handoff・wake.md（github 以外） |

```json
{"tickets":{"system":"github","spec":{"label":"doc:spec"}}}
{"tickets":{"system":"jira","project":"BATCH","spec":{"label":"spec"}}}
```

`~/.claude/` には直接書かない。
