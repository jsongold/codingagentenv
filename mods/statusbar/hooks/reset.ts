import { label } from './format'

// Time until a rate-limit window resets.
// five_hour -> "h:mm" ("2:15"); seven_day family (w / ow / sw / F) -> whole days ("3d", "0d" under a day).
// Missing/invalid resetsAt or any other kind -> null. A past reset counts as 0.
export function untilReset(kind: string, resetsAt: string | undefined, now: number): string | null {
  if (resetsAt === undefined) return null
  const at = Date.parse(resetsAt)
  if (Number.isNaN(at)) return null
  const ms = Math.max(0, at - now)
  const l = label(kind)
  if (l === '5h') {
    const min = Math.floor(ms / 60000)
    return `${Math.floor(min / 60)}:${String(min % 60).padStart(2, '0')}`
  }
  if (l === 'w' || l === 'ow' || l === 'sw' || l === 'F') return `${Math.floor(ms / 86400000)}d`
  return null
}
