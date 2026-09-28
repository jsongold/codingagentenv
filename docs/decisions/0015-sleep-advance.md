# ADR-0015: sleep 中は CC cloud worker セッション（CCO）がプロジェクトの文脈から次にやることを判断して進め、merge まで行う。cad-2 の timer（orchd）は起こすだけ。ai 系ラベルは廃止

- 日付：2026-09-28
- 状態：採用

## 文脈
ADR-0014 は「sleep 中に要る判断は class の分類だけで、寝る前に済ませられる」を前提に、cad-2 の timer が `orchd tick` で `ai` Issue を拾って配置し、完了 = `Closes #n` の PR とした。LLM の Orchestrator は sleep 中に置かない。

しかし PR ができた後にも判断が要る。レビュー指摘への対応、merge してよいかの判断、main との衝突の解消、そして次に何をやるか。これらを orchd の状態表（ラベルと PR の状態）で決める案も検討したが、判断を orchd 側に寄せるとラベル運用が要り、プロジェクトの文脈（mission・ADR・Issue の中身）を読めない（下の却下した案）。前提「sleep 中の判断は分類だけ」が崩れた。

## 決定
**sleep 中は CCO（CC cloud worker セッション）がプロジェクトの文脈から次にやることを判断して進め、merge まで行う。cad-2 の timer（orchd）は『何をやるか』を選ばず、起こすだけ。**
- `ai` / `wip` / `ai-failed` / `class:` ラベルは廃止する。何をやるかは CCO がプロジェクトの文脈（[mission.md](../mission.md)、ADR、マイルストーン無しの open Issue、open PR）から判断する
  - マイルストーン付きの Issue は対象外（owner 2026-09-28）
  - owner の判断が要るものは、CCO が Issue にコメントして残す
- orchd は次をすべて満たせば、固定の 1 通「プロジェクトの文脈で次を判断して進めよ」を `claude -p "<固定の指示>" --cloud <session>` で送る
  - sleep モード中
  - CC の usage に余裕がある
  - 前回の送信から一定間隔が空いた（前回分を処理し終えたかの判定方法は未検証。当面は間隔で代える）
- CC の枠が無いときは、worker VM（ADR-0013）の opencode に同じ指示を渡し、判断も opencode にさせる
- merge は CCO が行う。条件は次のすべて
  - 実装者と別のレビュアー（GitHub の Codex bot、枠切れなら codex-local）のレビューが、現在の PR head に対して完了している
  - その結果の P0/P1 がゼロ。レビューを依頼した直後でコメントが未着の状態は「指摘なし」と扱わない
  - CI があれば green
  - 自動 merge は当面 codingagentenv のみ

**置き換えるもの**：ADR-0014 のうち「sleep 中に LLM の Orchestrator は置かない」「判断は寝る前の分類だけ」「`class:` ラベル」「`ai` Issue を拾って配置する」「完了 = `Closes #n` の PR」。理由は前提「sleep 中の判断は分類だけ」が崩れたため。ADR-0014 の「外部公開なし」「GCE / GitHub は REST」「cad-2 の timer」は維持する（ADR-0014 のファイルは編集しない）。

## 検討して却下した案
| 案 | 却下理由 |
|---|---|
| CC cloud セッション内の `/loop` で CCO を常駐させる | 実測で発火しない。2026-09-28 12:29Z にセッションが「次回の確認は5分後です」と発言した後、16 分間活動がゼロ（#73） |
| orchd がラベルと PR の状態の表で次の一手を決める（この ADR の前版） | 判断を orchd 側に寄せるとラベル運用が要り、プロジェクトの文脈を読めない。owner が却下 |
| Routine（Cloud Scheduler → `POST /v1/claude_code/routines/{id}/fire`）を CCO にする | routine には daily run cap がある（Max 15 回/日、Pro 5、Team/Ent 25。超えた分は extra usage の従量課金）。5〜15 分間隔（96〜288 回/日）には足りず、既存の Email Triage routine が平日 10 回/日を使っている。API fire は 1 routine あたり 30 回/時、account あたり 100 回/時。schedule trigger の最短間隔は 1 時間。各回が新規セッションになる。根拠：[上限の数値](https://claude.com/blog/introducing-routines-in-claude-code)、[daily cap の存在・API trigger・最短 1 時間](https://code.claude.com/docs/en/routines)、[30 回/時・100 回/時](https://platform.claude.com/docs/en/api/claude-code/routines-fire)。`claude -p --cloud` で既存セッションへ送る方式は routine ではないため daily cap の対象外で、制約はサブスクの枠だけ（owner 2026-09-28 採用） |
| Claude Code Projects（公式 public beta）で CCO を置き換える | 外から起こす API・CLI が無く、opencode への切り替えもできない。自前を続ける（owner 2026-09-29。調査：research_notes/oss-24x7-agents.md） |

## 影響
- 良い影響：sleep 中も次にやることの選択から merge まで進む。ラベル運用が無くなり、判断は CCO に集まる。orchd は条件を見て固定の 1 通を送るだけになる
- 受け入れたトレードオフ：sleep 中に LLM の Orchestrator（CCO）を置く。自動 merge は当面 codingagentenv のみ（他 repo は owner 判断）
- 既存の PR：
  - PR #69（`ai` ラベル前提の orchd の issue list / dispatch）は止め、作り直すかを判断する
  - PR #70・#71・#72 は、この ADR が確定するまで保留する
- 未検証の前提：
  - 前回送った指示を処理し終えたかの判定方法（当面は間隔）
  - 長時間同じセッションを使うことによる文脈の劣化（compact）
- 再検討する条件：
  - daily run cap に縛られずに、既存の cloud セッションへ定期実行できる公式手段が確認できたら（routine は cap があるため該当しない）
  - cloud Orchestrator を夜に数回起こすだけで足りると分かったら、routine を保険として足す
