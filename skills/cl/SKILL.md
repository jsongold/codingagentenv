---
name: cl
description: Create, show, and update a checklist (CL) driven by AskUserQuestion answers. The optional argument is the CL name (e.g. `cl le8165-prod`; if omitted, a name is derived from the ticket id and purpose). `cl --list` lists every CL in the repo, `cl --show [name]` prints one. Use when the user says "CL", "show the CL", or "update the CL".
---

# cl — create, show, and update a checklist

A CL is a session-scoped working note stored as JSON at `<scratchpad>/cl/<name>.json` (scratchpad = the Scratchpad directory in the system prompt). Anything that must survive /clear belongs in a handoff or an Issue.

Every read and write goes through the script. Never edit the JSON directly.

```
node ${CLAUDE_SKILL_DIR}/cl.ts <scratchpad> <cmd> ...
  new <name> --purpose <s> [--deadline <s>]
  add <name> <P0|P1|P2> <title>
  spec <name> <#> <todo|decided>
  impl <name> <#> <todo|done|n/a>      # n/a: nothing to implement (agreement, permission, etc.)
  link <name> <#> <label> <url>
  decide <name> <text>                 # appends with today's date
  show <name>                          # own scratchpad first, then the whole repo
  list                                 # all CLs in the repo
```

Each write command prints the rendered CL; show that output as-is.

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
