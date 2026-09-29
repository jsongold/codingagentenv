# Mission

**コーディングエージェントを 24 時間 365 日動かし続け、コストを最小に抑えながら、使える計算資源と AI の使用枠を使い切る。**

この repo は、その状態を作り・運用するための**ツールの詰め合わせ**である。1 つの大きなシステムではなく、役割ごとに小さく独立したツールを持ち、不要になったものは差し替える。

## 今の姿
- ローカルのターミナルで動く Claude Code が、実質的な Orchestrator になっている。task を分解し、Subagent や別の Agent（opencode など）に振り分けている。
- 計算資源と使用枠の管理は、人間（owner）が手で行っている。
- そのため、owner と Mac が止まると、開発も止まる。

## 目指す姿
- **管理を常駐させ、クラウドへ上げる**: task の状態と「次の task をどこで・誰に実行させるか」の判断を、ローカルの人手から切り離し、24/7 動く control plane に置く。
- **資源を判断の材料にする**: 各 Agent の使用枠（サブスクの窓・従量課金）と、各 Computer の空き・料金を決定的なデータとして集め、振り分けに使う。
- 複数の Orchestrator（ローカルとクラウド）の共存は、後で扱う。

## 最適化するもの
1. **コスト**: 定額の枠（サブスク）と無料の資源（ローカル Mac）を先に使い切り、従量課金の資源は必要なときだけ使う。
2. **稼働率**: 計算資源と使用枠を遊ばせない。空いている枠があるのに task が待っている状態をなくす。
3. **検証できること**: 振り分けは決定的なルールで行い、結果と理由を記録する。LLM の判断は task の分類に限る（#97）。

## 詰め合わせの中身
| ツール | 役割 |
|---|---|
| ハーネス（skills / hooks / `bin/codingenv`） | Claude Code を Orchestrator として動かす規則と、/clear をまたぐ文脈の保持 |
| cad | 事実を集めて配信する（Agent の使用量、Computer の料金など） |
| orchd | 次の task の実行先を決める |
| Hatchet | task の永続的な状態、キュー、worker の管理（#98） |
| worker | Agent の CLI を実行し、PR を出す |
| GitHub | task の入口（Issue）と、結果の検証（PR と CI） |

## 解くべき課題
- **実行プロセスの置き場所**: 各 Agent のプロセス（Claude / opencode / codex）を、ローカルとクラウドのどこで動かすか。
- **文脈の共有**: Orchestrator と worker の間で、task の文脈（handoff、spec、関連ドキュメント）をどう渡すか。
- **ネットワークとトラフィック**: control plane と worker の間でやり取りするコード・ドキュメントの経路と量、そのコスト。
- **サブスクをクラウドで使えるか**: 各サービスの規約と認証方式が、クラウドの worker での利用を許すか。
- **セキュリティ**: クラウドに置く認証情報、public repo、main の保護、prompt injection。

関連: [#95](https://github.com/jsongold/codingagentenv/issues/95) / [#97](https://github.com/jsongold/codingagentenv/issues/97) / [#98](https://github.com/jsongold/codingagentenv/issues/98)
