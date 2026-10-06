import { expect, test } from 'claude-code/testing'
import { untilReset } from './reset'

const NOW = Date.parse('2026-01-01T00:00:00Z')
const at = (ms: number) => new Date(NOW + ms).toISOString()
const H = 3600000
const M = 60000
const D = 24 * H

test('five_hour shows h:mm with 2-digit minutes', () => {
  expect(untilReset('five_hour', at(2 * H + 15 * M), NOW)).toBe('2:15')
  expect(untilReset('five_hour', at(H + 5 * M + 59000), NOW)).toBe('1:05')
  expect(untilReset('five_hour', at(30 * M), NOW)).toBe('0:30')
})

test('seven_day family shows whole days, floored', () => {
  expect(untilReset('seven_day', at(3 * D + 23 * H), NOW)).toBe('3d')
  expect(untilReset('seven_day', at(23 * H), NOW)).toBe('0d')
  expect(untilReset('seven_day_opus', at(2 * D), NOW)).toBe('2d')
  expect(untilReset('seven_day_sonnet', at(2 * D), NOW)).toBe('2d')
  expect(untilReset('seven_day_fable', at(2 * D), NOW)).toBe('2d')
})

test('a past reset counts as 0', () => {
  expect(untilReset('five_hour', at(-H), NOW)).toBe('0:00')
  expect(untilReset('seven_day', at(-D), NOW)).toBe('0d')
})

test('missing, invalid or other kinds give null', () => {
  expect(untilReset('five_hour', undefined, NOW)).toBeNull()
  expect(untilReset('five_hour', 'soon', NOW)).toBeNull()
  expect(untilReset('spend_limit', at(H), NOW)).toBeNull()
})
