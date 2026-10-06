export type Where = { worktree: string; branch: string }

const MAX = 12

// 12 chars or fewer as is; longer: first 11 chars + an ellipsis.
export const clip = (s: string): string => (s.length <= MAX ? s : `${s.slice(0, MAX - 1)}…`)

const basename = (p: string): string => p.replace(/\/+$/, '').split('/').pop() ?? ''

// git stdout -> where to show. Empty toplevel (not in a repo) -> null; detached HEAD shows the short sha.
export function parseWhere(g: { toplevel: string; branch: string; sha: string }): Where | null {
  const top = g.toplevel.trim()
  if (top === '') return null
  return { worktree: clip(basename(top)), branch: clip(g.branch.trim() || g.sha.trim()) }
}
