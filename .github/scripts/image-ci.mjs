import { pathToFileURL } from 'node:url'

export function currentRun(runs, sha) {
  return runs.find(run => run.head_sha === sha
    && ['push', 'workflow_dispatch', 'schedule'].includes(run.event))
}

export function githubAPI({ repository, token, fetcher = fetch, apiURL = 'https://api.github.com', sleep = ms => new Promise(resolve => setTimeout(resolve, ms)) }) {
  return async (workflow, method = 'GET', body, sha) => {
    const suffix = method === 'POST' ? 'dispatches' : `runs?head_sha=${sha}&per_page=100`
    const url = `${apiURL}/repos/${repository}/actions/workflows/${workflow}/${suffix}`
    const options = {
      method,
      headers: { Authorization: `Bearer ${token}`, Accept: 'application/vnd.github+json', 'Content-Type': 'application/json', 'Cache-Control': 'no-cache' },
      ...(body ? { body: JSON.stringify(body) } : {}),
    }
    // Retry only reads: retrying a dispatch after a lost response could start
    // duplicate workflows. The next scheduled poll can reconcile that case.
    const attempts = method === 'GET' ? 4 : 1
    for (let attempt = 0; attempt < attempts; attempt++) {
      let response
      try {
        response = await fetcher(url, { ...options, signal: AbortSignal.timeout(30_000) })
      } catch (error) {
        if (attempt === attempts - 1) throw error
      }
      if (response?.ok) return method === 'GET' ? response.json() : undefined
      if (response && (![429, 500, 502, 503, 504].includes(response.status) || attempt === attempts - 1)) {
        throw new Error(`GitHub ${method} ${workflow}: HTTP ${response.status}`)
      }
      await sleep(1000 * 2 ** attempt)
    }
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

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const { GITHUB_REPOSITORY: repository, GH_TOKEN: token, SOURCE_SHA: sha } = process.env
  if (!repository || !token || !/^[a-f0-9]{40}$/.test(sha || '')) throw new Error('GITHUB_REPOSITORY, GH_TOKEN and SOURCE_SHA are required')
  const api = githubAPI({ repository, token, apiURL: process.env.GITHUB_API_URL })
  if (process.argv[2] === 'ensure') {
    console.log(await ensureWorkflow(api, 'ci.yml', sha))
  } else {
    throw new Error('Expected ensure')
  }
}
