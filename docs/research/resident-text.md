目的：常駐テキスト（毎セッション読み込まれる CLAUDE.md など）を削る前後の量を比べる。

## やったこと

- CAO-143：測定 script（`tools/resident-size`）を作り、削る前の値を `docs/research/resident-text-before.tsv` に記録した。
- CAO-144：PR・並列開発の決まりと CCOinOrca の役割表を `skills/cco/pr-rules.md`・`roles.md` に移した（`global/CLAUDE.harness.md` が 7,094 → 3,751 bytes）。
- CAO-145（この文書）：同じ測り方で削った後を測り、`resident-text-after.tsv` に記録した。

## 前後の量

before は `resident-text-before.tsv`、after は `resident-text-after.tsv`。数え方は同じ（chars は Unicode の文字数、bytes は UTF-8、NA は取れなかったもの）。

| target | before chars | after chars | 差 | before bytes | after bytes | 差 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| global_claude_md | 4,512 | 2,463 | -2,049 | 9,254 | 5,067 | -4,187 |
| harness_source | 3,538 | 1,859 | -1,679 | 7,094 | 3,751 | -3,343 |
| project_claude_md | 2,154 | 2,154 | 0 | 3,828 | 3,828 | 0 |
| memory_index | NA | NA | NA | NA | NA | NA |
| skill_descriptions_user | 3,974 | 3,974 | 0 | NA | NA | NA |
| session_start_hook | NA | NA | NA | NA | NA | NA |

global_claude_md の before は CAO-143 の測定値。その後 ~/.claude/CLAUDE.md の harness 以外の部分が変わっており、install 直前の実物は 4,142 chars / 8,410 bytes / 83 lines だった（harness の節は 7,094 bytes のまま）。install 直前から after までの差は -1,679 chars / -3,343 bytes で、harness の節の差（-3,343 bytes）と一致する。

## token の A/B

| 条件 | session_first_call_tokens |
| --- | ---: |
| A：install 前 | NA |
| B：install 後 | NA |
| 参考：CAO-143 の測定（install 前の実物 session） | 35,170 |

NA の理由：A（install 前）も B（install 後）も NA。CLAUDE_CONFIG_DIR を上書きした claude -p は 'Not logged in · Please run /login'（exit 1）。HOME 上書きで実物の login を使う 2 回目の試行は権限が拒否したので実行していない。before.tsv の 35170 は install 前の実物 session の値で A/B の A ではない

## 限界

- after は install を一時ディレクトリ（`CLAUDE_DIR` / `BIN_DIR` の上書き）で再現した値。実環境の再測定は `bin/codingenv install` のあと（`bash tools/resident-size --tokens`）。
- memory_index と session_start_hook は before・after とも NA。memory_index：not found: ~/.claude/projects/<cwd をエンコードしたパス>/memory/MEMORY.md（測定用の新しい worktree には memory が無い。before も同じ理由で NA）。session_start_hook：command failed: ~/.claude/hooks/harness-session-start.sh: No such file or directory（symlink 先が無い）; harness-session-start only; 2 other SessionStart command(s) not run (side effects)。
- skill_descriptions_user は install では変わらない対象で、実環境の現在値をそのまま測っている。
- token は A/B とも取れていないので、削減が token でいくつになるかはこの文書では言えない。chars・bytes の差だけが確定値。
- n=1（各値は 1 回の測定）。
