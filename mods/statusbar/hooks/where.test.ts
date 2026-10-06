import { expect, test } from 'claude-code/testing'
import { clip, parseWhere } from './where'

test('clip keeps 12 chars or fewer', () => {
  expect(clip('abc')).toBe('abc')
  expect(clip('123456789012')).toBe('123456789012')
})

test('clip cuts longer text to 11 chars and an ellipsis', () => {
  expect(clip('1234567890123')).toBe('12345678901…')
})

test('parseWhere uses the toplevel basename and the branch', () => {
  expect(parseWhere({ toplevel: '/a/b/repo\n', branch: 'main\n', sha: 'abc1234\n' })).toEqual({ worktree: 'repo', branch: 'main' })
})

test('parseWhere falls back to the sha on a detached HEAD', () => {
  expect(parseWhere({ toplevel: '/a/repo', branch: '', sha: 'abc1234' })).toEqual({ worktree: 'repo', branch: 'abc1234' })
})

test('parseWhere clips long names', () => {
  expect(parseWhere({ toplevel: '/a/agent-a0b31a7761269161b', branch: 'mods-statusbar-where', sha: 'x' })).toEqual({ worktree: 'agent-a0b31…', branch: 'mods-status…' })
})

test('parseWhere is null outside a repo', () => {
  expect(parseWhere({ toplevel: '', branch: '', sha: '' })).toBeNull()
})
