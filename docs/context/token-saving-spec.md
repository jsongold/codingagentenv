# トークン削減実装のための仕様確定（原文引用）

確認日: 2026-09-20
方法: `curl -sL https://code.claude.com/docs/en/<page>.md` で markdown 原文を取得し grep で該当箇所を確認（WebFetch の要約は使わない。ADR-0004参照）。

---

## 1. `enabledPlugins` を project settings.json に書いて user settings の true を無効化できるか

**結論**: できる。project settings は user settings より優先順位が高いため、project の `.claude/settings.json` に `"<plugin>@<marketplace>": false` と書けば、user 側 `true` を project スコープで上書き・無効化できる。

**キー**: `enabledPlugins`（`plugin-name@marketplace-name` → boolean のマップ。scope: Any file）

**docs URL**:
- https://code.claude.com/docs/en/settings-reference.md#enabledplugins
- https://code.claude.com/docs/en/settings.md#settings-precedence

**原文引用（settings-reference.md, enabledPlugins）**:
> Project settings take precedence over user settings, so setting a plugin to `false` in `~/.claude/settings.json` doesn't disable a plugin that the project's `.claude/settings.json` enables. To opt out of a project-enabled plugin on your machine, set it to `false` in `.claude/settings.local.json` instead. Plugins force-enabled by managed settings can't be disabled this way, since managed settings override local settings.

**原文引用（settings.md, Settings precedence の順序、高い順）**:
> 1. **Managed settings**
> 2. **Command line arguments**
> 3. **Project local settings** (`.claude/settings.local.json`)
> 4. **Shared project settings** (`.claude/settings.json`)
> 5. **User settings** (`~/.claude/settings.json`)

（上記の enabledPlugins 個別記述は「project > user」の一般原則の具体例。project settings.json での `false` は user settings.json の `true` を上書きする。逆方向（user=false で project=true を消す）はできないと明記されている。）

---

## 2. `skillOverrides` の実在確認

**結論**: 実在する設定キー。settings.json（Any file スコープ）で個別 skill の可視性を制御できる。

**キー**: `skillOverrides`（skill名 → 以下4値のマップ）
- `"on"`: Claude に表示・説明あり、`/name` 補完あり
- `"name-only"`: Claude に名前のみ表示（説明なし）、`/name` 補完あり
- `"user-invocable-only"`: Claude には非表示、`/name` は自分で入力可
- `"off"`: Claude にも `/` 補完にも非表示
- 未指定なら `"on"` 扱い

**書けるファイル**: Any file（`/skills` メニューは `.claude/settings.local.json` に書く）。plugin skill には効かない（`/plugin` で管理）。

**docs URL**:
- https://code.claude.com/docs/en/settings-reference.md#skilloverrides
- https://code.claude.com/docs/en/skills.md#override-skill-visibility-from-settings

**原文引用（settings-reference.md）**:
> Hide or collapse a skill without editing its `SKILL.md`. Claude Code applies the value under each skill's name to the skill list Claude sees and to your `/` autocomplete.
> * **Scope**: `Any file`. The `/skills` menu writes to `.claude/settings.local.json`.
> * **Type**: object mapping skill name to one of: `"on"` / `"name-only"` / `"user-invocable-only"` / `"off"`
> Overrides don't apply to plugin skills, which you manage through `/plugin`.

**補足（frontmatter との併用、`disable-model-invocation`）**: frontmatter でも同等のことができる。
> To keep a personal or project skill but stop Claude from invoking it on its own, set `disable-model-invocation: true` in its frontmatter, or `"user-invocable-only"` in `skillOverrides` when you don't want to edit the file.

**コンテキストコストへの影響（表、skills.md）**:
| Frontmatter | You can invoke | Claude can invoke | When loaded into context |
|---|---|---|---|
| (default) | Yes | Yes | Description always in context, full skill loads when invoked |
| `disable-model-invocation: true` | Yes | No | Description not in context, full skill loads when you invoke |
| `user-invocable: false` | No | Yes | Description always in context, full skill loads when invoked |

→ `disable-model-invocation: true`（または `skillOverrides` の `"off"`/`"user-invocable-only"`）は skill の description すら Claude のコンテキストに乗らないため、`"name-only"` より削減効果が大きい。`"name-only"` は名前だけ残るため説明文の分だけ削減。

---

## 3. subagent の model 決定優先順位

**結論**: 4段階の優先順位。上から順に最初に定義された値が使われる。

1. Agent tool 呼び出し時の per-invocation `model` パラメータ
2. subagent 定義（frontmatter）の `model`（`inherit` は main conversation の model を選択）
3. 環境変数 `CLAUDE_CODE_SUBAGENT_MODEL`（alias か model ID を設定した場合）
4. main conversation の model

**docs URL**: https://code.claude.com/docs/en/sub-agents.md#choose-a-model

**原文引用**:
> When Claude invokes a subagent, it can also pass a `model` parameter for that specific invocation. Claude Code resolves the subagent's model in this order:
> 1. The per-invocation `model` parameter
> 2. The subagent definition's `model` frontmatter, where `inherit` selects the main conversation's model
> 3. The `CLAUDE_CODE_SUBAGENT_MODEL` environment variable, when you set it to a model alias or model ID
> 4. The main conversation's model
>
> Before v2.1.251, `CLAUDE_CODE_SUBAGENT_MODEL` came first in this order and overrode both the per-invocation parameter and the frontmatter, including `model: inherit`.

**補足**: `CLAUDE_CODE_SUBAGENT_MODEL_FORCE=1` を追加設定すると、上記より強制的に `CLAUDE_CODE_SUBAGENT_MODEL` を全 subagent / teammate / workflow agent に適用できる（`model` frontmatter や per-invocation model を無視）。組み込み Explore/Plan は `CLAUDE_CODE_SUBAGENT_MODEL` 単独では変わらない。

---

## 4. TaskCompleted / SubagentStop の入力 JSON フィールド

### SubagentStop

**結論**: `transcript_path`（親セッションのtranscript）と別に、subagent 自身の transcript を指す `agent_transcript_path` フィールドが存在する。task id/subject 相当のフィールドはない（agent_id/agent_typeのみ）。

**フィールド**: common input fields に加えて `stop_hook_active`, `agent_id`, `agent_type`, `agent_transcript_path`, `last_assistant_message`。さらに `background_tasks`, `session_crons`（親セッション基準）。

**docs URL**: https://code.claude.com/docs/en/hooks.md#subagentstop

**原文引用**:
> In addition to the common input fields, SubagentStop hooks receive `stop_hook_active`, `agent_id`, `agent_type`, `agent_transcript_path`, and `last_assistant_message`. The `agent_type` field is the value used for matcher filtering. The `transcript_path` is the main session's transcript, while `agent_transcript_path` is the subagent's own transcript stored in a nested `subagents/` folder. The `last_assistant_message` field contains the text content of the subagent's final response, so hooks can access it without parsing the transcript file.

サンプルJSON中の該当キー: `"agent_id": "def456"`, `"agent_type": "Explore"`, `"agent_transcript_path": "~/.claude/projects/.../abc123/subagents/agent-def456.jsonl"`。

### TaskCompleted

**結論**: task を識別するフィールドは `task_id` と `task_subject`（+ 任意で `task_description`, `teammate_name`, `team_name`）。subagent transcript を指すフィールドは無し。

**フィールド**: common input fields に加えて `task_id`, `task_subject`, 任意で `task_description`, `teammate_name`, `team_name`（`team_name` は deprecated）。

**docs URL**: https://code.claude.com/docs/en/hooks.md#taskcompleted

**原文引用**:
> In addition to the common input fields, TaskCompleted hooks receive `task_id`, `task_subject`, and optionally `task_description`, `teammate_name`, and `team_name`.

| Field | Description |
|---|---|
| `task_id` | Identifier of the task being completed |
| `task_subject` | Title of the task |
| `task_description` | Detailed description of the task. May be absent |
| `teammate_name` | Name of the teammate completing the task. May be absent |
| `team_name` | Deprecated. Session-derived team name; will be removed in a future release |

---

## 5. skill frontmatter での subagent 実行（`context: fork`）と `model` 指定

**結論**: どちらも可能。`context: fork` で forked subagent context 実行、`agent` で subagent type 指定、`model` で（`context: fork` 時は）forked subagent の model を指定できる。

**キー（frontmatter）**:
- `context`: `fork` を指定すると forked subagent context で実行
- `agent`: `context: fork` 時にどの subagent type を使うか
- `background`: `context: fork` 時のみ有効。`false` にすると呼び出したターン内で結果を待つ（デフォルト `true` = バックグラウンド実行）
- `model`: skill 実行中のモデル上書き。`context: fork` の場合は forked subagent の model を設定する値になる

**docs URL**: https://code.claude.com/docs/en/skills.md#run-skills-in-a-subagent

**原文引用（frontmatterテーブル）**:
> `context` | No | Set to `fork` to run in a forked subagent context. See Run skills in a subagent.
> `agent` | No | Which subagent type to use when `context: fork` is set.
> `background` | No | Only applies with `context: fork`. Set to `false` to wait for the forked subagent's result in the turn that invoked the skill, instead of running it in the background. Default: `true`.
> `model` | No | Model to use when this skill is active. ... With `context: fork`, the value sets the forked subagent's model instead, and an excluded value follows the same rules as a subagent model override.

**原文引用（本文）**:
> Add `context: fork` to your frontmatter when you want a skill to run in isolation. Claude Code starts a new subagent of the type set in the `agent` field and gives it the skill content as its prompt. The subagent doesn't see your conversation history, so the skill's instructions have to stand on their own.
>
> Despite the name, a skill with `context: fork` doesn't run in a fork of the current conversation, which would hand the subagent everything you've discussed so far. When the task depends on that history, fork the conversation instead of using `context: fork`.

---

## 未解決事項
- なし（5項目とも docs 原文で確認済み。「未記載」に該当する項目はなかった）。
