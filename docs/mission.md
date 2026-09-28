# Mission

**コーディングエージェントを 24 時間 365 日動かし続け、コストを最小に抑えながら、使える計算資源と AI の使用枠を使い切る。**

## 今の姿
- ローカルのターミナルで動く Claude Code が、実質的な Orchestrator になっている。task を分解し、Subagent や別の Agent（opencode など）に振り分けている。
- 計算資源と使用枠の管理は、人間（owner）が手で行っている。
- そのため、owner と Mac が止まると、開発も止まる。

## 目指す姿
- **Control plane を常駐させる**: task の状態を管理し、次の task をどこで・誰に実行させるかを決めるサービス（orchd）が、常に動いている。
- **管理をクラウドへ上げる**: 24/7 で動かすため、task の状態と振り分けの判断をローカルの人手から切り離し、クラウドに置く。
- **複数の Orchestrator が共存できる**: ローカルとクラウドに Orchestrator が複数いても、同じ task を二重に取らず、同じ使用枠や計算資源を二重に数えない。
- **資源を判断の材料にする**: 各 Agent の使用枠（サブスクの窓・従量課金）と、各 Computer の空き・料金を、決定的なデータとして集め（cad）、振り分けに使う。

## 最適化するもの
1. **コスト**: 定額の枠（サブスク）と無料の資源（ローカル Mac）を先に使い切り、従量課金の資源は必要なときだけ使う。
2. **稼働率**: 計算資源と使用枠を遊ばせない。空いている枠があるのに task が待っている状態をなくす。
3. **検証できること**: 振り分けは決定的なルールで行い、結果と理由を記録する。LLM の判断は task の分類に限る（ADR-0010）。

## 役割の分担
| 構成要素 | 役割 |
|---|---|
| cad | 事実を集めて配信する（Agent の使用量、Computer の料金など） |
| orchd | task の状態を持ち、次の実行先を決める control plane（常駐） |
| Hatchet | task の永続的な状態、キュー、worker の管理（ADR-0011） |
| worker | Agent の CLI を実行し、PR を出す |
| GitHub | task の入口（Issue）と、結果の検証（PR と CI） |

関連: [ADR-0008](decisions/0008-orchestrator-worker-cad.md) / [ADR-0010](decisions/0010-placement.md) / [ADR-0011](decisions/0011-orchd-hatchet.md)
