// worktree: null in the main worktree, the linked worktree's name otherwise.
export type Where = { worktree: string | null; branch: string }

const MAX = 12

// 12 chars or fewer as is; longer: first 11 chars + an ellipsis.
export const clip = (s: string): string => (s.length <= MAX ? s : `${s.slice(0, MAX - 1)}…`)

const basename = (p: string): string => p.replace(/\/+$/, '').split('/').pop() ?? ''

// git stdout -> where to show. Empty toplevel (not in a repo) -> null; detached HEAD shows the short sha.
// dirs = `rev-parse --path-format=absolute --git-dir --git-common-dir`: the two lines match only in the main worktree.
export function parseWhere(g: { toplevel: string; dirs: string; branch: string; sha: string }): Where | null {
  const top = g.toplevel.trim()
  if (top === '') return null
  const [gitDir, commonDir] = g.dirs.trim().split('\n')
  const linked = gitDir !== undefined && commonDir !== undefined && gitDir !== commonDir
  return { worktree: linked ? clip(basename(top)) : null, branch: clip(g.branch.trim() || g.sha.trim()) }
}
