# statusbar

目的：プロンプトの上に、利用中のモデル（effort）・context %・使用枠（5h / w など）・org を色付きで常時表示する Claude Code の mod。

表示：`opus55 (high) | ctx 42% | 5h 23%  w 10% | cafkah.com | my-worktree | main`（org の後ろに作業 worktree 名と branch 名。各 12 文字超は 11 文字 + `…`、detached HEAD は短い sha、git 外では出さない。ctx は常に黄、使用枠は 50% 未満が緑・80% 未満が黄・それ以上が赤。使用枠が無いときは枠ごと省く）

使い方：`claude --plugin-dir mods/statusbar`
