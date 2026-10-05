import { expect, test } from 'claude-code/testing'
import { color, label, ordered, parseOrg } from './format'

test('labels', () => {
  expect(label('five_hour')).toBe('5h')
  expect(label('seven_day')).toBe('w')
  expect(label('seven_day_fable')).toBe('F')
  expect(label('Fable')).toBe('F')
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
