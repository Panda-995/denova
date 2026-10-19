import assert from 'node:assert/strict'
import { test } from 'node:test'
import { currentRun, ensureWorkflow, githubAPI, waitForCI } from './image-ci.mjs'

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

test('missing build dispatches the fork workflow without CI-only inputs', async () => {
  const calls = []
  const api = async (...args) => { calls.push(args); return { workflow_runs: [] } }
  await ensureWorkflow(api, 'docker-publish.yml', sha)
  assert.deepEqual(calls[1], ['docker-publish.yml', 'POST', { ref: 'master' }])
})

test('another commit or untrusted PR cannot satisfy the source CI gate', () => {
  assert.equal(currentRun([run('completed', 'success', { head_sha: 'b'.repeat(40) }), run('completed', 'success', { event: 'pull_request' })], sha), undefined)
})

test('CI gate waits through queued and running states until success', async () => {
  const runs = [run('queued', null), run('in_progress', null), run('completed', 'success', { html_url: 'https://github.com/example/denova/actions/runs/42' })]
  let sleeps = 0
  const result = await waitForCI(async () => ({ workflow_runs: [runs.shift()] }), sha, { sleep: async () => { sleeps++ } })
  assert.equal(sleeps, 2)
  assert.match(result, /Source CI passed/)
})

for (const conclusion of ['failure', 'cancelled', 'timed_out', 'skipped']) {
  test(`CI gate refuses ${conclusion} without retrying or publishing`, async () => {
    await assert.rejects(waitForCI(async () => ({ workflow_runs: [run('completed', conclusion)] }), sha), /latest will not be published/)
  })
}

test('missing CI is bounded by the wait deadline', async () => {
  let elapsed = 0
  await assert.rejects(waitForCI(async () => ({ workflow_runs: [] }), sha, {
    now: () => elapsed, sleep: async ms => { elapsed += ms }, timeoutMs: 100, intervalMs: 50,
  }), /Timed out/)
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
