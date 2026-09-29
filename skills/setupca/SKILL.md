---
name: setupca
description: project ごとのルールを対話で決めて harness.json を作る・更新する。今のルールは tickets（チケット管理の場所と spec の見分け方）だけ。「harness の設定」「チケット管理の場所を変えたい」「spec の見分け方を変えたい」と言われたら使う。
argument-hint: "[global | project]"
---

# setupca — project のルールを対話で決めて harness.json を作る

harness.json は project ごとのルールを書くファイル（project の `.claude/harness.json` > global の既定値 `global/harness.json`）。今のルールは `tickets` だけ。

project のチケット管理の場所と spec の見分け方を聞き、環境に合わせて harness.json の `tickets` を書く。形は下の例を参考に、その時々で決めてよい。ほかのキーは消さない。global を書いたら `bin/codingenv install` をユーザーに依頼する。

```json
{"tickets":{"system":"github","spec":{"label":"doc:spec"}}}
{"tickets":{"system":"jira","project":"BATCH","spec":{"label":"spec"}}}
```

`~/.claude/` には直接書かない。
