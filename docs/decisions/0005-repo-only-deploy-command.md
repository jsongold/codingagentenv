# ADR-0005: 編集は repo 内だけ。global への展開は `bin/codingenv` に集約する

- 日付：2026-09-20
- 状態：採用（ADR-0003 の install 手順と ADR-0004 の `hooks/install.sh` を置き換え。両 ADR の他の決定は有効）

## 文脈
ハーネスは global（`~/.claude/`）で効く（ADR-0003）が、展開の経路がばらばらだった。skill の symlink・env・`~/.claude/CLAUDE.md` のハーネス節は README の手作業、hook だけ `hooks/install.sh`。ハーネス節は正本が repo に無く、global にしか存在しなかった。

Claude が `~/.claude/settings.json` を書き換えようとすると、auto mode の guardrail（Self-Modification）に拒否される。これは意図された制限で、回避しない。

## 決定（ユーザー決定）
- Claude が編集するのは repo 内だけ。`~/.claude/` 配下は Claude も人も直接編集しない。
- global への展開は `bin/codingenv install` の1コマンドで、ユーザーが実行する。`uninstall` と `status`（drift 検出、読み取りのみ）も持つ。
- 正本はすべて repo に置く：`skills/`、`hooks/`、`global/CLAUDE.harness.md`（`~/.claude/CLAUDE.md` のハーネス節）。
- install は冪等。symlink、`settings.json` への追記（既存の hook・env・他のキーは変更しない）、CLAUDE.md のマーカー区間の置き換えを行い、毎回バックアップを取る。マーカー導入前に手で書いた節も置き換える。
- `bin/codingenv` 自身も install が `~/.local/bin/codingenv` へ symlink し、どこからでも呼べるようにする（2026-09-20 追記、ユーザー依頼）。同名の通常ファイルは上書きせず失敗する。`~/.claude/` の外なので本 ADR の編集禁止の対象ではない
- テストは `bash test/harness.test.sh`。`CLAUDE_DIR` で一時ディレクトリに向けて実行し、実際の `~/.claude/` には触らない。

## 検討して却下した案
| 案 | 却下理由 |
|---|---|
| Claude が `~/.claude/` を直接書き換える | guardrail に拒否される。履歴も残らず、repo と乖離する |
| README の手作業手順のまま | 手順が増えるたびに漏れる。drift を検出できない |
| `~/.claude/CLAUDE.md` 全体を repo で管理する | 個人の応答スタイルは repo に入れない決定（ADR-0003）と衝突する。管理するのはハーネス節だけ |
| plugin としてパッケージ化する | 他人と共有する必要が出るまで不要（ADR-0003 の再検討条件） |

## 影響
- 良い影響：直す場所が repo に一本化される。`status` で展開漏れが分かる。新しいマシンは clone して1コマンド
- 受け入れたトレードオフ：repo を直しただけでは CLAUDE.md の節と hook の登録は反映されない（symlink の中身は即反映）。install の実行を忘れると drift する
- 再検討する条件：install の実行忘れが繰り返されたら、SessionStart hook で `status` を走らせて警告する
