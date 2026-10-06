---
name: graphlocal
description: Mermaid 図をブラウザで見られるようにする。ローカル server を起動して URL をユーザーに提示し、見終わったら止める。「Mermaid 図をブラウザで見たい」「設計の Node Graph を表示して」「図を開いて」と言われたら使う。
argument-hint: "[Mermaid テキスト | ファイルパス]"
---

# graphlocal — Mermaid 図をブラウザで見る

Mermaid テキストを 1 ページの HTML にして、127.0.0.1 のローカル server で配信する。Mermaid 本体は CDN（jsdelivr）から読むので、見るには通信が要る。1 図 = 1 ページ。

- Input：Mermaid 構文のテキスト（ファイルまたは stdin）
- Output：URL（`http://127.0.0.1:<port>/`）。ユーザーに提示する

## 手順
1. Mermaid テキストを scratchpad に `<name>.mmd` として書く（`<name>` は図の内容が分かる名前。URL とページタイトルと slug に使われる）。
2. 起動して URL を読む。

```
node "$(readlink -f "${CLAUDE_SKILL_DIR}")/../../tools/graphlocal/graphlocal.ts" serve <scratchpad>/<name>.mmd
```

   - 2 行出る。1 行目が URL、2 行目が `stop with: ... kill <slug>`。slug を控える。
   - server は切り離されたプロセス（別 pgid）で動き、`serve` は URL を出してすぐ戻る。`run_in_background` は要らない。
   - ファイルを渡さず stdin から読ませることもできる（このとき slug は `graph-<hash>`）。
3. URL をユーザーに提示する。URL 以外の中身（HTML、ログ）は貼らない。
4. ユーザーが見終わったら、必ず止める。

```
node "$(readlink -f "${CLAUDE_SKILL_DIR}")/../../tools/graphlocal/graphlocal.ts" kill <slug>
```

## 挙動（実行して確認済み）
- slug は `<ファイル名の拡張子なし>-<内容の sha1 先頭 8 桁>`。同じ内容で `serve` し直すと、前の server を止めて新しいポートで起動する（URL は変わる）。内容が違えば別 server として並ぶ。
- 状態は `<os.tmpdir()>/graphlocal/`（macOS では `/tmp` ではなく `$TMPDIR` 配下）。`<slug>.html` / `<slug>.lock`（pid・port・url・作成時刻）/ `<slug>.sock`。
- `url <slug>`：lock から URL を再表示する。server が死んでいたら exit 1。
- `kill <slug>`：socket 経由で止める。server がいなければ「no server」、lock だけ残っていれば掃除して exit 0。`<slug>` に `/` は使えない。
- GET `/` は HTML をディスクから読み直して返す（`<slug>.html` を手で直せば再読み込みで反映）。`/edit` へ POST した body は HTML を丸ごと置き換える。それ以外のパスは 404。
- 入力が空・ファイルが読めない・引数なしは `error: ...` を stderr に出して exit 1。Mermaid 構文の検証はしない（構文エラーはブラウザ上の描画で分かる）。
- server は止めない限り残る。Claude を終えても生きているので、提示した後に kill を忘れない。
- 他セッションの server は `<os.tmpdir()>/graphlocal/` に並ぶ。自分が起動した slug 以外は kill しない。

## やらないこと
- オフライン描画（Mermaid の同梱）。
- 複数の図を 1 ページにまとめること。
