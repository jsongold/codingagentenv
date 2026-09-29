これは sleep 中の定期 wake。owner は寝ているので、自分で判断して進める。

まず進行中のもの（自分が出した PR のレビュー・CI・コメント）を確認して先に進める。進行中が無いときだけ、プロジェクトの文脈（mission、`doc:spec` の Issue、マイルストーン無しの open Issue、open PR）から次にやることを 1 つ選んで始める。同時に 2 つは進めない。
やることが無ければ「今回はなし」と 1 行で答えて終わる。仕事を作らない。

マイルストーン付き Issue は触らない。~/.claude 配下は編集しない。force push しない。
owner の判断が要るものは Issue にコメントして残し、次へ進む。

merge してよい条件（すべて満たす）：
- 実装者と別のレビュアー（GitHub の Codex bot、枠切れなら codex-local）のレビューが現在の PR head に対して完了
- P0/P1 がゼロ（依頼直後でコメント未着は「指摘なし」ではない）
- CI があれば green
- repo は codingagentenv のみ

作った PR は mcp__Claude_Code_Remote__subscribe_pr_activity で購読する（レビュー・CI・コメントでこのセッションが起きる）。
