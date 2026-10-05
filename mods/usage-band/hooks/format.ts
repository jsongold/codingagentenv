// rate-limit kind -> short label. Unknown kinds keep their raw name.
export function label(kind: string): string {
  if (kind === 'five_hour') return '5h'
  if (kind === 'seven_day') return 'w'
  if (kind.includes('fable')) return 'fw'
  if (kind.includes('opus')) return 'ow'
  if (kind.includes('sonnet')) return 'sw'
  return kind
}

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
