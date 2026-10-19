import { pathToFileURL } from 'node:url'

export function currentRun(runs, sha) {
  return runs.find(run => run.head_sha === sha
    && ['push', 'workflow_dispatch', 'schedule'].includes(run.event))
}

export function githubAPI({ repository, token, fetcher = fetch, apiURL = 'https://api.github.com' }) {
  return async (workflow, method = 'GET', body, sha) => {
    const suffix = method === 'POST' ? 'dispatches' : `runs?head_sha=${sha}&per_page=100`
    const response = await fetcher(`${apiURL}/repos/${repository}/actions/workflows/${workflow}/${suffix}`, {
      method,
      headers: { Authorization: `Bearer ${token}`, Accept: 'application/vnd.github+json', 'Content-Type': 'application/json' },
      ...(body ? { body: JSON.stringify(body) } : {}),
      signal: AbortSignal.timeout(30_000),
    })
    if (!response.ok) throw new Error(`GitHub ${method} ${workflow}: HTTP ${response.status}`)
    return method === 'GET' ? response.json() : undefined
  }
}

export async function ensureWorkflow(api, workflow, sha, ref = 'master') {
  const run = currentRun((await api(workflow, 'GET', undefined, sha)).workflow_runs, sha)
  if (run && (run.conclusion === 'success' || run.status !== 'completed')) {
    return `Already successful or running: ${workflow} (${run.id}) for ${sha}`
  }
  await api(workflow, 'POST', { ref, ...(workflow === 'ci.yml' ? { inputs: { suite: 'smoke' } } : {}) })
  return `Dispatched ${workflow} for ${sha}`
}

export async function waitForCI(api, sha, {
  sleep = ms => new Promise(resolve => setTimeout(resolve, ms)),
  now = Date.now,
  timeoutMs = 50 * 60_000,
  intervalMs = 20_000,
} = {}) {
  const deadline = now() + timeoutMs
  while (now() < deadline) {
    const run = currentRun((await api('ci.yml', 'GET', undefined, sha)).workflow_runs, sha)
    if (run?.status === 'completed') {
      if (run.conclusion !== 'success') throw new Error(`Source CI ${run.id} ended with ${run.conclusion}; latest will not be published`)
      return `Source CI passed: ${run.html_url}`
    }
    await sleep(intervalMs)
  }
  throw new Error(`Timed out waiting for source CI of ${sha}; latest will not be published`)
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const { GITHUB_REPOSITORY: repository, GH_TOKEN: token, SOURCE_SHA: sha } = process.env
  if (!repository || !token || !/^[a-f0-9]{40}$/.test(sha || '')) throw new Error('GITHUB_REPOSITORY, GH_TOKEN and SOURCE_SHA are required')
  const api = githubAPI({ repository, token, apiURL: process.env.GITHUB_API_URL })
  if (process.argv[2] === 'ensure') {
    for (const workflow of ['ci.yml', 'docker-publish.yml']) console.log(await ensureWorkflow(api, workflow, sha))
  } else if (process.argv[2] === 'wait') {
    console.log(await waitForCI(api, sha))
  } else {
    throw new Error('Expected ensure or wait')
  }
}
