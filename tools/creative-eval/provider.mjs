import { randomUUID } from 'node:crypto'
import { readFile, readdir, rm, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { stringify } from 'smol-toml'
import { startBackend, request } from './backend.mjs'

// Calls the product APIs; evaluation never replaces the Native execution loop.
export default class DenovaProvider {
  constructor({ label, variant, settings, binary, skillsDirectory, outputDirectory }) {
    this.label = label
    this.variant = variant
    this.settings = settings
    this.binary = binary
    this.skillsDirectory = skillsDirectory
    this.outputDirectory = outputDirectory
  }

  id() { return `denova:${this.label}` }

  async callApi(prompt, context, options) {
    const scenario = context.vars.scenario
    const directory = path.join(this.outputDirectory, `${scenario.id}-${randomUUID()}`)
    const evidence = { case_id: scenario.id, variant: this.label, checks: [], events: [] }
    const configuration = {
      ...this.settings,
      language: 'en-US', allow_lan_access: false,
      update_check_enabled: false, update_download_enabled: false,
      agent_approval_mode: 'write',
      agent_runtimes: { ide: { selected: 'native' }, interactive_story: { selected: 'native' } },
    }
    if (this.variant.profile_id) {
      for (const kind of ['default', 'ide', 'interactive_story', 'general']) {
        configuration.agent_models = { ...configuration.agent_models,
          [kind]: { ...configuration.agent_models?.[kind], profile_id: this.variant.profile_id } }
      }
    }
    let backend
    try {
      backend = await startBackend({ binary: this.binary, directory, configuration: stringify(configuration),
        skillsDirectory: this.skillsDirectory, signal: options?.abortSignal })
      const api = (route, body) => request(backend.baseURL, route, { body, signal: options?.abortSignal })
      const created = await api('/api/books/create', { title: `Evaluation ${scenario.id}`, author: 'Denova evaluation' })
      evidence.project_id = created.project_id
      await api('/api/workspace/switch', { path: created.workspace })
      const projectRoute = `/api/projects/${encodeURIComponent(created.project_id)}`
      for (const [file, content] of Object.entries(scenario.files ?? {})) {
        const result = await api(`${projectRoute}/files/operations`, { operations: [{ id: file, kind: 'create', path: file, type: 'file', content }] })
        if (!result.results?.[0]?.ok) throw new Error(`Could not create fixture ${file}: ${JSON.stringify(result)}`)
      }
      if (scenario.lore) await api(`${projectRoute}/book/lore/items`, {
        name: 'Tidal magic', type: 'world', enabled: true, importance: 'major',
        load_mode: 'resident', content: scenario.lore, brief_description: scenario.lore,
      })
      const executionStarted = Date.now()
      const run = async (route, body) => {
        let completed = false
        await request(backend.baseURL, route, { body: { command_id: randomUUID(), ...body }, signal: options?.abortSignal,
          onEvent: ({ event, data }) => {
            const kind = event || data.type
            evidence.events.push({ type: kind, data })
            if (kind === 'done' || kind === 'finish') completed = true
            if (['error', 'data-agent-error', 'aborted', 'abort', 'suspended', 'ask_pending', 'data-agent-ask', 'data-agent-proposed-plan'].includes(kind)) {
              throw new Error(`Agent did not complete unattended: ${kind}: ${JSON.stringify(data)}`)
            }
          },
        })
        if (!completed) throw new Error('Agent stream ended without a completion event')
      }
      let output
      if (scenario.mode === 'writing') {
        const beforeFiles = await contentSnapshot(created.workspace)
        const session = await api('/api/sessions', { title: scenario.id })
        evidence.session_id = session.id
        await run('/api/chat', { session_id: session.id, message: prompt, writing_skill: this.variant.writing_skill || 'novel-lite' })
        evidence.files = {}
        for (const file of Object.keys(scenario.files)) {
          const saved = await api(`${projectRoute}/files/file?${new URLSearchParams({ path: file })}`)
          evidence.files[file] = saved.content
        }
        for (const file of scenario.protected_files ?? []) {
          evidence.checks.push({ name: `Preserve ${file}`, pass: evidence.files[file] === scenario.files[file] })
        }
        const afterFiles = await contentSnapshot(created.workspace)
        evidence.changed_files = [...new Set([...Object.keys(beforeFiles), ...Object.keys(afterFiles)])]
          .filter(file => beforeFiles[file] !== afterFiles[file])
        evidence.checks.push({ name: 'Modify only the requested chapter',
          pass: evidence.changed_files.every(file => file === scenario.target_file) })
        const content = evidence.files[scenario.target_file]
        evidence.checks.push({ name: 'Write the requested chapter', pass: Boolean(content?.trim()) && content !== scenario.files[scenario.target_file] })
        if (scenario.expected_content) evidence.checks.push({ name: 'Apply only the exact requested replacement', pass: content === scenario.expected_content })
        evidence.messages = await api(`/api/session/messages?${new URLSearchParams({ session_id: session.id })}`)
        output = content
      } else {
        const story = await api('/api/interactive/stories', {
          title: scenario.id, origin: scenario.origin,
          protagonist: { mode: 'custom', name: 'Mira', profile: 'A careful traveler with no magical ability and one copper token.' },
          planning_mode: 'disabled', state_schema_policy: { mode: 'fixed_template' },
          reply_target_chars: 300, choice_count: 2,
        })
        evidence.story_id = story.id
        const storyRoute = `/api/interactive/stories/${encodeURIComponent(story.id)}`
        const snapshot = branch => api(`${storyRoute}/snapshot?${new URLSearchParams({ branch })}`)
        const turn = (branch, message) => run('/api/interactive/chat', { mode: 'story', story_id: story.id, branch, message })
        await run('/api/interactive/chat', { mode: 'story', story_id: story.id, branch: 'main', start_opening: true })
        evidence.before = await snapshot('main')
        const branch = 'main'
        if (scenario.branch_action) {
          const fork = await api(`${storyRoute}/branches`, { parent_event_id: evidence.before.turns.at(-1).id, title: 'Alternate discovery' })
          await api(`${storyRoute}/switch-branch`, { branch_id: fork.id })
          await turn(fork.id, scenario.branch_action)
          evidence.alternate = await snapshot(fork.id)
          evidence.checks.push({ name: 'Actually establish knowledge on the alternate branch',
            pass: evidence.alternate.turns.slice(evidence.before.turns.length).some(turn => turn.narrative.includes('NIGHTGLASS-742')) })
          evidence.main_after_alternate = await snapshot('main')
          const project = value => ({ turns: value.turns.map(({ id, narrative }) => ({ id, narrative })), state: value.state })
          evidence.checks.push({ name: 'Alternate branch leaves the main branch unchanged',
            pass: JSON.stringify(project(evidence.before)) === JSON.stringify(project(evidence.main_after_alternate)) })
          await api(`${storyRoute}/switch-branch`, { branch_id: 'main' })
        }
        await turn(branch, prompt)
        evidence.after = await snapshot(branch)
        const finalTurn = evidence.after.turns.at(-1)
        evidence.checks.push({ name: 'Persist exactly one new turn on the evaluated branch', pass: evidence.after.turns.length === evidence.before.turns.length + 1 })
        evidence.checks.push({ name: 'Persist narrative and resolved state', pass: Boolean(finalTurn?.narrative?.trim()) && finalTurn.state_status === 'ready' })
        evidence.branch_id = branch
        output = finalTurn?.narrative || ''
        if (scenario.branch_action) evidence.checks.push({ name: 'Keep alternate knowledge out of the main branch', pass: !output.includes('NIGHTGLASS-742') })
      }
      evidence.output = output
      evidence.complete = true
      evidence.execution_duration_ms = Date.now() - executionStarted
      const usage = evidence.events.filter(item => ['token_usage', 'data-agent-token-usage'].includes(item.type))
        .map(item => item.data.data || item.data)
      const sum = key => usage.reduce((total, item) => total + (item[key] || 0), 0)
      const tokenUsage = usage.length ? { prompt: sum('prompt_tokens'), completion: sum('completion_tokens'), total: sum('total_tokens'), cached: sum('cached_prompt_tokens'), numRequests: sum('model_calls') } : undefined
      return {
        output: `${output}\n\n## Persisted evidence\n${JSON.stringify({ files: evidence.files, before: evidence.before?.state, after: evidence.after?.state, alternate: evidence.alternate?.turns.map(turn => turn.narrative) }, null, 2)}`,
        tokenUsage,
        metadata: { evidence_path: path.join(directory, 'evidence.json'), checks: evidence.checks,
          execution_duration_ms: evidence.execution_duration_ms,
          cost_note: 'Monetary cost is unavailable; promptfoo cost=0 is not a billing total.',
          project_id: evidence.project_id, session_id: evidence.session_id, story_id: evidence.story_id, branch_id: evidence.branch_id },
      }
    } catch (error) {
      evidence.error = error.message
      return { error: error.message, metadata: { evidence_path: path.join(directory, 'evidence.json') } }
    } finally {
      if (backend) await backend.stop()
      // Settings initialization can create migration backups containing the
      // same credentials. None of those temporary files is diagnostic evidence.
      const dataDirectory = path.join(directory, '.denova')
      const files = await readdir(dataDirectory).catch(error => {
        if (error.code === 'ENOENT') return []
        throw error
      })
      for (const file of files.filter(name => name === 'config.toml' || name.startsWith('config.toml.'))) {
        await rm(path.join(dataDirectory, file), { recursive: true, force: true })
      }
      // The directory is created before the child starts, including startup failures.
      await writeFile(path.join(directory, 'evidence.json'), JSON.stringify(evidence, null, 2))
    }
  }
}

async function contentSnapshot(directory, prefix = '') {
  const snapshot = {}
  for (const entry of await readdir(path.join(directory, prefix), { withFileTypes: true })) {
    if (!prefix && ['.git', '.denova', '.nova', 'book.json'].includes(entry.name)) continue
    const relative = path.posix.join(prefix, entry.name)
    if (entry.isDirectory()) Object.assign(snapshot, await contentSnapshot(directory, relative))
    else if (entry.isFile()) snapshot[relative] = (await readFile(path.join(directory, relative))).toString('base64')
    else throw new Error(`Unexpected non-regular fixture path: ${relative}`)
  }
  return snapshot
}
