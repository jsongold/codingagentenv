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

// turn.step effort (level or number) -> display text; absent/empty -> null.
export function formatEffort(effort: string | number | undefined): string | null {
  if (effort === undefined || effort === '') return null
  return String(effort)
}
