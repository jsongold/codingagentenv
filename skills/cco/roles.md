# CCOinOrca の役割と流れ
目的：main・配送役・worker・Subagent の担当を 1 か所で読めるようにする（常駐の harness から移した）。

main（Claude Code のメインセッション）は、決める・渡す・完了を判定するだけ。作業の種類ごとの担当は次のとおり。

| 作業 | 担当 |
|---|---|
| 依頼の分類、設計の提案、Task への分割（`/taskman`） | main |
| Task の実装・テスト・コミット（1 Task = 1 コミット） | Orca の worker |
| worker の起動、待ち受け、質問への返答、片付け | Subagent（配送役） |
| PR 作成、CI 待ち、merge | Subagent（配送役） |
| 調査 | opencode（裏取りは Subagent） |
| チケットの登録と一括の修正 | Subagent |
| 完了の判定（done コマンドの再実行 1 回）と、ユーザーへの報告 | main |
| 1 ファイルの小さな修正、短い質問への回答 | main |

- 流れ：main が Task をチケットに登録する → 配送役が Orca の orchestration（skill `orchestration`。worker の起動は `orca orchestration worker-start`）で worker に振る → 待つ → worker の報告（`worker_done`）を受ける → PR・CI・merge → main が done コマンドを再実行して判定し、チケットを閉じる。手順は `/cco`
- 担当は作業の種類で決める。「数回のツール呼び出しで済む」ことは、main がやる理由にならない
- 複数ステップの依頼では、最初のツール呼び出しの前に、ステップごとの担当を書いた計画を出す
- Task はチケットで管理する。置き場所は harness.json の `tickets` に従う（github なら Issue、jira なら Jira）
- チケットを閉じる前に、main が worker や Subagent の報告を鵜呑みにせず、完了条件（done）のコマンドを自分で再実行して確認する
- Subagent と worker の報告は 10 行以内にさせる
