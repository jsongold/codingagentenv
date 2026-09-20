'use strict';
const { test, beforeEach } = require('node:test');
const assert = require('node:assert');
const { spawnSync, spawn } = require('node:child_process');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');

const TQ = path.join(__dirname, '..', 'bin', 'tq');
let file;

beforeEach(() => {
  file = path.join(fs.mkdtempSync(path.join(os.tmpdir(), 'tq-')), 'tasks.jsonl');
});

function tq(...args) {
  const r = spawnSync(TQ, args, { env: { ...process.env, TQ_FILE: file }, encoding: 'utf8' });
  const lines = r.stdout.split('\n').filter(Boolean).map((l) => JSON.parse(l));
  return { code: r.status, stderr: r.stderr, lines, task: lines[0] };
}

test('enqueue assigns sequential ids and defaults', () => {
  const a = tq('enqueue', 'first', 'task').task;
  const b = tq('enqueue', 'second', '--agent', 'Explore').task;
  assert.equal(a.id, 'T-001');
  assert.equal(a.title, 'first task');
  assert.equal(a.state, 'queued');
  assert.equal(a.agent, 'general-purpose');
  assert.equal(b.id, 'T-002');
  assert.equal(b.agent, 'Explore');
});

test('claim takes oldest queued task, exit 2 when empty', () => {
  tq('enqueue', 'a');
  tq('enqueue', 'b');
  assert.equal(tq('claim').task.id, 'T-001');
  assert.equal(tq('claim').task.id, 'T-002');
  const empty = tq('claim');
  assert.equal(empty.code, 2);
  assert.match(empty.stderr, /queue empty/);
});

test('claim --id refuses a non-queued task', () => {
  tq('enqueue', 'a');
  tq('claim');
  const r = tq('claim', '--id', 'T-001');
  assert.equal(r.code, 1);
  assert.match(r.stderr, /is running/);
});

test('done/fail require running state; requeue restores', () => {
  tq('enqueue', 'a');
  assert.equal(tq('done', 'T-001').code, 1);
  tq('claim');
  const failed = tq('fail', 'T-001', '--note', 'boom').task;
  assert.equal(failed.state, 'failed');
  assert.equal(failed.note, 'boom');
  assert.equal(tq('requeue', 'T-001').task.state, 'queued');
  tq('claim');
  assert.equal(tq('done', 'T-001').task.state, 'done');
});

test('list filters by state and rejects unknown state', () => {
  tq('enqueue', 'a');
  tq('enqueue', 'b');
  tq('claim');
  assert.deepEqual(tq('list', '--state', 'queued').lines.map((t) => t.id), ['T-002']);
  assert.equal(tq('list').lines.length, 2);
  assert.equal(tq('list', '--state', 'bogus').code, 1);
});

test('unknown task and unknown command fail', () => {
  assert.equal(tq('show', 'T-999').code, 1);
  assert.equal(tq('nope').code, 1);
  assert.equal(tq('enqueue').code, 1);
});

test('concurrent enqueue loses no tasks and yields unique ids', async () => {
  const N = 20;
  await Promise.all(
    Array.from({ length: N }, (_, i) =>
      new Promise((resolve, reject) => {
        const p = spawn(TQ, ['enqueue', `task ${i}`], { env: { ...process.env, TQ_FILE: file } });
        p.on('exit', (c) => (c === 0 ? resolve() : reject(new Error(`exit ${c}`))));
      })
    )
  );
  const ids = tq('list').lines.map((t) => t.id);
  assert.equal(ids.length, N);
  assert.equal(new Set(ids).size, N);
});

test('concurrent claim never hands out the same task twice', async () => {
  const N = 10;
  for (let i = 0; i < N; i++) tq('enqueue', `t${i}`);
  const claimed = await Promise.all(
    Array.from({ length: N }, () =>
      new Promise((resolve) => {
        let buf = '';
        const p = spawn(TQ, ['claim'], { env: { ...process.env, TQ_FILE: file } });
        p.stdout.on('data', (d) => (buf += d));
        p.on('exit', () => resolve(JSON.parse(buf).id));
      })
    )
  );
  assert.equal(new Set(claimed).size, N);
});

test('a failing command releases the lock', () => {
  tq('enqueue', 'a');
  assert.equal(tq('done', 'T-001').code, 1);
  assert.equal(fs.existsSync(file + '.lock'), false);
});
