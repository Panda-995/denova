import assert from 'node:assert/strict'
import { test } from 'node:test'
import { currentRun, ensureWorkflow, githubAPI } from './image-ci.mjs'

const sha = 'a'.repeat(40)
const run = (status, conclusion, extra = {}) => ({ id: 42, head_sha: sha, event: 'workflow_dispatch', status, conclusion, ...extra })

for (const [status, conclusion, dispatch] of [
  ['completed', 'success', false], ['in_progress', null, false], ['queued', null, false],
  ['completed', 'failure', true], ['completed', 'cancelled', true], ['completed', 'timed_out', true],
]) {
  test(`publication decision: ${status}/${conclusion}`, async () => {
    const calls = []
    const api = async (...args) => {
      calls.push(args)
      return { workflow_runs: [run(status, conclusion)] }
    }
    await ensureWorkflow(api, 'ci.yml', sha)
    assert.equal(calls.length, dispatch ? 2 : 1)
    assert.deepEqual(calls[0], ['ci.yml', 'GET', undefined, sha])
    if (dispatch) assert.deepEqual(calls[1], ['ci.yml', 'POST', { ref: 'master', inputs: { suite: 'smoke' } }])
  })
}

test('missing source CI dispatches the verified pipeline', async () => {
  const calls = []
  const api = async (...args) => { calls.push(args); return { workflow_runs: [] } }
  await ensureWorkflow(api, 'ci.yml', sha)
  assert.deepEqual(calls[1], ['ci.yml', 'POST', { ref: 'master', inputs: { suite: 'smoke' } }])
})

test('another commit or untrusted PR cannot satisfy the source CI gate', () => {
  assert.equal(currentRun([run('completed', 'success', { head_sha: 'b'.repeat(40) }), run('completed', 'success', { event: 'pull_request' })], sha), undefined)
})

test('API requests target the explicit fork and exact source commit', async () => {
  let request
  const api = githubAPI({ repository: 'Panda-995/denova', token: 'test-token', fetcher: async (...args) => {
    request = args
    return { ok: true, json: async () => ({ workflow_runs: [] }) }
  } })
  await api('ci.yml', 'GET', undefined, sha)
  assert.equal(request[0], `https://api.github.com/repos/Panda-995/denova/actions/workflows/ci.yml/runs?head_sha=${sha}&per_page=100`)
  assert.equal(request[1].headers.Authorization, 'Bearer test-token')
})

test('permission or API failures cannot silently count as successful verification', async () => {
  const api = githubAPI({ repository: 'Panda-995/denova', token: 'test-token', fetcher: async () => ({ ok: false, status: 403 }) })
  await assert.rejects(ensureWorkflow(api, 'ci.yml', sha), /HTTP 403/)
})

test('transient read failures are retried before accepting a verified response', async () => {
  let calls = 0
  const waits = []
  const api = githubAPI({ repository: 'Panda-995/denova', token: 'test-token', sleep: async ms => waits.push(ms), fetcher: async () => {
    calls++
    return calls < 3 ? { ok: false, status: 502 } : { ok: true, json: async () => ({ workflow_runs: [run('completed', 'success')] }) }
  } })
  assert.match(await ensureWorkflow(api, 'ci.yml', sha), /Already successful/)
  assert.equal(calls, 3)
  assert.deepEqual(waits, [1000, 2000])
})

test('persistent service failures remain failures after a bounded retry budget', async () => {
  let calls = 0
  const api = githubAPI({ repository: 'Panda-995/denova', token: 'test-token', sleep: async () => {}, fetcher: async () => {
    calls++
    return { ok: false, status: 503 }
  } })
  await assert.rejects(ensureWorkflow(api, 'ci.yml', sha), /HTTP 503/)
  assert.equal(calls, 4)
})

test('a failed dispatch is not automatically duplicated', async () => {
  let calls = 0
  const api = githubAPI({ repository: 'Panda-995/denova', token: 'test-token', sleep: async () => {}, fetcher: async () => {
    calls++
    return { ok: false, status: 502 }
  } })
  await assert.rejects(api('ci.yml', 'POST', { ref: 'master' }), /HTTP 502/)
  assert.equal(calls, 1)
})
