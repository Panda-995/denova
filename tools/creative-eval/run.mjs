import { spawn } from 'node:child_process'
import { once } from 'node:events'
import { mkdir, readFile, rm, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { parseArgs } from 'node:util'
import { parse } from 'smol-toml'
import DenovaProvider from './provider.mjs'
import { structuralChecks } from './checks.mjs'
import { profileGrader } from './profile-grader.mjs'

export const toolDirectory = fileURLToPath(new URL('.', import.meta.url))
const repositoryRoot = path.resolve(toolDirectory, '../..')

export async function evaluateCreative({ configPath, cases, repeat = 1, filter, binary, signal }) {
  const config = JSON.parse(await readFile(configPath, 'utf8'))
  const resolveConfig = value => path.resolve(path.dirname(configPath), value)
  if (!config.grader) throw new Error('Configure a promptfoo grader in the evaluation config; semantic checks require an explicit grading model')
  const scenarios = cases || JSON.parse(await readFile(new URL('./cases.json', import.meta.url), 'utf8'))
  const selected = filter ? scenarios.filter(item => item.id.includes(filter)) : scenarios
  if (!selected.length) throw new Error(`No scenarios match ${filter}`)
  const variants = config.variants || [{ label: 'current' }]
  if (!variants.length || variants.some(item => !item.label) || new Set(variants.map(item => item.label)).size !== variants.length) {
    throw new Error('Variants must have unique, nonempty labels')
  }
  const outputDirectory = path.join(toolDirectory, 'results', `${new Date().toISOString().replaceAll(':', '-')}-${process.pid}`)
  await mkdir(outputDirectory, { recursive: true, mode: 0o700 })
  const executable = binary || path.join(outputDirectory, process.platform === 'win32' ? 'denova.exe' : 'denova')
  let gradingProfile
  try {
    const providers = []
    for (const variant of variants) {
      const settingsFile = variant.settings_file || config.settings_file
      if (!settingsFile) throw new Error(`Set settings_file for variant ${variant.label}`)
      const source = parse(await readFile(resolveConfig(settingsFile), 'utf8'))
      if (!gradingProfile && config.grader.profile_id) gradingProfile = profileGrader(source, config.grader.profile_id)
      if (variant.profile_id && !source.model_profiles?.some(item => item.id === variant.profile_id && item.enabled !== false)) {
        throw new Error(`Variant ${variant.label} selects an unavailable model profile: ${variant.profile_id}`)
      }
      // Copy model/context configuration only. Paths, access credentials, runtime
      // selections, workspace registries, and automation are deliberately excluded.
      const keys = ['model_endpoints', 'model_profiles', 'agent_models', 'agent_context', 'agent_prompts',
        'model_max_retries', 'openai_api_key', 'openai_base_url', 'openai_model', 'openai_context_window_tokens']
      const settings = Object.fromEntries(keys.filter(key => source[key] !== undefined).map(key => [key, source[key]]))
      providers.push(new DenovaProvider({ label: variant.label, variant, settings, binary: executable,
        skillsDirectory: variant.skills_directory ? resolveConfig(variant.skills_directory) : path.join(repositoryRoot, 'skills'), outputDirectory }))
    }
    if (!binary) {
      console.log('[creative-eval] Building the current checkout')
      const build = spawn('go', ['build', '-o', executable, './cmd/denova'], { cwd: repositoryRoot, stdio: 'inherit', signal })
      const [code] = await once(build, 'exit')
      if (code !== 0) throw new Error(`Backend build failed: ${code}`)
    }
    const git = spawn('git', ['rev-parse', 'HEAD'], { cwd: repositoryRoot, stdio: ['ignore', 'pipe', 'ignore'] })
    let commit = ''
    git.stdout.on('data', chunk => { commit += chunk })
    await once(git, 'exit')
    const status = spawn('git', ['status', '--porcelain'], { cwd: repositoryRoot, stdio: ['ignore', 'pipe', 'ignore'] })
    let worktreeStatus = ''
    status.stdout.on('data', chunk => { worktreeStatus += chunk })
    await once(status, 'exit')
    const metadata = { commit: commit.trim(), started_at: new Date().toISOString(), repeat,
      worktree_dirty: Boolean(worktreeStatus.trim()),
      cases: selected.map(item => item.id), variants: variants.map(({ label, profile_id, writing_skill, skills_directory }) => ({ label, profile_id, writing_skill, skills_directory })) }
    await writeFile(path.join(outputDirectory, 'run.json'), JSON.stringify(metadata, null, 2))
    process.env.PROMPTFOO_DISABLE_TELEMETRY = '1'
    process.env.PROMPTFOO_CONFIG_DIR = path.join(toolDirectory, 'results', 'promptfoo')
    const { default: promptfoo } = await import('promptfoo')
    console.log(`[creative-eval] Running ${selected.length} scenarios, ${variants.length} variants, ${repeat} repetitions`)
    const evaluation = await promptfoo.evaluate({
      description: `Denova creative evaluation ${metadata.started_at}`,
      prompts: ['{{request}}'], providers,
      tests: selected.map(scenario => ({
        description: scenario.id, vars: { request: scenario.prompt, scenario },
        assert: [
          { type: 'javascript', value: structuralChecks, metric: 'Product constraints' },
          { type: 'llm-rubric', metric: 'Creative constraints', provider: gradingProfile?.provider || config.grader,
            value: `Original user request:\n${scenario.prompt}\n\nInitial project facts:\n${JSON.stringify({ files: scenario.files, lore: scenario.lore, origin: scenario.origin }, null, 2)}\n\nCriteria:\n${scenario.rubric}\n\nEvaluate the saved main output against these facts. The output also includes persisted evidence; use alternate-branch evidence only to check the isolation exercise setup.` },
        ],
      })),
      metadata, sharing: false, writeLatestResults: true,
      outputPath: [path.join(outputDirectory, 'report.json'), path.join(outputDirectory, 'report.html')],
    }, { cache: false, repeat, maxConcurrency: 1, timeoutMs: 0, maxEvalTimeMs: 0, abortSignal: signal })
    const summary = await evaluation.toEvaluateSummary()
    console.log(`[creative-eval] Report: ${path.join(outputDirectory, 'report.html')}`)
    console.log(`[creative-eval] Results: ${JSON.stringify(summary.stats)}`)
    return { outputDirectory, summary }
  } finally {
    gradingProfile?.release()
    if (!binary) await rm(executable, { force: true })
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const { values } = parseArgs({ options: {
    config: { type: 'string', default: path.join(toolDirectory, 'run.local.json') }, repeat: { type: 'string', default: '1' },
    case: { type: 'string' }, help: { type: 'boolean', short: 'h' },
  } })
  if (values.help) {
    console.log('Usage: ./scripts/eval-creative.sh [--config <run.local.json>] [--case <id substring>] [--repeat <count>]\nDefaults: saved tools/creative-eval/run.local.json, all scenarios, one repetition.')
  } else {
    const controller = new AbortController()
    for (const signal of ['SIGINT', 'SIGTERM']) process.once(signal, () => controller.abort(new Error(`Cancelled by ${signal}`)))
    try {
      const repeat = Number(values.repeat)
      if (!Number.isInteger(repeat) || repeat < 1) throw new Error('--repeat must be a positive integer')
      const { summary } = await evaluateCreative({ configPath: path.resolve(values.config), repeat, filter: values.case, signal: controller.signal })
      // A quality regression is a report, not a CI gate. Execution/grading
      // errors remain failures so a manual or scheduled run can be diagnosed.
      if (controller.signal.aborted) process.exitCode = 130
      else if (summary.stats.errors) process.exitCode = 1
    } catch (error) {
      console.error(`[creative-eval] ${error.message}`)
      process.exitCode = controller.signal.aborted ? 130 : 1
    }
  }
}
