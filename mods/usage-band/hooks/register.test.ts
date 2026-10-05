import { expect, test } from 'claude-code/testing'
import type { Engine } from 'claude-code/testing'

const AUTH = { exitCode: 0, stdout: '{"orgName":"a@acme.com\'s Organization"}', stderr: '', isStdoutTruncated: false, isStderrTruncated: false }
const BAND = { hasSurvey: false, isWorking: false, maxRows: 10, bodyColumns: 120, scroll: { offset: 0, bodyRows: 10 }, view: {} }
const SWITCH = { requested_model: 'sonnet', source: 'command', context_tokens: 0, prompt_cache_warm: false, cache_ttl: '5m', estimated_cache_write_usd: 0, pricing: 'catalog' } as const

const band = ($: Engine) => $.ui.mount({ plugin: 'usage-band', surface: 'terminal', component: 'AbovePrompt', props: BAND })

test('/clear fetches org again', async ($, on) => {
  on('process.run', () => ({ value: AUTH }))
  on('classic.SessionStart', () => ({}))
  await $.classic.SessionStart({ source: 'clear' })
  expect(await (await band($)).find({ type: 'Text', text: /^acme\.com$/ })).toBeDefined()
})

test('/clear survives claude failing to run', async ($, on) => {
  on('process.run', () => ({ deny: 'no claude' }))
  on('classic.SessionStart', () => ({}))
  await $.classic.SessionStart({ source: 'clear' })
  expect(await (await band($)).find({ type: 'Text', text: /^acme\.com$/ })).toBeUndefined()
})

test('/model switch shows the new model', async ($, on) => {
  on('classic.PostModelSwitch', () => ({}))
  await $.classic.PostModelSwitch({ ...SWITCH, from_model: 'claude-opus-5-5', to_model: 'claude-sonnet-5-6' })
  expect(await (await band($)).find({ type: 'Text', text: /^sonnet56$/ })).toBeDefined()
})

test('no limits: no empty separator', async $ => {
  expect(await (await band($)).find({ type: 'Text', text: /^ \|$/ })).toBeUndefined()
})
