# ADR 一覧

| 番号 | タイトル | 状態 | 日付 |
|---|---|---|---|
| [0003](./0003-global-harness.md) | ハーネスは global に置き、各 project はデータだけを持つ | 採用 | 2026-09-20 |
| [0005](./0005-repo-only-deploy-command.md) | 編集は repo 内だけ。global への展開は `bin/codingenv` に集約する | 採用 | 2026-09-20 |
| [0007](./0007-multi-session-handoff.md) | /handoff の保存先を `.claude/handoff/<name>.md`（1セッション1ファイル）にする | 採用 | 2026-09-21 |
| [0008](./0008-orchestrator-worker-cad.md) | Orchestrator（ローカル）と Worker（任意のクラウド）を分け、メタデータは Go 製の `cad` が決定的に返す | 採用 | 2026-09-27 |
| [0009](./0009-task-spec-orchestration.md) | Orchestrator と Worker の契約は task 1件ごとの YAML の task spec。provider は Orchestrator が strategy 付きで決め、`dev-dispatch` が決定的に適用する | 採用 | 2026-09-27 |
| [0010](./0010-placement.md) | 配置は (Agent, Computer) の組を選ぶ。判断は Orchestrator の class 分類だけ、選択は `orchd place`（旧 `cad` の `POST /v1/place`、ADR-0011）が決定的に返す | 提案 | 2026-09-27 |
| [0011](./0011-orchd-hatchet.md) | `cad` は事実（usage）だけにし、判断は `orchd` に分ける。task の状態・キュー・実行管理は Hatchet | 提案 | 2026-09-28 |
| [0012](./0012-singleton-handover.md) | 状態を持つ singleton の引き継ぎは性質で決める。cad は stop-first + usage 保存 + ready 待ち、二重に振り分けてはいけないものは durable execution に載せる | 採用 | 2026-09-28 |
| [0013](./0013-opencode-worker-vm.md) | opencode の cloud worker は停止した GCE VM 2 台（Spot / standard）を on demand で start し、終わったら VM が自分で止まる。選択は orchd の rule 順、起動できなければ exit 5 で置き直す | 採用 | 2026-09-28 |
| [0014](./0014-sleep-loop.md) | 起きている間は手元の Orchestrator が orchd を呼び、sleep 中は cad-2 の timer が `orchd tick` を回す（CC cloud worker 優先、無ければ停止 VM の opencode）。sleep 中に LLM の Orchestrator は置かない | 採用 | 2026-09-28 |
| [0015](./0015-sleep-advance.md) | sleep 中は CCO（CC cloud worker セッション）がプロジェクトの文脈から次にやることを判断して進め、merge まで行う。cad-2 の timer（orchd）は起こすだけ。ai 系ラベルは廃止（ADR-0014 の一部を置き換え） | 採用 | 2026-09-28 |
| [0016](./0016-remove-task-list.md) | Task は GitHub Issue で管理する。内蔵 Task list・`VERIFIED:`・TaskCompleted hook・トークン記録は持たない | 採用 | 2026-09-29 |

新しい ADR を追加するには、0000-template.md をコピーして次の番号のファイルを作り、上の表に1行追加する。

状態が「置き換え」の ADR と「却下した案」は再提案しない。
