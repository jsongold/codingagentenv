import { expect, test } from 'claude-code/testing'
import type { Engine } from 'claude-code/testing'

const AUTH = { exitCode: 0, stdout: '{"orgName":"a@acme.com\'s Organization"}', stderr: '', isStdoutTruncated: false, isStderrTruncated: false }
const BAND = { hasSurvey: false, isWorking: false, maxRows: 10, bodyColumns: 120, scroll: { offset: 0, bodyRows: 10 }, view: {} }
const SWITCH = { requested_model: 'sonnet', source: 'command', context_tokens: 0, prompt_cache_warm: false, cache_ttl: '5m', estimated_cache_write_usd: 0, pricing: 'catalog' } as const

const band = ($: Engine) => $.ui.mount({ plugin: 'statusbar', surface: 'terminal', component: 'AbovePrompt', props: BAND })

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

const ok = (stdout: string) => ({ exitCode: 0, stdout, stderr: '', isStdoutTruncated: false, isStderrTruncated: false })
const fail = { exitCode: 128, stdout: '', stderr: 'fatal', isStdoutTruncated: false, isStderrTruncated: false }
const STOP = { stop_hook_active: false, last_assistant_message: '' } as never

// Answers git by argv; outside a repo every git call fails.
const git = (on: (name: 'process.run' | 'session.cwd', h: (_$: unknown, e: { argv: readonly string[] }) => unknown) => void, inRepo: boolean, branch = 'feature-x') => {
  on('session.cwd', () => ({ value: '/work/repo' }))
  on('process.run', (_$, e) => {
    if (!inRepo) return { value: fail }
    const a = e.argv.join(' ')
    if (a.includes('--show-toplevel')) return { value: ok('/work/repo\n') }
    if (a.includes('--show-current')) return { value: ok(`${branch}\n`) }
    return { value: ok('abc1234\n') }
  })
}

test('worktree and branch show after org', async ($, on) => {
  git(on as never, true)
  on('classic.Stop', () => ({}))
  await $.classic.Stop(STOP)
  const b = await band($)
  expect(await b.find({ type: 'Text', text: /^repo$/ })).toBeDefined()
  expect(await b.find({ type: 'Text', text: /^feature-x$/ })).toBeDefined()
})

test('outside a git repo no where field is shown', async ($, on) => {
  git(on as never, false)
  on('classic.Stop', () => ({}))
  await $.classic.Stop(STOP)
  expect(await (await band($)).find({ type: 'Text', text: /^repo$/ })).toBeUndefined()
})

test('a lookup whose cwd changed meanwhile is dropped', async ($, on) => {
  let calls = 0
  on('session.cwd', () => ({ value: ++calls === 1 ? '/old' : '/new' }))
  on('process.run', () => ({ value: ok('/old/stale\n') }))
  on('classic.Stop', () => ({}))
  await $.classic.Stop(STOP)
  expect(await (await band($)).find({ type: 'Text', text: /^stale$/ })).toBeUndefined()
})

test('cwd change shows the new branch', async ($, on) => {
  git(on as never, true, 'moved')
  on('classic.CwdChanged', () => ({}))
  await $.classic.CwdChanged({ old_cwd: '/a', new_cwd: '/b' })
  expect(await (await band($)).find({ type: 'Text', text: /^moved$/ })).toBeDefined()
})
