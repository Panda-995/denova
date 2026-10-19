import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { once } from 'node:events'
import { mkdtemp, readFile, readdir, rm, writeFile } from 'node:fs/promises'
import { createServer } from 'node:http'
import os from 'node:os'
import path from 'node:path'
import { stringify } from 'smol-toml'
import { evaluateCreative, toolDirectory } from './run.mjs'
import DenovaProvider from './provider.mjs'

// Explicit integration verification with a real product backend and local
// model fixture. No development server, real credentials, or paid APIs.
const temporary = await mkdtemp(path.join(os.tmpdir(), 'denova-eval-verify-'))
let calls = 0
let cancelNextRequest
const controller = new AbortController()
for (const signal of ['SIGINT', 'SIGTERM']) process.once(signal, () => controller.abort(new Error(`Verification cancelled by ${signal}`)))
const fixture = createServer(async (request, response) => {
  try {
    let raw = ''
    for await (const chunk of request) raw += chunk
    const body = JSON.parse(raw)
    if (cancelNextRequest) {
      const cancel = cancelNextRequest
      cancelNextRequest = undefined
      cancel()
      return
    }
    assert.ok(++calls < 100, 'Fixture exceeded its expected bounded request count')
    const messages = JSON.stringify(body.messages)
    const game = body.tools?.some(item => item.function.name === 'submit_interactive_turn')
    let narrative = 'Mira cannot create water. The dry cup remains empty; her copper token is unchanged.'
    let tools = []
    if (game) {
      const latestUser = [...body.messages].reverse().find(item => item.role === 'user')
      const action = JSON.stringify(latestUser)
      if (action.includes('alternate branch, an archivist')) narrative = 'The archivist tells Mira the password NIGHTGLASS-742. She remembers it and remains outside.'
      else if (action.includes('currently know about entering')) narrative = 'Mira knows no password and has met no archivist. She waits outside the locked archive.'
      else if (messages.includes('locked archive')) narrative = 'Mira waits outside the locked archive, knowing no secrets.'
      tools = [{ name: 'submit_interactive_turn', arguments: {
        state_changes: [
          { op: 'replace', actor_id: 'story', field_id: '当前详细地点', value: messages.includes('locked archive') ? 'Outside the archive' : 'Dry stone room' },
          { op: 'replace', actor_id: 'story', field_id: '当前事件', value: narrative },
        ], choices: ['Wait', 'Observe'],
      } }]
    } else if (body.tools?.some(item => item.function.name === 'write')) {
      const hasResult = body.messages.some(item => item.role === 'tool')
      if (!hasResult) {
        const scoped = messages.includes('Mira paused at the threshold.')
        tools = [{ name: 'write', arguments: { path: 'chapters/scene.md', content: scoped
          ? '# Arrival\n\nMira paused at the threshold.\n\nThe brass clock was silent.\n'
          : '# The empty cup\n\nMira cannot create water. Her cup remains empty.\n' } }]
        if (body.model === 'fixture-bad' && scoped) tools.push({ name: 'write', arguments: { path: 'chapters/untouched.md', content: 'This unauthorized edit must fail evaluation.\n' } })
      }
      narrative = 'Saved the requested chapter.'
    } else {
      if (body.model === 'fixture-grader') {
        assert.ok(messages.includes('Original user request:'), 'Semantic grader did not receive the original request')
        assert.ok(messages.includes('Initial project facts:'), 'Semantic grader did not receive fixture facts')
      }
      narrative = JSON.stringify({ pass: true, score: 1, reason: 'Deterministic harness fixture; real semantics are not evaluated here.' })
    }
    const tool_calls = tools.map((tool, index) => ({ id: `fixture-${calls}-${index}`, index, type: 'function', function: { name: tool.name, arguments: JSON.stringify(tool.arguments) } }))
    const usage = { prompt_tokens: 10, completion_tokens: 10, total_tokens: 20 }
    if (!body.stream) {
      response.writeHead(200, { 'Content-Type': 'application/json' })
      response.end(JSON.stringify({ id: 'fixture', choices: [{ index: 0, message: { role: 'assistant', content: narrative }, finish_reason: 'stop' }], usage }))
      return
    }
    response.writeHead(200, { 'Content-Type': 'text/event-stream' })
    const frame = { id: `fixture-${calls}`, object: 'chat.completion.chunk', created: 1, model: 'fixture' }
    response.write(`data: ${JSON.stringify({ ...frame, choices: [{ index: 0, delta: { role: 'assistant', content: narrative, ...(tools.length ? { tool_calls } : {}) }, finish_reason: tools.length ? 'tool_calls' : 'stop' }] })}\n\n`)
    response.end(`data: ${JSON.stringify({ ...frame, choices: [], usage })}\n\ndata: [DONE]\n\n`)
  } catch (error) {
    response.writeHead(500, { 'Content-Type': 'application/json' })
    response.end(JSON.stringify({ error: error.message }))
  }
})

try {
  fixture.listen(0, '127.0.0.1')
  await once(fixture, 'listening')
  const baseURL = `http://127.0.0.1:${fixture.address().port}/v1`
  const settingsFile = path.join(temporary, 'settings.toml')
  const secret = 'fixture-secret-not-for-report-export'
  const modelSettings = {
    model_endpoints: [{ id: 'fixture', provider: 'openai-compatible', protocol: 'openai-chat-completions', base_url: baseURL, api_key: secret }],
    model_profiles: ['fixture', 'fixture-bad', 'fixture-grader'].map(id => ({ id, endpoint_id: 'fixture', model: id, context_window_tokens: 100000 })),
    agent_models: { default: { profile_id: 'fixture', thinking_level: 'off' } },
  }
  await writeFile(settingsFile, stringify(modelSettings))
  process.env.DENOVA_EVAL_FIXTURE_GRADER_KEY = secret
  const configPath = path.join(temporary, 'run.json')
  const configuration = { settings_file: settingsFile,
    variants: [{ label: 'fixture', profile_id: 'fixture' }],
    grader: { profile_id: 'fixture-grader' },
  }
  await writeFile(configPath, JSON.stringify(configuration))
  const executable = path.join(temporary, process.platform === 'win32' ? 'denova.exe' : 'denova')
  const build = spawn('go', ['build', '-o', executable, './cmd/denova'], { cwd: path.resolve(toolDirectory, '../..'), stdio: 'inherit', signal: controller.signal })
  assert.equal((await once(build, 'exit'))[0], 0)
  const positive = await evaluateCreative({ configPath, binary: executable, signal: controller.signal })
  assert.equal(positive.summary.stats.successes, 4, JSON.stringify(positive.summary.results.map(item => ({ description: item.testCase?.description, error: item.error, reason: item.gradingResult?.reason }))))
  assert.equal(positive.summary.stats.errors, 0)
  assert.equal(positive.summary.stats.failures, 0)
  const report = await readFile(path.join(positive.outputDirectory, 'report.json'), 'utf8')
  assert.ok(!report.includes(secret), 'Report exported a credential')
  assert.ok((await readFile(path.join(positive.outputDirectory, 'report.html'), 'utf8')).includes('writing-scoped-edit'))
  for (const entry of await readdir(positive.outputDirectory, { withFileTypes: true })) {
    if (!entry.isDirectory()) continue
    await assert.rejects(readFile(path.join(positive.outputDirectory, entry.name, '.denova/config.toml')), { code: 'ENOENT' })
    const remaining = await readdir(path.join(positive.outputDirectory, entry.name, '.denova'))
    assert.ok(!remaining.some(file => file.startsWith('config.toml')), 'Temporary credential backup was retained')
  }
  configuration.variants = [{ label: 'baseline', profile_id: 'fixture' }, { label: 'candidate', profile_id: 'fixture-bad' }]
  configuration.grader = { id: 'openai:chat:fixture-grader', config: { apiBaseUrl: baseURL, apiKeyEnvar: 'DENOVA_EVAL_FIXTURE_GRADER_KEY' } }
  await writeFile(configPath, JSON.stringify(configuration))
  const negative = await evaluateCreative({ configPath, binary: executable, filter: 'writing-scoped-edit', signal: controller.signal })
  assert.equal(negative.summary.stats.successes, 1)
  assert.equal(negative.summary.stats.errors, 0)
  assert.equal(negative.summary.stats.failures, 1, 'The deliberate unauthorized edit was not detected')
  const cancellation = new AbortController()
  cancelNextRequest = () => cancellation.abort(new Error('Verification requested cancellation'))
  const scenario = JSON.parse(await readFile(new URL('./cases.json', import.meta.url), 'utf8'))[0]
  const cancelled = await new DenovaProvider({ label: 'cancelled', variant: { profile_id: 'fixture' },
    settings: modelSettings, binary: executable, skillsDirectory: path.resolve(toolDirectory, '../../skills'),
    outputDirectory: positive.outputDirectory }).callApi(scenario.prompt, { vars: { scenario } }, { abortSignal: cancellation.signal })
  assert.equal(cancelNextRequest, undefined, 'Cancellation never reached an in-flight model request')
  assert.ok(cancelled.error, 'Cancellation was recorded as successful output')
  const cancelledDirectory = path.dirname(cancelled.metadata.evidence_path)
  const cancelledEvidence = JSON.parse(await readFile(cancelled.metadata.evidence_path, 'utf8'))
  assert.notEqual(cancelledEvidence.complete, true)
  assert.ok(!(await readdir(path.join(cancelledDirectory, '.denova'))).some(file => file.startsWith('config.toml')))
  console.log('[creative-eval] Verified four product scenarios, two-variant regression detection, report export, credential cleanup, and in-flight cancellation')
} finally {
  fixture.closeAllConnections()
  await new Promise(resolve => fixture.close(resolve))
  await rm(temporary, { recursive: true, force: true })
}
