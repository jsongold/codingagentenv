import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register, SessionContextUsage, SessionRateLimit } from 'claude-code'

import type { Stats } from '../types'
import { CTX_COLOR, color, formatEffort, label, ordered, parseOrg, shortModel } from './format'
import { parseWhere } from './where'
import type { Where } from './where'

const stats = atom({ plugin: 'statusbar', key: 'stats' } as const, { limits: [] } as Stats)
const org = atom({ plugin: 'statusbar', key: 'org' } as const, null)
const where = atom({ plugin: 'statusbar', key: 'where' } as const, null as Where | null)
const model = atom({ plugin: 'statusbar', key: 'model' } as const, null)
const effort = atom({ plugin: 'statusbar', key: 'effort' } as const, null)

const toStats = (context: SessionContextUsage, limits: SessionRateLimit[]): Stats => ({
  context: context.percent,
  limits: limits.map(l => ({ kind: l.kind, percentUsed: l.percentUsed })),
})

// `claude auth status` -> org atom. Never throws: a missing/hung `claude` keeps the current org.
async function loadOrg($: EngineInterface): Promise<void> {
  try {
    const auth = await $.process.run(['claude', 'auth', 'status'], { timeoutMs: 5000 })
    await update($, org, () => parseOrg(auth.stdout))
  } catch {
    // keep the org already shown
  }
}

// git in the session cwd -> where atom (null outside a repo). Never throws: a failure keeps the current value.
async function loadWhere($: EngineInterface): Promise<void> {
  try {
    const cwd = await $.session.cwd()
    const git = (args: string[]) => $.process.run(['git', ...args], { cwd, timeoutMs: 5000 })
    const [top, br, sha] = await Promise.all([
      git(['rev-parse', '--show-toplevel']),
      git(['branch', '--show-current']),
      git(['rev-parse', '--short', 'HEAD']),
    ])
    const w = top.exitCode !== 0
      ? null
      : parseWhere({
          toplevel: top.stdout,
          branch: br.exitCode === 0 ? br.stdout : '',
          sha: sha.exitCode === 0 ? sha.stdout : '',
        })
    // cwd moved while git ran: a newer lookup owns the atom, drop this stale result.
    if ((await $.session.cwd()) !== cwd) return
    await update($, where, () => w)
  } catch {
    // keep the where already shown
  }
}

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    const started = await next(e)
    const usage = await $.session.usage()
    await update($, stats, () => toStats(usage.context, usage.rateLimits))
    const m = await $.session.model()
    await update($, model, () => shortModel(m))
    // Not awaited: the first session.start blocks the first prompt.
    void loadOrg($)
    void loadWhere($)
    return started
  })

  // /clear starts a new session without session.start: fetch org again.
  on('classic.SessionStart', async ($, e, next) => {
    if (e.source === 'clear') {
      await loadOrg($)
      await loadWhere($)
    }
    return next(e)
  })

  // cd / worktree switch, and branch changes made during a turn.
  on('classic.CwdChanged', async ($, e, next) => {
    await loadWhere($)
    return next(e)
  })

  on('classic.Stop', async ($, e, next) => {
    await loadWhere($)
    return next(e)
  })

  // /model: show the new model right away.
  on('classic.PostModelSwitch', async ($, e, next) => {
    await update($, model, () => shortModel(e.to_model))
    return next(e)
  })

  on('session.measure', async ($, e, next) => {
    await update($, stats, () => toStats(e.context, e.rateLimits))
    return next(e)
  })

  // Model and effort as the request actually uses them (fallbacks included); main loop only.
  on('turn.step', async function* ($, e, next) {
    if (e.agentId === undefined) {
      const m = shortModel(e.model)
      if (m !== (await read($, model))) await update($, model, () => m)
      const f = formatEffort(e.effort)
      if (f !== (await read($, effort))) await update($, effort, () => f)
    }
    return yield* next(e)
  })

  on('ui.render', { component: 'AbovePrompt' }, async ($, e, next) => {
    if (e.props.hasSurvey) return next(e)
    const s = await read($, stats)
    const o = await read($, org)
    const w = await read($, where)
    const m = await read($, model)
    const f = await read($, effort)
    const limits = ordered(s.limits)
    const { Box, Text } = $.ui.resolve(e)

    return (
      <Box>
        <Text color="magenta" bold>{m ?? '-'}</Text>
        {f !== null && <Text dimColor>{` (${f})`}</Text>}
        <Text dimColor> | </Text>
        <Text dimColor>ctx </Text>
        <Text color={CTX_COLOR}>{s.context === undefined ? '-' : `${s.context}%`}</Text>
        {limits.length > 0 && <Text dimColor> |</Text>}
        {limits.map((l, i) => (
          <Text key={l.kind}>
            <Text dimColor>{i === 0 ? ' ' : '  '}{label(l.kind)} </Text>
            <Text color={color(l.percentUsed)}>{`${l.percentUsed}%`}</Text>
          </Text>
        ))}
        <Text dimColor> | </Text>
        <Text color="cyan">{o ?? '-'}</Text>
        {w !== null && <Text dimColor> | </Text>}
        {w !== null && <Text color="green">{w.worktree}</Text>}
        {w !== null && <Text dimColor> | </Text>}
        {w !== null && <Text color="green">{w.branch}</Text>}
      </Box>
    )
  })
}
