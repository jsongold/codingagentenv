# tokens

目的：Task 1 件を振ったときに main・Subagent・worker がそれぞれ使った token 量を、Claude Code の transcript から数字で出す。

```sh
tools/tokens <session-id | dir>... [--json] [--root <dir>]
```

- 実行単位（session / subagent）ごとに 1 行と、合計の行を出す。会話の本文は出さない。`--json` で JSON。
- session id は `--root`（既定 `~/.claude/projects`）の下の全 project から探す。ディレクトリを渡すと、その中の全 session を数える。
- テスト：`bash test/tokens.test.sh`
