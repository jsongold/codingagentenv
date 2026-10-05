import { atom, read, update } from 'claude-code'
import type { Register, SessionContextUsage, SessionRateLimit } from 'claude-code'

import type { Stats } from '../types'
import { CTX_COLOR, color, label, ordered, parseOrg } from './format'

const stats = atom({ plugin: 'usage-band', key: 'stats' } as const, { limits: [] } as Stats)
const org = atom({ plugin: 'usage-band', key: 'org' } as const, null)
const model = atom({ plugin: 'usage-band', key: 'model' } as const, null)

const toStats = (context: SessionContextUsage, limits: SessionRateLimit[]): Stats => ({
  context: context.percent,
  limits: limits.map(l => ({ kind: l.kind, percentUsed: l.percentUsed })),
})

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    const started = await next(e)
    const usage = await $.session.usage()
    await update($, stats, () => toStats(usage.context, usage.rateLimits))
    const m = await $.session.model()
    await update($, model, () => m)
    const auth = await $.process.run(['claude', 'auth', 'status'])
    await update($, org, () => parseOrg(auth.stdout))
    return started
  })

  on('session.measure', async ($, e, next) => {
    await update($, stats, () => toStats(e.context, e.rateLimits))
    const m = await $.session.model()
    await update($, model, () => m)
    return next(e)
  })

  on('ui.render', { component: 'AbovePrompt' }, async ($, e, next) => {
    if (e.props.hasSurvey) return next(e)
    const s = await read($, stats)
    const o = await read($, org)
    const m = await read($, model)
    const { Box, Text } = $.ui.resolve(e)

    return (
      <Box>
        <Text color="magenta" bold>{m ?? '-'}</Text>
        <Text dimColor> | </Text>
        <Text dimColor>ctx </Text>
        <Text color={CTX_COLOR}>{s.context === undefined ? '-' : `${s.context}%`}</Text>
        <Text dimColor> |</Text>
        {ordered(s.limits).map((l, i) => (
          <Text key={l.kind}>
            <Text dimColor>{i === 0 ? ' ' : '  '}{label(l.kind)} </Text>
            <Text color={color(l.percentUsed)}>{`${l.percentUsed}%`}</Text>
          </Text>
        ))}
        <Text dimColor> | </Text>
        <Text color="cyan">{o ?? '-'}</Text>
      </Box>
    )
  })
}
