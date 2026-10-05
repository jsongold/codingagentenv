// rate-limit kind -> short label. Unknown kinds keep their raw name.
export function label(kind: string): string {
  if (kind === 'five_hour') return '5h'
  if (kind === 'seven_day') return 'w'
  if (kind.toLowerCase().includes('fable')) return 'F'
  if (kind.includes('opus')) return 'ow'
  if (kind.includes('sonnet')) return 'sw'
  return kind
}

// Usage items in display order: Fable (`F`) always last, others keep API order.
export function ordered<T extends { kind: string }>(limits: T[]): T[] {
  const isF = (l: T) => label(l.kind) === 'F'
  return [...limits.filter(l => !isF(l)), ...limits.filter(isF)]
}

// ctx is a fill level, not a quota: always yellow.
export const CTX_COLOR = 'yellow'

export function color(percent: number | undefined): string {
  if (percent === undefined) return 'gray'
  if (percent >= 80) return 'red'
  if (percent >= 50) return 'yellow'
  return 'green'
}

// `claude auth status` prints JSON with orgName.
export function parseOrg(stdout: string): string | null {
  try {
    const name = JSON.parse(stdout).orgName
    return typeof name === 'string' ? shortOrg(name) : null
  } catch {
    return null
  }
}

// "user@example.com's Organization" -> "example.com"; otherwise unchanged.
export function shortOrg(name: string): string {
  return /@(.+?)'s/.exec(name)?.[1] ?? name
}

// Model id or display name -> family + version digits ("claude-opus-5-5", "Opus 5.5" -> "opus55").
// Drops [1m]-style suffixes and date suffixes. Any alphabetic family works (no list).
// Unparsable or empty/null -> '?' so a broken parser is visible.
export function shortModel(raw: string | null | undefined): string {
  if (!raw || !raw.trim()) return '?'
  const s = raw.replace(/\[.*?\]/g, '').trim().toLowerCase()
  const m = /^(?:claude[\s-]+)?([a-z]+)[\s-]+(\d+)(?:[.\s-](\d{1,2})(?!\d))?/.exec(s)
  return m ? `${m[1]}${m[2]}${m[3] ?? ''}` : '?'
}

const EFFORT_SHORT: Record<string, string> = { low: 'low', medium: 'mid', high: 'high', xhigh: 'xl', max: 'max' }

// turn.step effort (level or number) -> display text (<= 4 chars); absent/empty -> null.
export function formatEffort(effort: string | number | undefined): string | null {
  if (effort === undefined || effort === '') return null
  const s = String(effort)
  return EFFORT_SHORT[s.toLowerCase()] ?? (/^\d+$/.test(s) ? s.slice(0, 4) : '?')
}
