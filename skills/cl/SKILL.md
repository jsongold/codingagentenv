---
name: cl
description: Create, show, and update a checklist (CL) driven by AskUserQuestion answers. The optional argument is the CL name (e.g. `cl le8165-prod`; if omitted, a name is derived from the ticket id and purpose). `cl --list` lists every CL in the repo, `cl --show [name]` prints one. Use when the user says "CL", "show the CL", or "update the CL".
---

# cl — create, show, and update a checklist

A CL is a session-scoped working note stored as JSON at `<scratchpad>/cl/<name>.json` (scratchpad = the Scratchpad directory in the system prompt). Anything that must survive /clear belongs in a handoff or an Issue.

Old Markdown CLs (`cl/*.md`) are not read. They were session-only notes, so losing them is accepted.

Without a name (`cl`): if your own scratchpad has exactly 1 CL, use it; if several, let the user choose (run `list`); if 0, create one (the name is assigned after the purpose is confirmed).

Every read and write goes through the script. Never edit the JSON directly.

```
node ${CLAUDE_SKILL_DIR}/cl.ts <scratchpad> <cmd> ...
  new <name> --purpose <s> [--deadline <s>]
  add <name> <P0|P1|P2> <title>
  spec <name> <#> <todo|decided>
  impl <name> <#> <todo|done|n/a>      # n/a: nothing to implement (agreement, permission, etc.)
  link <name> <#> <label> <url>
  decide <name> <text>                 # appends with today's date
  store <name> <url>                   # GitHub issue or Jira issue URL (also: new ... --store <url>)
  sync <name>                          # push to the store (see Sync)
  synced <name> <n> [commentId]        # mark the first n log entries as pushed (Jira)
  checkpoint <name>                    # log "not synced"; pushes nothing
  show <name>                          # own scratchpad first, then the whole repo
  list                                 # all CLs in the repo (Pending column when a store is set)
```

Each write command prints the rendered CL and appends one log entry (before -> after); show that output as-is.

## Options
- `cl --list`: run `list` and print its output as-is. Columns: Name / Purpose / Open / Updated (newest first) / This session.
- `cl --show [name]`: run `show <name>` and print its output as-is. CLs from other sessions are read-only; mutating commands only touch your own scratchpad. If the name is omitted: use the only CL in your scratchpad, otherwise run `list` and ask which one.

## Create (no CL yet)
1. Confirm the purpose and deadline with the user.
2. If no name was given, derive one from the ticket/PR id and the purpose (short, lowercase, hyphenated, about 20 chars, e.g. `le8165-prod`) after the purpose is confirmed. Tell the user the name.
3. Read the materials (PRs, Issues, tickets, code) and find what is missing.
4. Assign each item a priority:
   - P0: blocker for the deadline
   - P1: works but may break prod or give wrong results; worked around by procedure this time
   - P2: fix before steady-state operation
5. `new`, then `add` each item. Mark unverified facts as "unverified" in the title; never assert guesses.

## Update
1. Ask the open points with AskUserQuestion: at most 4 per call, recommended option first with "(Recommended)".
2. Record each answer with `spec` / `impl` / `link`, and add a dated entry with `decide`.
3. Record free-form instructions (e.g. "drop e2e", "TZ=Tokyo") the same way.
4. If an answer changes assumptions or creates new risks, `add` them and report what changed in a line or two.
5. After recording, keep asking about the remaining open items (spec not decided, or impl todo) until none are left.

Item ids (`#`) are never renumbered; new items take the next number.

## Sync (only when the CL has a store)
1. After a round of writes, ask with AskUserQuestion: "Sync to store?" with options "Yes (Recommended)" / "No".
2. No: run `checkpoint`. The log entries stay pending for the next sync.
3. Yes, GitHub store: run `sync`. It edits one managed comment holding the CL and posts each pending log entry as its own comment.
4. Yes, Jira store: run `sync`; it prints JSON `{kind,url,commentId,body,pending,next}` and changes nothing. Post `body` (create the comment, or edit `commentId` if set) and each entry in `pending` with the Atlassian MCP, then run `synced <name> <next> [commentId]`. If posting fails, do not run `synced`.
