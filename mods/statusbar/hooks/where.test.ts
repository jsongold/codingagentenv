import { expect, test } from 'claude-code/testing'
import { clip, parseWhere } from './where'

test('clip keeps 12 chars or fewer', () => {
  expect(clip('abc')).toBe('abc')
  expect(clip('123456789012')).toBe('123456789012')
})

test('clip cuts longer text to 11 chars and an ellipsis', () => {
  expect(clip('1234567890123')).toBe('12345678901…')
})

const MAIN = '/a/repo/.git\n/a/repo/.git\n'
const LINKED = '/a/repo/.git/worktrees/wt\n/a/repo/.git\n'

test('parseWhere shows no worktree name in the main worktree', () => {
  expect(parseWhere({ toplevel: '/a/b/repo\n', dirs: MAIN, branch: 'main\n', sha: 'abc1234\n' })).toEqual({ worktree: null, branch: 'main' })
})

test('parseWhere uses the toplevel basename in a linked worktree', () => {
  expect(parseWhere({ toplevel: '/a/b/wt\n', dirs: LINKED, branch: 'feat\n', sha: 'x' })).toEqual({ worktree: 'wt', branch: 'feat' })
})

test('parseWhere falls back to the sha on a detached HEAD', () => {
  expect(parseWhere({ toplevel: '/a/repo', dirs: MAIN, branch: '', sha: 'abc1234' })).toEqual({ worktree: null, branch: 'abc1234' })
})

test('parseWhere clips long names', () => {
  expect(parseWhere({ toplevel: '/a/agent-a0b31a7761269161b', dirs: LINKED, branch: 'mods-statusbar-where', sha: 'x' })).toEqual({ worktree: 'agent-a0b31…', branch: 'mods-status…' })
})

test('parseWhere is null outside a repo', () => {
  expect(parseWhere({ toplevel: '', dirs: '', branch: '', sha: '' })).toBeNull()
})
