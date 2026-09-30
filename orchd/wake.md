これは sleep 中の定期 wake。owner は寝ているので、自分で判断して進める。

timer の役割は 2 つだけ。PR の作業はイベントで進む（下記）。
- blocked でない進行中の PR が無いときだけ、プロジェクトの文脈（mission、spec のチケット、マイルストーン無しの open なチケット（harness.json の tickets に従う。github のときは `gh issue list --state open --search "no:milestone -label:<tickets.spec.label>"`。spec は設計判断で、やる仕事ではない）、open PR）から新しい仕事を 1 つ選んで始める。同時に 2 つは進めない
- 最後の commit から一定時間イベントが無い自分の PR を拾い直し、最新 main の orchd/on-pr-event.md（`git fetch -q origin main && git show origin/main:orchd/on-pr-event.md`）に従って進める。未購読なら購読する

やることが無ければ「今回はなし」と 1 行で答えて終わる。仕事を作らない。

マイルストーン付き Issue は触らない。~/.claude 配下は編集しない。force push しない。
owner の判断が要るものは Issue にコメントして残し、次へ進む。

merge してよい条件（すべて満たす）：
- 実装者と別のレビュアー（GitHub の Codex bot、枠切れなら codex-local）のレビューが現在の PR head に対して完了
- P0/P1 がゼロ（依頼直後でコメント未着は「指摘なし」ではない）
- CI があれば green
- repo は codingagentenv のみ

PR を作ったら、mcp__Claude_Code_Remote__subscribe_pr_activity で購読し、すぐレビューを依頼する（レビュー・CI・コメントでこのセッションが起きる）。
PR のイベントが届いたら、最新 main の orchd/on-pr-event.md（`git fetch -q origin main && git show origin/main:orchd/on-pr-event.md`。手元の checkout は古いことがある）に従う。
