export type Limit = { kind: string; percentUsed: number; resetsAt?: string }
export type Stats = { context?: number; limits: Limit[] }

declare module 'claude-code' {
  interface PluginState {
    'statusbar': { stats: Stats; org: string | null; where: { worktree: string; branch: string } | null; model: string | null; effort: string | null }
  }
}
