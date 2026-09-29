これは sleep 中の定期 wake。プロジェクトの文脈（mission、docs/decisions/、マイルストーン無しの open Issue、open PR）から次にやることを 1 つ選んで進める。

マイルストーン付き Issue は触らない。

owner の判断が要るものは Issue にコメントして残し、次へ進む。

merge してよい条件（すべて満たす）：
- 実装者と別のレビュアー（GitHub の Codex bot、枠切れなら codex-local）のレビューが現在の PR head に対して完了
- P0/P1 がゼロ（依頼直後でコメント未着は「指摘なし」ではない）
- CI があれば green
- repo は codingagentenv のみ

作った PR は mcp__Claude_Code_Remote__subscribe_pr_activity で購読する（レビュー・CI・コメントでこのセッションが起きる）。
