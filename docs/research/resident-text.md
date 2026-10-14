目的：常駐テキスト（毎セッション読み込まれる CLAUDE.md など）を削る前後の量を比べる。

## やったこと

- CAO-143：測定 script（`tools/resident-size`）を作り、削る前の値を `docs/research/resident-text-before.tsv` に記録した。
- CAO-144：PR・並列開発の決まりと CCOinOrca の役割表を `skills/cco/pr-rules.md`・`roles.md` に移した（`global/CLAUDE.harness.md` が 7,094 → 3,751 bytes）。
- CAO-145 前半：install を一時ディレクトリで再現した after を `resident-text-after.tsv` に記録した。
- CAO-145 後半（この更新）：ユーザーが `bin/codingenv install` を実行したあとの実環境を測り、`resident-text-after-install.tsv` に記録した（測定 2026-10-14 18:17 JST）。

## 前後の量

before は `resident-text-before.tsv`、after（再現）は `resident-text-after.tsv`、実環境 after は `resident-text-after-install.tsv`。数え方は同じ（chars は Unicode の文字数、bytes は UTF-8、NA は取れなかったもの）。差は 実環境 after − before。

| target | before chars | after（再現）chars | 実環境 after chars | 差 | before bytes | after（再現）bytes | 実環境 after bytes | 差 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| global_claude_md | 4,512 | 2,463 | 2,463 | -2,049 | 9,254 | 5,067 | 5,067 | -4,187 |
| harness_source | 3,538 | 1,859 | 1,859 | -1,679 | 7,094 | 3,751 | 3,751 | -3,343 |
| project_claude_md | 2,154 | 2,154 | 2,154 | 0 | 3,828 | 3,828 | 3,828 | 0 |
| memory_index | NA | NA | NA | NA | NA | NA | NA | NA |
| skill_descriptions_user | 3,974 | 3,974 | 6,489 | +2,515 | NA | NA | NA | NA |
| session_start_hook | NA | NA | 0 | NA | NA | NA | 0 | NA |

- global_claude_md：実環境 after は 2,463 chars / 5,067 bytes / 60 lines。再現値（2,463 / 5,067 / 60）と一致した（再現は正しかった）。before（CAO-143）の値は、その後 ~/.claude/CLAUDE.md の harness 以外の部分が変わっているため単純比較できない。install 直前の実物は 4,142 chars / 8,410 bytes / 83 lines だった。install 直前から実環境 after までの差は -1,679 chars / -3,343 bytes。
- skill_descriptions_user：17 skills → 33 skills に増えており、description の合計も増えた。install では変わらない対象なので、install 以外の変更が before と after の間に入っている。
- session_start_hook：before は symlink 先が無く NA。実環境 after は symlink 先が存在し、hook は実行できたが出力は 0 chars（この worktree に handoff が無く、hook は何も注入しない）。handoff がある cwd では出力が増えるので、この値は常駐量の下限。
- symlink：~/.claude/skills/cco -> /Users/yasumasa_takemura/projects/codingagentenv-global/skills/cco (target exists: yes)。~/.claude/hooks/harness-session-start.sh -> /Users/yasumasa_takemura/projects/codingagentenv-global/hooks/harness-session-start.sh (target exists: yes)。

## token の A/B

`session_first_call_tokens` は `claude -p ok` の最初の API 呼び出しの入力側（input + cache_creation + cache_read）。内訳は transcript の usage から取った。

| 条件 | 合計 | input | cache_creation | cache_read |
| --- | ---: | ---: | ---: | ---: |
| A：install 前（CAO-143 の測定。2026-10-14） | 35,170 | 2 | 24,191 | 10,977 |
| B 1 回目：install 後 | 35,868 | 2 | 22,305 | 13,561 |
| B 2 回目：install 後 | 35,868 | 2 | 0 | 35,866 |

| 差（B − A） | tokens | A に対する割合 |
| --- | ---: | ---: |
| B 1 回目 − A | +698 | +2.0% |
| B 2 回目 − A | +698 | +2.0% |

- B の 2 回は合計が同じ（35,868 と 35,868）。cache_creation と cache_read の割り振りは回ごとに違う（cache の状態による）が、合計は cache の状態に左右されない。
- 常駐テキストを bytes で -3,343 削ったのに、token の合計は 減っていない（+698）。before と after の間に skills が 17 → 33 に増えており（上の表）、その分が差に混ざっている。この測定だけでは「CLAUDE.md を削った分」と「skills が増えた分」を分けられない。

## 限界

- n=2（B は 2 回。A は 1 回で、CAO-143 の測定。2026-10-14）。A の再測定はできない（install 前の環境は戻せない）。
- A と B は測定した時点の plugin・skill・設定の構成が違う。skill_descriptions_user は 17 → 33 skills で、token の差にはこの違いが混ざる。常駐テキストの削減だけの効果は token では確定できない。
- A は cwd が `cao-143-resident-size` の worktree、B は `cao-145-after-install` の worktree。どちらも worktree のルートで測り、project_claude_md は同じ大きさ（3,828 bytes）。memory_index は両方 NA（測定用の新しい worktree には memory が無い）。
- 実環境の再測定は実施済み（このファイルの上の節）。global_claude_md の chars・bytes は実環境で確定した値。
- 確定値は chars・bytes の差と B の 2 回の token。「削減が token でいくつか」は上の混入があるため言えない。
