# ADR-0014: 起きている間は手元の Orchestrator が orchd を呼び、sleep 中は cad-2 の timer が `orchd tick` を回す。sleep 中に LLM の Orchestrator は置かない

- 日付：2026-09-28
- 状態：採用

## 文脈
Mission は 24/7 の稼働（[mission.md](../mission.md)）。今は手元の Claude Code（local）が Orchestrator で、`orchd pick → place → dispatch` を毎回呼ぶ（[orchestrate](../../skills/orchestrate/SKILL.md)、ADR-0011 追記）。owner と Mac が寝ると Issue が流れない。

ADR-0010 は Mac 睡眠時の GitHub Actions 経路を撤去し「Orchestrator は常駐し停止時は owner が再開する」とした。その後の案は「MODE AUTO = Claude Code cloud の Orchestrator（CCO）」で、cloud 上の LLM Orchestrator が寝ている間も回す想定だった。しかし sleep 中に要る判断は class の分類だけで、それは寝る前に手元で済ませられる（ADR-0010：判断は分類の 1 点）。残りは rule・usage・Issue の状態から決定的に決まる。

## 決定
**起きている間 = 手元の Orchestrator（Claude Code local）が orchd を直接呼ぶ（今の URGENT の形）。sleep 中 = cad-2（e2-micro）の timer が `orchd tick` を回し、LLM の Orchestrator は使わない。**
- `orchd tick` は `ai` Issue を拾い、配置する
  - CC の usage に余裕があれば優先して Claude Code cloud worker へ（VM から `claude -p "<task>" --cloud <worker session>`）
  - 無ければ停止して待機している worker VM（1〜3 台、ADR-0013）を orchd が必要なときに start して opencode
- 完了 = `Closes #n` の PR。タイムアウトで `ai-failed`
- 分類：寝る前に手元の Orchestrator が `class:` ラベルを付ける。無ければ既定の class
- 外部公開なし（inbound は IAP SSH のみのまま）。VM の操作は gcloud ではなく GCE REST API（metadata server の token）、GitHub も REST（PAT）

**置き換えるもの**：「MODE AUTO = Claude Code cloud の Orchestrator（CCO）」。あわせて ADR-0010 の「Orchestrator は常駐し停止時は owner が再開する」（Mac 睡眠時の撤去理由）と、ADR-0011 の未決「Mac 睡眠時の経路を Hatchet の cloud worker に置き換えるか」は、sleep 中の経路についてこの ADR で読み替える（それらの ADR ファイルは編集しない）。

## 検討して却下した案
共通の却下理由：sleep 中は判断が分類だけで、LLM の Orchestrator を cloud に置く必要がない。公開の口と秘密が増える。

| 案 | 却下理由 |
|---|---|
| CCO を常駐させる | 共通の理由 |
| Routine で CCO を起動する（A'） | 共通の理由。起動のたびに LLM を通す必要がない |
| `orchd serve` / HTTPS で公開（B1） | 共通の理由。inbound の口ができる |
| Tailscale Funnel / Cloudflare Tunnel | 共通の理由 |
| remote MCP | 共通の理由 |
| Pub/Sub | 共通の理由。timer で足りる |

## 影響
- 良い影響：sleep 中も Issue が流れる。外部公開も LLM の Orchestrator も無く、cad-2 と停止した worker VM だけで済む。判断は起きている間の分類に集まる
- 受け入れたトレードオフ：sleep 中は分類の付け替えや失敗時の判断ができない（タイムアウトで `ai-failed` にし、起きてから owner / Orchestrator が見る）。`class:` ラベルの無い Issue は既定の class で動く。cad-2 に PAT と GCE を操作する権限を置く
- 未検証の前提：非 TTY の `claude -p --cloud` が cron（timer）から通り、回収済みのセッションでも実行されるか（spike 中）
- 再検討する条件：上の前提が成り立たなければ、CC worker を worker VM 上の docker（OAuth）で動かす
