import { expect, test } from 'claude-code/testing'
import { color, formatEffort, label, ordered, parseOrg, shortModel } from './format'

test('labels', () => {
  expect(label('five_hour')).toBe('5h')
  expect(label('seven_day')).toBe('w')
  expect(label('seven_day_fable')).toBe('F')
  expect(label('Fable')).toBe('F')
  expect(label('seven_day_Opus')).toBe('ow')
  expect(label('Seven_Day_Sonnet')).toBe('sw')
  expect(label('seven_day_Opus')).toBe('ow')
  expect(label('Seven_Day_Sonnet')).toBe('sw')
  expect(label('spend_limit')).toBe('spend_limit')
})

test('colors by threshold', () => {
  expect(color(undefined)).toBe('gray')
  expect(color(10)).toBe('green')
  expect(color(50)).toBe('yellow')
  expect(color(95)).toBe('red')
})

test('org from auth status', () => {
  expect(parseOrg('{"orgName":"Acme"}')).toBe('Acme')
  expect(parseOrg('not json')).toBe(null)
})

test('org shortened between @ and \'s', () => {
  expect(parseOrg(`{"orgName":"a.b@cafkah.com's Organization"}`)).toBe('cafkah.com')
})

test('F ordered last', () => {
  const kinds = ordered([{ kind: 'seven_day_fable' }, { kind: 'five_hour' }, { kind: 'seven_day' }]).map(l => label(l.kind))
  expect(kinds).toEqual(['5h', 'w', 'F'])
})

test('effort shortened to <= 4 chars', () => {
  expect(formatEffort('low')).toBe('low')
  expect(formatEffort('medium')).toBe('mid')
  expect(formatEffort('high')).toBe('high')
  expect(formatEffort('xhigh')).toBe('xl')
  expect(formatEffort('max')).toBe('max')
  expect(formatEffort(8000)).toBe('8000')
  expect(formatEffort(123456)).toBe('1234')
  expect(formatEffort('turbo')).toBe('?')
  expect(formatEffort(undefined)).toBe(null)
})

test('model shortened to family + version', () => {
  expect(shortModel('claude-opus-5-5')).toBe('opus55')
  expect(shortModel('Opus 5.5')).toBe('opus55')
  expect(shortModel('claude-sonnet-5-6')).toBe('sonnet56')
  expect(shortModel('claude-haiku-4-5-20251001')).toBe('haiku45')
  expect(shortModel('Fable 5.1')).toBe('fable51')
  expect(shortModel('claude-fable-5-1')).toBe('fable51')
  expect(shortModel('claude-opus-5-5[1m]')).toBe('opus55')
  expect(shortModel('claude-opus-4-20250514')).toBe('opus4')
  expect(shortModel('Default (recommended)')).toBe('?')
  expect(shortModel('claude-lyra-6-0')).toBe('lyra60')
  expect(shortModel('claude-lyra-6-0-20270101[1m]')).toBe('lyra60')
  expect(shortModel('Lyra 6.0')).toBe('lyra60')
  expect(shortModel('Lyra 6')).toBe('lyra6')
  expect(shortModel('')).toBe('?')
  expect(shortModel(null)).toBe('?')
})
